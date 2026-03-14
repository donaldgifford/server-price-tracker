# REFACTOR.md

## The Problem in One Sentence

We built two sources of truth: PostgreSQL holds the real data, but the
application treats Prometheus metrics as a parallel state store — which means
after a pod restart the metrics are gone, the DB is fine, and the system looks
broken even when it isn't.

---

## What We Have

The current data flow is:

```
PostgreSQL (authoritative)
  └── SyncStateMetrics() → Prometheus gauges (ephemeral, in-memory)
                               └── Grafana (displays the ephemeral copy)
```

Every gauge — watches enabled, listings unscored, baselines warm/cold,
incomplete extractions — is a snapshot copied from the DB at the end of each
ingestion cycle. The moment the pod restarts, all gauges zero out. After the
last deployment the Grafana scoring panels showed 0 warm baselines, 0 total
baselines, 100% cold start rate — not because the data was gone, but because the
metrics hadn't been repopulated yet. The DB had ~2700 listings and a growing
baselines table the whole time.

This is the fundamental inversion: **the DB should drive the metrics, not the
other way around**. Metrics are observations, not state.

---

## Secondary Problems That Make the App "Not Usable"

These compound the above and are worth fixing in the same refactor pass.

### 1. `watches.last_polled_at` is never written

The column exists in the schema and is never touched. There is no way to tell
from the DB (or the API) when a watch was last ingested or whether it has ever
run. The operator has to infer this from Grafana counters that reset on restart.

### 2. Scheduler state is ephemeral

The `robfig/cron` scheduler lives in-process only. If the pod restarts between a
scheduled baseline refresh, the next refresh doesn't run until the full interval
elapses from restart time. There is no `job_runs` table, no record of "last
success", no error history. A baseline refresh that has failed ten times in a
row is invisible.

### 3. Sequential ingestion pipeline

All extraction and scoring happens synchronously inside a single
`for range watches` loop. For 18 watches with pages of 100 listings each, that
is potentially 1800 sequential LLM calls. The `llm.concurrency = 4` config
exists but is wired to a semaphore only inside the extractor — the per-watch
loop itself is still serial.

### 4. `last_polled_at` / `RescoreAll` loads everything at once

`RescoreAll` fetches all listings in a single query (limit=500 label is
misleading — it loops until exhausted). For the current dataset this is fine; it
will OOM at scale.

### 5. Alert deduplication can silently drop deals

`alerts` has `UNIQUE(watch_id, listing_id)`. A listing that scores 55, drops to
45, then returns to 55 will only ever fire one alert. The second price drop
event is silently discarded. Users miss the re-occurrence.

### 6. Notification delivery is not idempotent

Alerts are marked `notified=true` only after the Discord webhook returns 2xx. If
the webhook times out the alert stays pending and re-fires next cycle. No
idempotency key is included in the payload so Discord will render a duplicate
embed.

### 7. Time score is always 30

In `pkg/scorer/scorer.go` the time factor is computed but the `IsAuction`,
`AuctionEndingSoon`, and `IsNewListing` fields in `ListingData` are never
populated from the actual listing. The factor always evaluates to the neutral
branch (30/100). This means every auction listing is mispriced in the composite
score.

### 8. `extraction_confidence` is hardcoded to 0.9

The column is stored in the DB for every listing but the value is always 0.9
regardless of LLM response quality. It is never used in scoring or filtering.
Dead weight.

---

## Current State Architecture

```
  ┌──────────────────────────────────────────────────────────────────────┐
  │  server-price-tracker (single process)                               │
  │                                                                      │
  │  ┌──────────────────────────────────────────────────────────────┐   │
  │  │  robfig/cron Scheduler (in-memory, ephemeral)                │   │
  │  │                                                              │   │
  │  │  @every 15m ──▶ RunIngestion()                               │   │
  │  │                   │                                          │   │
  │  │                   ├─ for each watch (serial):                │   │
  │  │                   │    ├─ Search eBay API                    │   │
  │  │                   │    ├─ Upsert listing ──────────────────▶ DB  │
  │  │                   │    ├─ LLM Extract (sequential)           │   │
  │  │                   │    ├─ Score (sequential)                 │   │
  │  │                   │    └─ CreateAlert ──────────────────────▶ DB  │
  │  │                   │                                          │   │
  │  │                   ├─ ProcessAlerts                           │   │
  │  │                   │    └─ Discord webhook ──────────────────▶ ☁   │
  │  │                   │                                          │   │
  │  │                   └─ SyncStateMetrics() ◀────── DB queries   │   │
  │  │                        └─ Update Prometheus gauges ──────▶ ⚠  │   │
  │  │                              (EPHEMERAL — zeroes on restart)  │   │
  │  │                                                              │   │
  │  │  @every 6h ───▶ RunBaselineRefresh()                         │   │
  │  │                   ├─ RecomputeAllBaselines ──────────────────▶ DB  │
  │  │                   └─ RescoreAll (loads all listings at once) │   │
  │  │                                                              │   │
  │  └──────────────────────────────────────────────────────────────┘   │
  │                                                                      │
  │  Echo HTTP API ──────────────────────────────────────────────────▶ spt CLI
  │  Prometheus /metrics ──────────────────────────────────────────▶ Grafana
  │                                                                      │
  └──────────────────────────────────────────────────────────────────────┘
              │                              │
              ▼                              ▼
     ┌─────────────────┐          ┌─────────────────────┐
     │   PostgreSQL    │          │   Ollama / Claude   │
     │                 │          │   (LLM Backend)     │
     │  watches        │          └─────────────────────┘
     │  listings       │
     │  price_baselines│    ⚠  Problems:
     │  alerts         │    • Scheduler state: not persisted (job history lost on restart)
     │                 │    • Metrics: ephemeral copy of DB state (zeroes on restart)
     │  ❌ job_runs    │    • last_polled_at: column exists, never written
     │  ❌ queue       │    • Ingestion: fully serial (1 LLM call at a time per watch)
     └─────────────────┘    • RescoreAll: loads everything into memory at once
                            • Alert dedup: UNIQUE constraint prevents re-alerts
                            • Time score: always returns 30 (inputs never populated)
```

---

## Proposed Architecture

```
  ┌──────────────────────────────────────────────────────────────────────┐
  │  server-price-tracker (single process, or split if needed)           │
  │                                                                      │
  │  ┌──────────────────────────────────────────────────────────────┐   │
  │  │  DB-Backed Scheduler                                         │   │
  │  │  (wraps robfig/cron + persists to job_runs table)            │   │
  │  │                                                              │   │
  │  │  @every 15m ──▶ RunIngestion()                               │   │
  │  │                   │   ┌─ AcquireAdvisoryLock (pg) ────────▶ DB  │
  │  │                   │   ├─ InsertJobRun(status=running) ──────▶ DB  │
  │  │                   │   │                                     │   │
  │  │                   │   ├─ for each watch (serial for now):   │   │
  │  │                   │   │    ├─ Search eBay API               │   │
  │  │                   │   │    ├─ Upsert listing ──────────────▶ DB  │
  │  │                   │   │    ├─ Enqueue extraction ──────────▶ DB  │
  │  │                   │   │    └─ UpdateWatchLastPolled ────────▶ DB  │
  │  │                   │   │                                     │   │
  │  │                   │   └─ UpdateJobRun(status=succeeded) ───▶ DB  │
  │  │                   │                                         │   │
  │  │  @every 6h ───▶ RunBaselineRefresh()                        │   │
  │  │                   ├─ InsertJobRun ──────────────────────────▶ DB  │
  │  │                   ├─ RecomputeAllBaselines (chunked) ───────▶ DB  │
  │  │                   └─ RescoreAll (cursor-paginated) ─────────▶ DB  │
  │  │                                                              │   │
  │  └──────────────────────────────────────────────────────────────┘   │
  │                                                                      │
  │  ┌──────────────────────────────────────────────────────────────┐   │
  │  │  Extraction Worker Pool (goroutines, semaphore-bounded)      │   │
  │  │                                                              │   │
  │  │  Worker 1 ──▶ SELECT FOR UPDATE SKIP LOCKED ─────────────▶ DB  │
  │  │  Worker 2 ──▶  (extraction_queue table)                     │   │
  │  │  Worker 3 ──▶   ├─ ClassifyAndExtract ───────────────────▶ LLM  │
  │  │  Worker 4 ──▶   ├─ NormalizeRAMSpeed                       │   │
  │  │                 ├─ UpdateListingExtraction ─────────────────▶ DB  │
  │  │                 ├─ ScoreListing                             │   │
  │  │                 └─ UpdateScore ────────────────────────────▶ DB  │
  │  └──────────────────────────────────────────────────────────────┘   │
  │                                                                      │
  │  ┌──────────────────────────────────────────────────────────────┐   │
  │  │  Alert Processor                                             │   │
  │  │                                                              │   │
  │  │  Poll DB for unscored listings ────────────────────────────▶ DB  │
  │  │  Evaluate alert thresholds                                   │   │
  │  │  Insert notification_attempt ──────────────────────────────▶ DB  │
  │  │  Send Discord webhook ─────────────────────────────────────▶ ☁   │
  │  │  MarkAlertNotified ────────────────────────────────────────▶ DB  │
  │  └──────────────────────────────────────────────────────────────┘   │
  │                                                                      │
  │  SyncStateMetrics() ─── queries system_state VIEW ─────────────▶ DB  │
  │      └─ Updates Prometheus gauges (observational only,           │   │
  │         staleness <= 1 ingestion cycle, not authoritative)       │   │
  │                                                                      │
  │  Echo HTTP API ──────────────────────────────────────────────────▶ spt CLI
  │  Prometheus /metrics ──────────────────────────────────────────▶ Grafana
  │                                                                      │
  └──────────────────────────────────────────────────────────────────────┘
              │                              │
              ▼                              ▼
     ┌─────────────────┐          ┌─────────────────────┐
     │   PostgreSQL    │          │   Ollama / Claude   │
     │  (sole source   │          │   (LLM Backend)     │
     │   of truth)     │          └─────────────────────┘
     │                 │
     │  watches        │   ✓ All state lives here
     │  listings       │   ✓ Metrics derived from DB views
     │  price_baselines│   ✓ Scheduler history persisted
     │  alerts         │   ✓ Job queue backed by DB
     │  job_runs  ✅   │   ✓ Advisory locks for concurrency
     │  extraction_    │   ✓ Notification attempts tracked
     │    queue   ✅   │   ✓ last_polled_at written every cycle
     │  notification_  │
     │    attempts ✅  │
     └─────────────────┘
```

---

## Target Architecture

The goal is: **PostgreSQL is the only source of truth. Prometheus reads from it.
Everything else reads from PostgreSQL.**

Prometheus gauges become eventually consistent observations with a known
staleness bound (1 ingestion cycle at most). They are not authoritative. The API
endpoints are authoritative because they query the DB directly.

---

## What Needs to Change

### Phase A: DB as Ground Truth (no new infrastructure)

These are all pure Go + SQL changes. No Redis, no workers.

#### **A1. Persist scheduler state**

Add `job_runs` table:

```sql
CREATE TABLE job_runs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_name    TEXT NOT NULL,           -- 'ingestion', 'baseline_refresh', 're_extraction'
    started_at  TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    status      TEXT NOT NULL,           -- 'running', 'succeeded', 'failed'
    error_text  TEXT,
    rows_affected INT
);
CREATE INDEX ON job_runs (job_name, started_at DESC);
```

Before each scheduler job fires: insert `status='running'`. After: update
`status='succeeded'|'failed'`. The engine already returns errors — this is just
plumbing them into a store write. The `GET /api/v1/jobs` endpoint can expose
this so the operator knows without Grafana whether baseline refresh last worked.

#### **A2. Write `watches.last_polled_at`**

In `engine.RunIngestion`, after processing each watch:

```go
store.UpdateWatchLastPolled(ctx, watch.ID, time.Now())
```

One SQL update. The `spt watches list` output then shows real staleness.

#### **A3. Add `GET /api/v1/jobs` endpoint**

Simple read from `job_runs`. Returns the last N runs per job type, their status
and error. No new schema beyond A1. Makes operational state observable without
Grafana.

#### **A4. Fix time score**

In `engine.processListing` (where `ListingData` is assembled), populate:

- `IsAuction`: `listing.ListingType == domain.Auction`
- `AuctionEndingSoon`:
  `listing.AuctionEndAt != nil && time.Until(*listing.AuctionEndAt) < 4*time.Hour`
- `IsNewListing`: `time.Since(listing.FirstSeenAt) < 24*time.Hour`

This costs nothing and fixes a 5% weight factor that currently misfires for
every auction listing.

#### **A5. Fix alert deduplication**

Change from `UNIQUE(watch_id, listing_id)` to allow re-alerts with a cooldown:

```sql
ALTER TABLE alerts DROP CONSTRAINT alerts_watch_id_listing_id_key;
CREATE UNIQUE INDEX alerts_active_unique
    ON alerts (watch_id, listing_id)
    WHERE notified = false;
```

This lets a listing re-alert if the previous alert was already notified (the
deal came back). Add a `cooldown_until` column or just check
`notified_at > now() - interval '24h'` in the query.

#### **A6. Idempotent notifications**

Include `alert.ID` as a header (`X-Alert-ID`) in the Discord webhook POST. On
the Discord side this is fire-and-forget so the real protection is on our end:
add a `notification_attempts` table:

```sql
CREATE TABLE notification_attempts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_id    UUID NOT NULL REFERENCES alerts(id),
    attempted_at TIMESTAMPTZ NOT NULL,
    succeeded   BOOL NOT NULL,
    http_status INT,
    error_text  TEXT
);
```

Only `MarkAlertNotified` after a successful attempt is recorded. On the next
cycle, skip alerts that already have a successful `notification_attempts` row.

---

### Phase B: Redis for Rate Limiting and Distributed Lock

These become necessary if we ever run more than one replica, or if we want
accurate quota enforcement across pod restarts.

#### **B1. Persist rate limiter state**

Add a `rate_limiter_state` table (or use Redis SET with TTL). On startup,
restore the last known token count and reset time. On shutdown, flush current
state. This prevents the "I just restarted and think I have 5000 tokens but I
actually have 12" problem.

#### **B2. Distributed ingestion lock**

Use `pg_try_advisory_lock` (already available in PostgreSQL, no Redis needed) to
ensure only one replica runs ingestion at a time:

```go
func (s *PostgresStore) AcquireIngestionLock(ctx context.Context) (bool, error) {
    var acquired bool
    err := s.pool.QueryRow(ctx,
        "SELECT pg_try_advisory_lock(hashtext('ingestion'))").Scan(&acquired)
    return acquired, err
}

func (s *PostgresStore) ReleaseIngestionLock(ctx context.Context) error {
    _, err := s.pool.Exec(ctx,
        "SELECT pg_advisory_unlock(hashtext('ingestion'))")
    return err
}
```

This is free — PostgreSQL advisory locks are already in the toolkit.

---

### Phase C: Async Extraction Worker Pool

This is the scaling play. It unblocks the sequential processing bottleneck and
makes re-extraction a background task that doesn't interrupt ingestion.

**Architecture:**

```
RunIngestion
  ├── Search eBay, upsert listings → DB
  └── Enqueue extraction jobs → Redis list (or DB-backed queue)

ExtractionWorker (N replicas, same binary or separate)
  ├── Dequeue listing ID
  ├── ClassifyAndExtract
  ├── NormalizeRAMSpeed
  ├── UpdateListingExtraction → DB
  ├── ScoreListing
  └── UpdateScore → DB
```

The simplest version uses a `extraction_queue` table instead of Redis:

```sql
CREATE TABLE extraction_queue (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    listing_id  UUID NOT NULL REFERENCES listings(id),
    priority    INT NOT NULL DEFAULT 0,      -- 1=re-extract, 0=new
    enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at  TIMESTAMPTZ,
    claimed_by  TEXT,                         -- pod name
    completed_at TIMESTAMPTZ,
    attempts    INT NOT NULL DEFAULT 0,
    error_text  TEXT
);
CREATE INDEX ON extraction_queue (priority DESC, enqueued_at)
    WHERE completed_at IS NULL AND claimed_at IS NULL;
```

Workers `SELECT ... FOR UPDATE SKIP LOCKED` — PostgreSQL's native job queue
pattern. No Redis required for the MVP of this. Redis becomes an optimization
when throughput demands it.

---

## The Scheduler Needs to Be Replaced

Your intuition is correct. The current `robfig/cron` wrapper is too thin to
carry the weight of what we need. It runs jobs but knows nothing about them — no
history, no retry, no recovery. The refactor needs a real scheduler at its core.

### What the New Scheduler Looks Like

```
┌─────────────────────────────────────────────────────────────────┐
│  DBScheduler                                                    │
│                                                                 │
│  struct {                                                       │
│      cron    *cron.Cron                                         │
│      store   SchedulerStore      // job_runs + advisory lock    │
│      engine  *Engine                                            │
│      log     *slog.Logger                                       │
│  }                                                              │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  RunJob(ctx, jobName, fn) error                         │   │
│  │    1. AcquireAdvisoryLock(jobName)  ──────────────────▶ PG  │
│  │       └─ if locked: log + return (another replica runs) │   │
│  │    2. InsertJobRun(started, running) ─────────────────▶ PG  │
│  │    3. fn(ctx)  ◀── actual work (ingestion, baseline, …) │   │
│  │    4. UpdateJobRun(completed, succeeded|failed, error) ▶ PG  │
│  │    5. ReleaseAdvisoryLock(jobName)  ──────────────────▶ PG  │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  All cron entries call RunJob():                                │
│    @every 15m  → RunJob("ingestion",         eng.RunIngestion)  │
│    @every 6h   → RunJob("baseline_refresh",  eng.RunBaseline)   │
│    @every Xh   → RunJob("re_extraction",     eng.RunReExtract)  │
│    @every 15m  → RunJob("alert_processing",  eng.ProcessAlerts) │
└─────────────────────────────────────────────────────────────────┘
```

### New `job_runs` Schema and API Surface

```sql
CREATE TABLE job_runs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_name     TEXT NOT NULL,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    status       TEXT NOT NULL DEFAULT 'running',  -- running | succeeded | failed
    error_text   TEXT,
    rows_affected INT
);
CREATE INDEX ON job_runs (job_name, started_at DESC);
```

Exposed via two new endpoints:

```
GET /api/v1/jobs
  → [{ job_name, last_run_at, last_status, last_error, rows_affected }, ...]
  → One row per distinct job_name, most recent run

GET /api/v1/jobs/{job_name}
  → [{ id, started_at, completed_at, status, error_text, rows_affected }, ...]
  → Full history for a job (last 50 runs, paginated)
```

This replaces the need to read Prometheus for operational health. The `spt` CLI
gets a corresponding `spt jobs list` / `spt jobs history ingestion` command.

### Recovery on Restart

Because the scheduler writes `started_at` before running and `completed_at`
after, a pod crash leaves a row with `status='running'` and no `completed_at`.
On startup, the scheduler can detect these stale rows and either:

- Mark them `status='crashed'` (honest, easy)
- Re-run them immediately if the job is idempotent (ingestion is, baseline
  refresh is)

```go
func (s *DBScheduler) RecoverStaledJobs(ctx context.Context) {
    stale, _ := s.store.ListRunningJobsOlderThan(ctx, 2*time.Hour)
    for _, job := range stale {
        s.log.Warn("recovering stale job run", "job", job.Name, "started_at", job.StartedAt)
        s.store.UpdateJobRun(ctx, job.ID, "crashed", "process restarted", 0)
    }
}
```

Called in `startServer()` before starting the cron, this immediately gives the
operator accurate state the moment the pod comes up — no more "mystery zeros" in
the dashboard.

### Scheduler Transition Plan

The existing `internal/engine/scheduler.go` is kept but gutted to a thin shim
during transition:

1. Add `job_runs` migration (new table, no data risk)
2. Add `SchedulerStore` interface methods (`InsertJobRun`, `UpdateJobRun`,
   `AcquireAdvisoryLock`, `ReleaseAdvisoryLock`)
3. Add `RecoverStaledJobs` called on startup
4. Wrap each cron callback in `RunJob()`
5. Add `GET /api/v1/jobs` handler + `spt jobs` CLI command
6. Remove the in-process `SyncNextRunTimestamps` calls that were only there to
   work around ephemeral state (replaced by DB-authoritative job history)

This is Phase A1 expanded — same migration, more wiring, highest operational
value of any change in this document.

---

## What Not to Change

- **The schema for `watches`, `listings`, `price_baselines`, `alerts`** — these
  are solid. Do not migrate the existing data, do not rename columns. Additive
  changes only.
- **The Huma API handlers** — the external contract is fine. Internal wiring
  changes only.
- **The scoring formula** — the weights and percentile math are correct. The fix
  is populating the inputs properly (Phase A4), not changing the formula.
- **The LLM extraction backends** — Ollama, Anthropic, OpenAI-compat all work.
  The NormalizeRAMSpeed post-processing is correct.
- **The Helm chart / Kustomize deploy** — deployment is not the problem.

---

## Data Safety

The current dataset (~2700 listings, growing baselines, 18 watches) must not be
touched. All changes are:

1. **Additive schema migrations** — new tables, new indexes, new columns with
   defaults. No drops, no renames, no type changes on existing columns.
2. **Backfill via `spt` CLI** — after deploying Phase A, run
   `spt baselines refresh && spt rescore` to repopulate metrics from the
   authoritative data that already exists.
3. **No data format changes** — `attributes` JSONB, `product_key` format,
   `score_breakdown` JSONB structure all stay the same.

---

## Prioritized Work Order

| Phase | Change                              | Value                  | Effort |
| ----- | ----------------------------------- | ---------------------- | ------ |
| A1    | `job_runs` table + scheduler writes | Operational visibility | Low    |
| A2    | Write `last_polled_at` per watch    | Debugging              | Low    |
| A4    | Fix time score inputs               | Score accuracy         | Low    |
| A5    | Fix alert deduplication             | Miss fewer deals       | Medium |
| A3    | `GET /api/v1/jobs` endpoint         | Observability          | Low    |
| A6    | Idempotent notifications            | Reliability            | Medium |
| B2    | Advisory lock for ingestion         | Multi-replica safety   | Low    |
| B1    | Rate limiter persistence            | Quota accuracy         | Medium |
| C     | Async extraction queue              | Throughput             | High   |

Phases A1, A2, A4 can be done in a single PR with no new infrastructure and
immediately fix the most visible operational pain (missing state after restarts,
wrong auction scores).
