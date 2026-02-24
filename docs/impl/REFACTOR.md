# Refactor Implementation Plan

Source: `docs/REFACTOR.md`

This plan implements the architectural changes described in the refactor document in
five sequential phases. Each phase is independently deployable and leaves the system
in a better state than it started — no phase requires the next to function correctly.
All schema changes are additive (new tables, new indexes, new nullable columns with
defaults). No existing columns are renamed, dropped, or type-changed.

**Golden rule:** the ~2700 existing listings, 18 watches, and all baselines must
survive every phase intact.

---

## Decisions

All open questions have been answered. Recorded here for reference.

1. **Extraction worker concurrency model** — **In-process goroutine pool** in the
   same binary. Uses the existing `llm.concurrency` config as the pool size. If we
   ever need multiple replicas the queue table already provides the coordination
   primitive; at that point we promote to a separate binary.

2. **Advisory lock scope for ingestion** — **`scheduler_locks` table with atomic
   upsert** (described in Phase 1 migration). PostgreSQL advisory locks are
   session-scoped and break with pgx connection pools. A dedicated lock table avoids
   the session-affinity problem entirely: acquire = `INSERT ... ON CONFLICT DO UPDATE
   ... WHERE expires_at < now()`, release = `DELETE`. Locks auto-expire after the
   configured job timeout so a crashed pod never permanently blocks the next run.
   Safest and pool-safe.

3. **Alert re-alerts** — **Disabled by default.** When enabled (opt-in per watch or
   globally via config), cooldown defaults to **24 hours**. The schema uses a partial
   unique index so the constraint is enforced regardless of the feature flag; the
   application layer checks the flag and cooldown before calling `CreateAlert`.

4. **`job_runs` retention** — **Delete rows older than 30 days.** Cleanup runs inside
   `RecoverStaleJobRuns` at startup (purge old rows while recovering stale ones). No
   separate cron job needed at current volume.

5. **`watches.last_polled_at` backfill** — **NULL is fine.** Will show as `never`
   until the next ingestion cycle. No backfill on migration.

6. **Discord idempotency dedup window** — **No dedup window.** The
   `notification_attempts` row is written before the webhook fires (attempt recorded
   as in-flight), then updated on success/failure. A timeout race that causes a
   duplicate Discord embed is acceptable at current scale.

7. **`RescoreAll` cursor** — **Use `id` (UUID primary key) as the cursor.**
   `id` is `gen_random_uuid()` — random but unique and always indexed via the PK.
   `first_seen_at` has microsecond precision but the existing index is `DESC`-only
   and ties are theoretically possible during bulk upserts. Ordering doesn't matter
   for `RescoreAll`; `WHERE id > $1 ORDER BY id ASC LIMIT $2` hits the PK index,
   has no ties, and is fully stable.

8. **Re-extraction queue vs. direct path** — **Keep both paths.** `RunReExtraction`
   (direct, synchronous) stays as-is for `POST /api/v1/reextract` and the CLI.
   The scheduler-triggered path enqueues into `extraction_queue` (async, worker pool).
   If a separate retriggering queue is ever needed it will be a new table.

---

## Phase 1: DB as Ground Truth — Scheduler State and Watch Staleness

**Goal:** Every piece of operational state that currently lives only in Prometheus or
in-process memory is persisted to PostgreSQL. After this phase, a pod restart loses
no information visible to operators.

**Scope:** New migration, engine changes, scheduler changes, two new store methods,
one new API endpoint, one new CLI command.

**No infrastructure changes required.**

---

### Migration

- [x] Create `migrations/002_scheduler_state.sql`:

  ```sql
  -- Job run history for scheduler observability and crash recovery.
  CREATE TABLE job_runs (
      id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
      job_name      TEXT        NOT NULL,
      started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
      completed_at  TIMESTAMPTZ,
      status        TEXT        NOT NULL DEFAULT 'running',  -- running | succeeded | failed | crashed
      error_text    TEXT,
      rows_affected INT
  );
  CREATE INDEX job_runs_name_started ON job_runs (job_name, started_at DESC);

  -- Distributed scheduler lock table (pool-safe alternative to pg advisory locks).
  -- Acquire: INSERT ... ON CONFLICT DO UPDATE ... WHERE expires_at < now() RETURNING *
  -- Release: DELETE WHERE job_name = $1 AND lock_holder = $2
  -- A crashed holder's lock auto-expires at expires_at so the next run is never blocked.
  CREATE TABLE scheduler_locks (
      job_name    TEXT        PRIMARY KEY,
      locked_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
      lock_holder TEXT        NOT NULL,   -- pod name or hostname
      expires_at  TIMESTAMPTZ NOT NULL    -- locked_at + job timeout ceiling (e.g. 30min)
  );

  -- Track last ingestion timestamp per watch.
  ALTER TABLE watches ADD COLUMN last_polled_at TIMESTAMPTZ;

  -- Idempotent notification tracking.
  CREATE TABLE notification_attempts (
      id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
      alert_id     UUID        NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
      attempted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
      succeeded    BOOL        NOT NULL,
      http_status  INT,
      error_text   TEXT
  );
  CREATE INDEX notification_attempts_alert ON notification_attempts (alert_id, attempted_at DESC);
  ```

- [x] Copy migration to `internal/store/migrations/002_scheduler_state.sql` (embed path)

---

### Store Interface (`internal/store/store.go`)

Add a new `// Scheduler` section to the `Store` interface:

- [x] Add method `InsertJobRun(ctx context.Context, jobName string) (id string, err error)`
- [x] Add method `CompleteJobRun(ctx context.Context, id string, status string, errText string, rowsAffected int) error`
- [x] Add method `ListJobRuns(ctx context.Context, jobName string, limit int) ([]domain.JobRun, error)`
- [x] Add method `ListLatestJobRuns(ctx context.Context) ([]domain.JobRun, error)` — one per distinct job name
- [x] Add method `UpdateWatchLastPolled(ctx context.Context, watchID string, t time.Time) error`
- [x] Add method `RecoverStaleJobRuns(ctx context.Context, olderThan time.Duration) (int, error)` — marks `status='crashed'` for `running` rows older than `olderThan`, then deletes rows older than 30 days
- [x] Add method `AcquireSchedulerLock(ctx context.Context, jobName string, holder string, ttl time.Duration) (bool, error)` — `INSERT INTO scheduler_locks ... ON CONFLICT (job_name) DO UPDATE ... WHERE expires_at < now()` returning whether lock was acquired
- [x] Add method `ReleaseSchedulerLock(ctx context.Context, jobName string, holder string) error` — `DELETE FROM scheduler_locks WHERE job_name = $1 AND lock_holder = $2`

---

### Domain Types (`pkg/types/types.go`)

- [x] Add `JobRun` struct:
  ```go
  type JobRun struct {
      ID           string     `json:"id"`
      JobName      string     `json:"job_name"`
      StartedAt    time.Time  `json:"started_at"`
      CompletedAt  *time.Time `json:"completed_at,omitempty"`
      Status       string     `json:"status"`
      ErrorText    string     `json:"error_text,omitempty"`
      RowsAffected *int       `json:"rows_affected,omitempty"`
  }
  ```
- [x] Add `last_polled_at *time.Time` field to `Watch` struct

---

### SQL Queries (`internal/store/queries.go`)

- [x] Add `queryInsertJobRun` constant
- [x] Add `queryCompleteJobRun` constant
- [x] Add `queryListJobRuns` constant (ORDER BY started_at DESC, parameterised limit)
- [x] Add `queryListLatestJobRuns` constant (DISTINCT ON job_name, ORDER BY started_at DESC)
- [x] Add `queryUpdateWatchLastPolled` constant
- [x] Add `queryRecoverStaleJobRuns` constant — two statements:
  1. UPDATE `status='crashed'` WHERE `status='running'` AND `started_at < now() - $1`
  2. DELETE WHERE `started_at < now() - interval '30 days'`
- [x] Add `queryAcquireSchedulerLock` constant:
  ```sql
  INSERT INTO scheduler_locks (job_name, lock_holder, expires_at)
  VALUES ($1, $2, now() + $3::interval)
  ON CONFLICT (job_name) DO UPDATE
      SET locked_at   = now(),
          lock_holder = EXCLUDED.lock_holder,
          expires_at  = EXCLUDED.expires_at
      WHERE scheduler_locks.expires_at < now()
  RETURNING job_name
  ```
  Acquired if a row is returned; not acquired if no row returned (conflict not replaced).
- [x] Add `queryReleaseSchedulerLock` constant — `DELETE FROM scheduler_locks WHERE job_name = $1 AND lock_holder = $2`

---

### PostgreSQL Store (`internal/store/postgres.go`)

- [x] Implement `InsertJobRun` — INSERT returning `id`
- [x] Implement `CompleteJobRun` — UPDATE by id
- [x] Implement `ListJobRuns` — SELECT with job_name filter and limit
- [x] Implement `ListLatestJobRuns` — DISTINCT ON query
- [x] Implement `UpdateWatchLastPolled` — UPDATE watches SET last_polled_at = $2 WHERE id = $1
- [x] Implement `RecoverStaleJobRuns` — crash-mark UPDATE + 30-day DELETE, return rows affected by crash-mark
- [x] Implement `AcquireSchedulerLock` — run `queryAcquireSchedulerLock`; acquired = row returned
- [x] Implement `ReleaseSchedulerLock` — run `queryReleaseSchedulerLock`

---

### Mocks

- [x] Run `make mocks` to regenerate `internal/store/mocks/MockStore`

---

### DB-Backed Scheduler (`internal/engine/scheduler.go`)

Replace the thin `robfig/cron` wrapper with a `DBScheduler` that persists every job
execution:

- [x] Add `store store.Store` field to `Scheduler` struct
- [x] Add `hostname string` field to `Scheduler` struct — populated from `os.Hostname()` at construction; used as `lock_holder` in `scheduler_locks`
- [x] Add `runJob(ctx context.Context, jobName string, ttl time.Duration, fn func(context.Context) error) error` private method:
  1. `store.AcquireSchedulerLock(ctx, jobName, s.hostname, ttl)` — if not acquired, log and return nil (another instance is running)
  2. `store.InsertJobRun(ctx, jobName)` → get `runID`
  3. `defer store.ReleaseSchedulerLock(ctx, jobName, s.hostname)`
  4. `defer store.CompleteJobRun(ctx, runID, status, errText, 0)`
  5. Call `fn(ctx)`, capture error → determine status
- [x] Update `NewScheduler` to accept `store store.Store` as a new parameter
- [x] Update `runIngestion()` → `s.runJob(ctx, "ingestion", 30*time.Minute, s.engine.RunIngestion)`
- [x] Update `runBaselineRefresh()` → `s.runJob(ctx, "baseline_refresh", 60*time.Minute, s.engine.RunBaselineRefresh)`
- [x] Update `runReExtraction()` → `s.runJob(ctx, "re_extraction", 30*time.Minute, func(ctx context.Context) error { ... })`
- [x] Add `RecoverStaleJobRuns(ctx context.Context)` method — calls `store.RecoverStaleJobRuns(ctx, 2*time.Hour)`
- [x] Update `serve.go` `buildEngine` to pass `pgStore` to `NewScheduler`
- [x] Call `sched.RecoverStaleJobRuns(ctx)` in `startServer()` before `sched.Start()`

---

### Engine (`internal/engine/engine.go`)

- [x] In `RunIngestion`, after processing each watch, call `s.store.UpdateWatchLastPolled(ctx, watch.ID, time.Now())`
  — must not fail the overall ingestion if this write fails (log + continue)

---

### API Handler (`internal/api/handlers/jobs.go`)

- [x] Create `internal/api/handlers/jobs.go`:
  - Define narrow `JobsProvider` interface:
    ```go
    type JobsProvider interface {
        ListLatestJobRuns(ctx context.Context) ([]domain.JobRun, error)
        ListJobRuns(ctx context.Context, jobName string, limit int) ([]domain.JobRun, error)
    }
    ```
  - Define `JobsHandler` struct with `store JobsProvider`
  - Define `NewJobsHandler(s JobsProvider) *JobsHandler`
  - Define `ListJobsOutput` with `Body []domain.JobRun`
  - Define `GetJobHistoryInput` with `JobName string \`path:"job_name"\``
  - Define `GetJobHistoryOutput` with `Body []domain.JobRun`
  - Implement `ListJobs(ctx, *struct{}) (*ListJobsOutput, error)` — calls `ListLatestJobRuns`
  - Implement `GetJobHistory(ctx, *GetJobHistoryInput) (*GetJobHistoryOutput, error)` — calls `ListJobRuns`
  - Implement `RegisterJobRoutes(api huma.API, h *JobsHandler)`:
    - `GET /api/v1/jobs` — operation ID `list-jobs`, tag `scheduler`
    - `GET /api/v1/jobs/{job_name}` — operation ID `get-job-history`, tag `scheduler`

- [x] Create `internal/api/handlers/jobs_test.go`:
  - `TestListJobs_Success` — mock returns two job runs, assert 200 and body
  - `TestListJobs_Empty` — mock returns empty slice, assert 200 and `[]`
  - `TestListJobs_Error` — mock returns error, assert 500
  - `TestGetJobHistory_Success` — mock returns history for `"ingestion"`, assert 200
  - `TestGetJobHistory_Error` — mock returns error, assert 500

- [x] Register in `cmd/server-price-tracker/cmd/serve.go` inside the `if s != nil` block:
  ```go
  jobsH := handlers.NewJobsHandler(s)
  handlers.RegisterJobRoutes(humaAPI, jobsH)
  ```

---

### API Client (`internal/api/client/`)

- [x] Add `jobs.go` to `internal/api/client/`:
  - `ListJobs(ctx context.Context) ([]domain.JobRun, error)`
  - `GetJobHistory(ctx context.Context, jobName string) ([]domain.JobRun, error)`

---

### CLI Command (`cmd/spt/cmd/jobs.go`)

- [x] Create `cmd/spt/cmd/jobs.go`:
  - `jobsCmd()` — parent command `spt jobs`
  - `jobsListCmd()` — `spt jobs list` → calls `client.ListJobs`, prints table
  - `jobsHistoryCmd()` — `spt jobs history <job_name>` → calls `client.GetJobHistory`
- [x] Register in `cmd/spt/cmd/root.go`: `rootCmd.AddCommand(jobsCmd())`

---

### Tests

- [x] `internal/store/postgres_test.go` (or `store_test.go`) — integration-tagged tests for new store methods (use existing test DB helper pattern)
- [x] `internal/engine/scheduler_test.go` — update `NewScheduler` calls to pass mock store; add `TestScheduler_RunJob_Success`, `TestScheduler_RunJob_Failure`, `TestScheduler_RecoverStaleJobs`
- [x] `internal/engine/engine_test.go` — add `TestRunIngestion_WritesLastPolledAt` verifying `UpdateWatchLastPolled` is called
- [x] Run `make test && make lint`

---

### Success Criteria

- After pod restart, `spt jobs list` shows all previous job run statuses and errors
- Any job run that was `running` at crash time is visible as `crashed` after restart
- `spt watches list` shows `last_polled_at` for every watch after the next ingestion cycle
- `GET /api/v1/jobs` returns one row per job type
- `GET /api/v1/jobs/ingestion` returns the last N ingestion runs with status and error
- All new store methods have unit tests
- `make test && make lint` pass with 0 issues

---

## Phase 2: Score Accuracy and Alert Reliability

**Goal:** Fix the three correctness bugs that silently degrade deal quality: the time
score factor that always returns 30, alert deduplication that drops re-occurring
deals, and notification delivery that can double-send on timeout.

**Scope:** Scorer input population, schema migration for alerts, notification attempt
tracking. No new endpoints. No new infrastructure.

---

### Migration

- [ ] Create `migrations/003_alert_reliability.sql`:

  ```sql
  -- Drop unique constraint that prevents re-alerting on a returned deal.
  ALTER TABLE alerts DROP CONSTRAINT alerts_watch_id_listing_id_key;

  -- Partial unique index: only one PENDING alert per (watch, listing) at a time.
  -- Once an alert is notified, the listing can alert again.
  CREATE UNIQUE INDEX alerts_pending_unique
      ON alerts (watch_id, listing_id)
      WHERE notified = false;

  -- Cooldown: prevent re-alerting the same listing within 24h of last notification.
  -- Enforced in application logic, not schema (avoids DDL complexity).
  ```

- [ ] Copy migration to `internal/store/migrations/003_alert_reliability.sql`

---

### Fix Time Score Inputs (`internal/engine/engine.go`)

In the `processListing` function (the private helper that assembles `score.ListingData`
from a `*domain.Listing`):

- [ ] Set `IsAuction`:
  ```go
  IsAuction: listing.ListingType == domain.ListingTypeAuction,
  ```
- [ ] Set `AuctionEndingSoon`:
  ```go
  AuctionEndingSoon: listing.ListingType == domain.ListingTypeAuction &&
      listing.AuctionEndAt != nil &&
      time.Until(*listing.AuctionEndAt) < 4*time.Hour,
  ```
- [ ] Set `IsNewListing`:
  ```go
  IsNewListing: time.Since(listing.FirstSeenAt) < 24*time.Hour,
  ```
- [ ] Add unit test `TestProcessListing_TimescoreInputs` confirming all three fields
  are populated correctly for an auction listing ending in 2 hours

---

### Alert Re-alert Config (`internal/config/config.go`)

Re-alerts are **disabled by default**. When enabled, the cooldown defaults to 24h.

- [ ] Add `ReAlerts` struct to config:
  ```go
  // AlertsConfig defines alert behavior.
  type AlertsConfig struct {
      ReAlertsEnabled      bool          `yaml:"re_alerts_enabled"`       // default: false
      ReAlertsCooldown     time.Duration `yaml:"re_alerts_cooldown"`      // default: 24h
  }
  ```
- [ ] Add `Alerts AlertsConfig` field to `Config` struct
- [ ] Add `applyAlertsDefaults` to `applyDefaults`: if `ReAlertsCooldown == 0`, set to `24*time.Hour`
- [ ] Add `cfg.Alerts` to `Engine` options via `WithAlertsConfig(cfg AlertsConfig) EngineOption`

---

### Alert Re-alert Cooldown (`internal/store/store.go` and `internal/store/postgres.go`)

- [ ] Add `HasRecentAlert(ctx context.Context, watchID, listingID string, cooldown time.Duration) (bool, error)` to `Store` interface
- [ ] Add `queryHasRecentAlert` SQL constant:
  ```sql
  SELECT EXISTS (
      SELECT 1 FROM alerts
      WHERE watch_id = $1
        AND listing_id = $2
        AND notified = true
        AND notified_at > now() - $3::interval
  )
  ```
- [ ] Implement `HasRecentAlert` in `postgres.go`
- [ ] In `engine.evaluateAlert`:
  - If `cfg.Alerts.ReAlertsEnabled == false`: keep the existing partial unique index behaviour — a second alert is prevented by `alerts_pending_unique` index, no application-layer check needed
  - If `cfg.Alerts.ReAlertsEnabled == true`: call `HasRecentAlert(ctx, watch.ID, listing.ID, cfg.Alerts.ReAlertsCooldown)` before `CreateAlert`; skip if true
- [ ] Add unit tests:
  - `TestEvaluateAlert_ReAlertsDisabled_NoDuplicateWhilePending`
  - `TestEvaluateAlert_ReAlertsEnabled_SkipsCooldownListing`
  - `TestEvaluateAlert_ReAlertsEnabled_AllowsAfterCooldown`
- [ ] Run `make mocks`

---

### Idempotent Notifications (`internal/notify/` and `internal/engine/engine.go`)

- [ ] Add `InsertNotificationAttempt(ctx context.Context, alertID string, succeeded bool, httpStatus int, errText string) error` to `Store` interface
- [ ] Add `HasSuccessfulNotification(ctx context.Context, alertID string) (bool, error)` to `Store` interface
- [ ] Add SQL constants `queryInsertNotificationAttempt` and `queryHasSuccessfulNotification`
- [ ] Implement both methods in `postgres.go`
- [ ] In `engine.ProcessAlerts`, for each alert before sending:
  1. Call `HasSuccessfulNotification(ctx, alert.ID)` — skip send if true (prevents re-send after timeout)
  2. After Discord webhook call (success or failure), call `InsertNotificationAttempt`
  3. Only call `MarkAlertNotified` on success
- [ ] Add unit tests:
  - `TestProcessAlerts_SkipsAlreadyNotified`
  - `TestProcessAlerts_RecordsFailedAttempt`
  - `TestProcessAlerts_RecordsSuccessfulAttempt`
- [ ] Run `make mocks`

---

### Tests

- [ ] `TestHasRecentAlert_WithinCooldown` — mock returns true, verify alert skipped
- [ ] `TestHasRecentAlert_OutsideCooldown` — mock returns false, verify alert created
- [ ] `TestScorer_TimeScore_AuctionEndingSoon` — assert time factor = 100
- [ ] `TestScorer_TimeScore_NewListing` — assert time factor = 80
- [ ] `TestScorer_TimeScore_OldBuyItNow` — assert time factor = 30
- [ ] Run `make test && make lint`

---

### Success Criteria

- Auction listings ending within 4 hours have a time score of 100 (5% of composite)
- Listings first seen within 24 hours have a time score of 80
- A deal that disappears and reappears fires a second alert (no duplicate while pending)
- Notifications are not re-sent if a successful `notification_attempts` row exists
- `make test && make lint` pass with 0 issues

---

## Phase 3: DB-Backed Extraction Queue

**Goal:** Decouple eBay ingestion from LLM extraction. Ingestion writes listings to
the DB and enqueues extraction jobs. A worker pool drains the queue concurrently,
bounded by `llm.concurrency`. This unblocks the serial bottleneck and makes
re-extraction a true background task.

**Scope:** New `extraction_queue` table, queue-aware store methods, worker pool in
engine, scheduler wired to drain queue continuously.

---

### Migration

- [ ] Create `migrations/004_extraction_queue.sql`:

  ```sql
  CREATE TABLE extraction_queue (
      id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
      listing_id   UUID        NOT NULL REFERENCES listings(id) ON DELETE CASCADE,
      priority     INT         NOT NULL DEFAULT 0,   -- 1=re-extract, 0=new
      enqueued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
      claimed_at   TIMESTAMPTZ,
      claimed_by   TEXT,                              -- pod name or goroutine ID
      completed_at TIMESTAMPTZ,
      attempts     INT         NOT NULL DEFAULT 0,
      error_text   TEXT
  );

  -- Partial index for fast queue dequeue (only unclaimed, uncompleted rows).
  CREATE INDEX extraction_queue_dequeue
      ON extraction_queue (priority DESC, enqueued_at ASC)
      WHERE completed_at IS NULL AND claimed_at IS NULL;
  ```

- [ ] Copy migration to `internal/store/migrations/004_extraction_queue.sql`

---

### Store Interface (`internal/store/store.go`)

Add a new `// ExtractionQueue` section:

- [ ] `EnqueueExtraction(ctx context.Context, listingID string, priority int) error`
- [ ] `DequeueExtractions(ctx context.Context, workerID string, batchSize int) ([]domain.ExtractionJob, error)` — uses `SELECT ... FOR UPDATE SKIP LOCKED`
- [ ] `CompleteExtractionJob(ctx context.Context, id string, errText string) error`
- [ ] `CountPendingExtractionJobs(ctx context.Context) (int, error)`

---

### Domain Types (`pkg/types/types.go`)

- [ ] Add `ExtractionJob` struct:
  ```go
  type ExtractionJob struct {
      ID        string    `json:"id"`
      ListingID string    `json:"listing_id"`
      Priority  int       `json:"priority"`
      EnqueuedAt time.Time `json:"enqueued_at"`
      Attempts  int       `json:"attempts"`
  }
  ```

---

### SQL Queries (`internal/store/queries.go`)

- [ ] Add `queryEnqueueExtraction` — INSERT with ON CONFLICT DO NOTHING (idempotent)
- [ ] Add `queryDequeueExtractions` — `SELECT ... FOR UPDATE SKIP LOCKED LIMIT $2` UPDATE claimed_at, claimed_by
- [ ] Add `queryCompleteExtractionJob` — UPDATE completed_at, error_text, increment attempts
- [ ] Add `queryCountPendingExtractionJobs` — COUNT WHERE completed_at IS NULL

---

### PostgreSQL Store (`internal/store/postgres.go`)

- [ ] Implement `EnqueueExtraction` — wraps INSERT with `ON CONFLICT (listing_id) WHERE completed_at IS NULL DO NOTHING`
- [ ] Implement `DequeueExtractions` — CTE with `SELECT ... FOR UPDATE SKIP LOCKED`, returns `ExtractionJob` slice
- [ ] Implement `CompleteExtractionJob`
- [ ] Implement `CountPendingExtractionJobs`

---

### Engine (`internal/engine/engine.go`)

- [ ] Add `workerCount int` field to `Engine` (sourced from `cfg.LLM.Concurrency`)
- [ ] Add `WithWorkerCount(n int) EngineOption`
- [ ] Add `StartExtractionWorkers(ctx context.Context)` — launches `workerCount` goroutines each calling `runExtractionWorker`
- [ ] Add `runExtractionWorker(ctx context.Context, workerID string)` — loop:
  1. `store.DequeueExtractions(ctx, workerID, 1)` — claim one job
  2. `store.GetListingByID(ctx, job.ListingID)` — fetch full listing
  3. `ClassifyAndExtract` → `NormalizeRAMSpeed` → `UpdateListingExtraction`
  4. `ScoreListing` → `UpdateScore`
  5. `store.CompleteExtractionJob(ctx, job.ID, errText)`
  6. Sleep 100ms on empty queue before retrying
- [ ] Modify `RunIngestion` — after `UpsertListing`, call `store.EnqueueExtraction(ctx, listing.ID, 0)` instead of calling `ClassifyAndExtract` inline
- [ ] Modify `RunReExtraction` — call `store.EnqueueExtraction(ctx, listing.ID, 1)` (priority=1) instead of extracting inline
- [ ] In `startServer()`, call `eng.StartExtractionWorkers(cancelCtx)` after engine creation

---

### Config (`internal/config/config.go`)

- [ ] Confirm `LLMConfig.Concurrency int` is already present — it is (`llm.concurrency`, default 4). Wire it to `WithWorkerCount` in `serve.go`.

---

### Metrics (`internal/metrics/metrics.go`)

- [ ] Add `ExtractionQueueDepth` gauge:
  ```go
  ExtractionQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
      Namespace: namespace,
      Name:      "extraction_queue_depth",
      Help:      "Number of pending extraction jobs in the queue.",
  })
  ```
- [ ] Wire into `SyncStateMetrics` — call `store.CountPendingExtractionJobs`

---

### Tests

- [ ] `TestStartExtractionWorkers_ProcessesJob` — enqueue a job, start 1 worker, assert listing extraction is called and job is completed
- [ ] `TestRunExtractionWorker_HandlesExtractError` — LLM returns error, assert job is completed with error text (not retried infinitely)
- [ ] `TestRunIngestion_EnqueuesNotExtracts` — verify `EnqueueExtraction` is called and `ClassifyAndExtract` is NOT called inline
- [ ] `TestDequeueExtractions_SkipLocked` — two goroutines dequeue simultaneously, assert no duplicate claims
- [ ] Run `make test && make lint`

---

### Success Criteria

- Ingestion cycle time drops significantly (no longer blocked on LLM calls)
- `spt_extraction_queue_depth` gauge shows pending jobs
- Worker pool concurrency matches `llm.concurrency` config
- Failed extraction jobs are recorded with error text, not silently dropped
- Re-extraction (manual or scheduled) uses priority=1 and is processed before new listings
- `make test && make lint` pass with 0 issues

---

## Phase 4: Metrics Derived from DB

**Goal:** Eliminate the dual source of truth. All Prometheus gauges are authoritative
snapshots of DB state. `SyncStateMetrics` queries a single materialized view.
Grafana shows the right numbers immediately after restart.

**Scope:** New DB view, refactored `SyncStateMetrics`, removal of stale gauge updates
scattered across the codebase.

---

### Migration

- [ ] Create `migrations/005_system_state_view.sql`:

  ```sql
  -- Precomputed aggregate view for Prometheus scrape.
  -- Queried by SyncStateMetrics() on every ingestion cycle.
  CREATE VIEW system_state AS
  SELECT
      (SELECT COUNT(*)               FROM watches)              AS watches_total,
      (SELECT COUNT(*)               FROM watches WHERE enabled) AS watches_enabled,
      (SELECT COUNT(*)               FROM listings)             AS listings_total,
      (SELECT COUNT(*)               FROM listings WHERE component_type IS NULL OR component_type = '')
                                                                AS listings_unextracted,
      (SELECT COUNT(*)               FROM listings WHERE score IS NULL)
                                                                AS listings_unscored,
      (SELECT COUNT(*)               FROM alerts WHERE notified = false)
                                                                AS alerts_pending,
      (SELECT COUNT(*)               FROM price_baselines)      AS baselines_total,
      (SELECT COUNT(*)               FROM price_baselines WHERE sample_count >= 10)
                                                                AS baselines_warm,
      (SELECT COUNT(*)               FROM price_baselines WHERE sample_count < 10)
                                                                AS baselines_cold,
      (SELECT COUNT(DISTINCT product_key)
          FROM listings
          WHERE product_key IS NOT NULL
            AND product_key NOT IN (SELECT product_key FROM price_baselines))
                                                                AS product_keys_no_baseline,
      (SELECT COUNT(*)
          FROM listings
          WHERE (component_type = 'ram' AND (product_key IS NULL OR product_key LIKE '%:0'))
             OR (component_type = 'drive' AND product_key LIKE '%:unknown%'))
                                                                AS listings_incomplete_extraction,
      (SELECT COUNT(*)               FROM extraction_queue WHERE completed_at IS NULL)
                                                                AS extraction_queue_depth;
  ```

- [ ] Copy migration to `internal/store/migrations/005_system_state_view.sql`

---

### Store Interface (`internal/store/store.go`)

- [ ] Add `GetSystemState(ctx context.Context) (*domain.SystemState, error)` to `Store` interface
- [ ] Remove (or deprecate) individual count methods that are now covered by the view:
  - `CountWatches`, `CountListings`, `CountUnextractedListings`, `CountUnscoredListings`,
    `CountPendingAlerts`, `CountBaselinesByMaturity`, `CountProductKeysWithoutBaseline`,
    `CountIncompleteExtractions`, `CountPendingExtractionJobs`
  - **Note:** Keep them for now and mark `// Deprecated: use GetSystemState`. Remove
    in Phase 5 cleanup only after all callers are updated.

---

### Domain Types (`pkg/types/types.go`)

- [ ] Add `SystemState` struct mirroring the view columns:
  ```go
  type SystemState struct {
      WatchesTotal              int `json:"watches_total"`
      WatchesEnabled            int `json:"watches_enabled"`
      ListingsTotal             int `json:"listings_total"`
      ListingsUnextracted       int `json:"listings_unextracted"`
      ListingsUnscored          int `json:"listings_unscored"`
      AlertsPending             int `json:"alerts_pending"`
      BaselinesTotal            int `json:"baselines_total"`
      BaselinesWarm             int `json:"baselines_warm"`
      BaselinesCold             int `json:"baselines_cold"`
      ProductKeysNoBaseline     int `json:"product_keys_no_baseline"`
      ListingsIncompleteExtraction int `json:"listings_incomplete_extraction"`
      ExtractionQueueDepth      int `json:"extraction_queue_depth"`
  }
  ```

---

### PostgreSQL Store (`internal/store/postgres.go`)

- [ ] Add `queryGetSystemState` constant — `SELECT * FROM system_state`
- [ ] Implement `GetSystemState` — single query, scan all columns

---

### Engine (`internal/engine/engine.go`)

- [ ] Rewrite `SyncStateMetrics(ctx context.Context)`:
  1. Call `store.GetSystemState(ctx)` — one DB round-trip
  2. Set all gauges from `SystemState` fields:
     ```go
     metrics.WatchesTotal.Set(float64(s.WatchesTotal))
     metrics.WatchesEnabled.Set(float64(s.WatchesEnabled))
     // ... all fields
     ```
  3. Remove all individual `store.CountX()` calls from `SyncStateMetrics`

---

### Expose System State via API

- [ ] Add `GET /api/v1/system/state` handler:
  - Returns `SystemState` JSON directly from DB
  - Registered in `if s != nil` block in `serve.go`
  - This makes operational dashboards possible without Prometheus

---

### Tests

- [ ] `TestSyncStateMetrics_UsesGetSystemState` — verify `GetSystemState` is called once and all gauges are set correctly
- [ ] `TestGetSystemState_ReturnsAllFields` — integration test against real view (tagged `//go:build integration`)
- [ ] Update all existing `TestSyncStateMetrics_*` tests to use new mock shape
- [ ] Run `make test && make lint`

---

### Success Criteria

- After pod restart, `spt_watches_total`, `spt_baselines_warm`, and all state gauges
  are populated within 30 seconds of startup (from the `SyncStateMetrics` call in `startServer`)
- `GET /api/v1/system/state` returns accurate counts in JSON without Prometheus
- `SyncStateMetrics` makes exactly one DB call (not N individual count queries)
- Grafana panels no longer zero-out on restart
- `make test && make lint` pass with 0 issues

---

## Phase 5: Cleanup and Rate Limiter Persistence

**Goal:** Remove dead code from the dual-source-of-truth era, add rate limiter state
persistence to PostgreSQL, and wire `RescoreAll` into cursor-based pagination.

**Scope:** Schema migration, code cleanup, rate limiter store methods. No new
endpoints.

---

### Migration

- [ ] Create `migrations/006_rate_limiter_state.sql`:

  ```sql
  CREATE TABLE rate_limiter_state (
      id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
      tokens_used    INT         NOT NULL,
      daily_limit    INT         NOT NULL,
      reset_at       TIMESTAMPTZ NOT NULL,
      synced_at      TIMESTAMPTZ NOT NULL DEFAULT now()
  );
  -- Only one row ever; use a partial unique index to enforce it.
  CREATE UNIQUE INDEX rate_limiter_state_singleton ON rate_limiter_state ((true));
  ```

- [ ] Copy migration to `internal/store/migrations/006_rate_limiter_state.sql`

---

### Rate Limiter Persistence (`internal/ebay/rate_limiter.go`)

- [ ] Add `PersistRateLimiterState(ctx context.Context, tokensUsed int, dailyLimit int, resetAt time.Time) error` to `Store` interface
- [ ] Add `LoadRateLimiterState(ctx context.Context) (*domain.RateLimiterState, error)` to `Store` interface
- [ ] Add `RateLimiterState` domain type:
  ```go
  type RateLimiterState struct {
      TokensUsed int       `json:"tokens_used"`
      DailyLimit int       `json:"daily_limit"`
      ResetAt    time.Time `json:"reset_at"`
      SyncedAt   time.Time `json:"synced_at"`
  }
  ```
- [ ] After `SyncQuota` succeeds in `engine.go`, call `store.PersistRateLimiterState`
- [ ] In `startServer()`, call `store.LoadRateLimiterState` and use it to pre-warm the `RateLimiter` before the first ingestion cycle

---

### RescoreAll Cursor Pagination

`id` is `gen_random_uuid()` — random UUID, always the primary key, tie-free, and
always indexed. `first_seen_at` has an index but it is `DESC`-only and ties are
possible during bulk upserts. Ordering doesn't matter for `RescoreAll`; `id` is
the correct cursor.

- [ ] Add `ListListingsCursor(ctx context.Context, afterID string, limit int) ([]domain.Listing, error)` to `Store` interface:
  ```sql
  SELECT ... FROM listings WHERE id > $1 ORDER BY id ASC LIMIT $2
  ```
  Pass `""` as `afterID` for the first page (UUID sorts above empty string in pgx).
  Use `'\x00000000-0000-0000-0000-000000000000'` as the sentinel if needed.
- [ ] Add `queryListListingsCursor` SQL constant
- [ ] Implement `ListListingsCursor` in `postgres.go`
- [ ] Rewrite `RescoreAll` in `engine.go` to use cursor pagination:
  ```go
  const rescoreBatchSize = 200
  var cursor string
  for {
      listings, err := store.ListListingsCursor(ctx, cursor, rescoreBatchSize)
      if err != nil || len(listings) == 0 { break }
      for i := range listings {
          _ = ScoreListing(ctx, store, &listings[i])
      }
      cursor = listings[len(listings)-1].ID
  }
  ```
- [ ] Add test `TestRescoreAll_CursorPagination` — mock store returns 3 pages of 2 listings, assert all 6 are scored and cursor advances correctly

---

### Code Cleanup

- [ ] Remove the `// Deprecated` count methods from `Store` interface and `postgres.go`
  that were superseded by `GetSystemState` (after confirming no callers remain):
  - `CountIncompleteExtractions`
  - `CountIncompleteExtractionsByType`
  - `CountPendingAlerts`
  - `CountUnextractedListings`
  - `CountUnscoredListings`
  - `CountBaselinesByMaturity`
  - `CountProductKeysWithoutBaseline`
- [ ] Remove `ListingsIncompleteExtraction` and `ListingsIncompleteExtractionByType`
  Prometheus gauges from `metrics.go` (replaced by `system_state` view column)
- [ ] Remove `SyncNextRunTimestamps` from `scheduler.go` — replaced by `job_runs` table
- [ ] Remove `SchedulerNextIngestionTimestamp`, `SchedulerNextBaselineTimestamp`,
  `SchedulerNextReExtractionTimestamp` gauges from `metrics.go` — the `GET /api/v1/jobs`
  endpoint now provides next-run visibility from DB
- [ ] Remove `ExtractionStatsProvider` interface from `handlers/extraction_stats.go`
  (the `GET /api/v1/extraction/stats` endpoint can now delegate to `GET /api/v1/system/state`)
- [ ] Run `make mocks` after every interface change
- [ ] Run `make test && make lint`

---

### Success Criteria

- Rate limiter state survives pod restart (no "I have 5000 tokens" false positive)
- `RescoreAll` processes arbitrarily large listing tables without OOM risk
- No deprecated count methods remain in the Store interface
- `SchedulerNextX` gauges removed — operational visibility comes from `spt jobs list`
- Binary size and interface surface area are smaller than before this phase
- `make test && make lint` pass with 0 issues

---

## Prioritized Work Order Summary

| Phase | Effort | Risk to Existing Data | Blocks |
|---|---|---|---|
| 1 — Scheduler state + watch staleness | Medium | None (additive) | Nothing |
| 2 — Score accuracy + alert reliability | Medium | Schema change on `alerts` | Nothing |
| 3 — Extraction queue | High | Additive | Phase 1 (job run tracking) |
| 4 — DB-derived metrics | Medium | None (additive view) | Phase 3 (queue depth metric) |
| 5 — Cleanup + rate limiter | Low | Additive + deletions | Phase 4 |

Phases 1 and 2 can be worked in parallel on separate branches. Phase 3 should not
start until Phase 1 is merged (the queue worker needs `runJob` wrapping). Phase 4
depends on Phase 3's `extraction_queue_depth` column in the view. Phase 5 is pure
cleanup and can be deferred indefinitely without harm.
