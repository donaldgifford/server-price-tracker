# Metrics Refactor Plan

## Motivation

With the eBay Analytics API quota metrics (PR #22) and job label filters
(PR #21) merged, the core Prometheus instrumentation is solid. However,
several gaps remain:

1. **Dead metric**: `spt_extraction_duration_seconds` is registered but
   never observed anywhere in application code.
2. **Deprecated metric**: `spt_ebay_daily_usage` is still being set in
   `browse.go` despite being superseded by Analytics API gauges.
3. **No system-state gauges**: There is no visibility into active watches,
   total listings, pending alerts, or next scheduled ingestion time.
4. **No notification timing**: We track alert counts and failures but not
   delivery latency or last-successful-send timestamps.
5. **Dashboard observations** (from current Grafana screenshots):
   - Extraction Duration panel is always empty (metric never observed).
   - Several timeseries panels show duplicate legend entries (e.g.,
     "calls/s" twice in API Calls Rate, "listings/min" twice, "errors/min"
     twice, "failures/s" twice, "alerts/s" twice). These are recording
     rules and raw queries overlapping — need to clean up.
   - Score Distribution shows all zeroes (scoring works, but the bucket
     labels are sorted lexicographically instead of numerically — cosmetic).

## Current Metrics Inventory

| Metric | Type | Status |
|--------|------|--------|
| `spt_http_request_duration_seconds` | histogram | Working |
| `spt_http_requests_total` | counter | Working |
| `spt_healthz_up` | gauge | Working |
| `spt_readyz_up` | gauge | Working |
| `spt_ingestion_listings_total` | counter | Working |
| `spt_ingestion_errors_total` | counter | Working |
| `spt_ingestion_duration_seconds` | histogram | Working |
| `spt_extraction_duration_seconds` | histogram | **BROKEN** — never observed |
| `spt_extraction_failures_total` | counter | Working |
| `spt_scoring_distribution` | histogram | Working |
| `spt_ebay_api_calls_total` | counter | Working |
| `spt_ebay_daily_usage` | gauge | **DEPRECATED** — still being set |
| `spt_ebay_daily_limit_hits_total` | counter | Working |
| `spt_ebay_rate_limit` | gauge | Working |
| `spt_ebay_rate_remaining` | gauge | Working |
| `spt_ebay_rate_reset_timestamp` | gauge | Working |
| `spt_alerts_fired_total` | counter | Working |
| `spt_notification_failures_total` | counter | Working |

## Phase 1 — Fix Broken and Deprecated Metrics

### 1a. Instrument `spt_extraction_duration_seconds`

The histogram is defined in `internal/metrics/metrics.go` and referenced
in the Extraction Duration dashboard panel, but `.Observe()` is never
called. The extraction happens in `engine.processListing()` which calls
`eng.extractor.ClassifyAndExtract()`.

**Action:** Wrap the `ClassifyAndExtract()` call in `processListing()`
with a timer:

```go
start := time.Now()
ct, attrs, extractErr := eng.extractor.ClassifyAndExtract(ctx, listing.Title, nil)
metrics.ExtractionDuration.Observe(time.Since(start).Seconds())
```

Observe on both success and failure so the histogram reflects total LLM
call latency regardless of outcome.

**Files:**
- `internal/engine/engine.go` — add timer around extraction call

**Tests:**
- Add test case in `engine_test.go` verifying the histogram sample count
  increments after `processListing()` runs.

### 1b. Remove deprecated `spt_ebay_daily_usage`

The metric was deprecated in PR #22 but is still being set in
`internal/ebay/browse.go:98`. The Analytics API gauges
(`spt_ebay_rate_limit`, `spt_ebay_rate_remaining`) fully replace it.

**Action:**
- Remove `metrics.EbayDailyUsage.Set()` call from `browse.go`
- Remove the `EbayDailyUsage` variable from `internal/metrics/metrics.go`
- Remove from dashgen `KnownMetrics` if still present

**Files:**
- `internal/ebay/browse.go` — remove `.Set()` call
- `internal/metrics/metrics.go` — remove variable declaration

**Tests:**
- Update any tests that reference `EbayDailyUsage`.

## Phase 2 — System State Gauges

These gauges expose the current state of the system. They should be
updated at the end of each ingestion cycle (in `RunIngestion()`) and
on startup. They are cheap to compute — a few SQL `COUNT(*)` queries
and a scheduler entry inspection.

### New metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `spt_watches_total` | gauge | — | Total number of watches |
| `spt_watches_enabled` | gauge | — | Number of enabled watches |
| `spt_listings_total` | gauge | — | Total listings in database |
| `spt_listings_unextracted` | gauge | — | Listings without LLM extraction |
| `spt_listings_unscored` | gauge | — | Listings without a score |
| `spt_alerts_pending` | gauge | — | Alerts not yet notified |
| `spt_scheduler_next_ingestion_timestamp` | gauge | — | Unix epoch of next scheduled ingestion |
| `spt_scheduler_next_baseline_timestamp` | gauge | — | Unix epoch of next scheduled baseline refresh |
| `spt_ingestion_last_success_timestamp` | gauge | — | Unix epoch of last successful ingestion cycle |
| `spt_baseline_last_refresh_timestamp` | gauge | — | Unix epoch of last successful baseline refresh |

### Implementation

**Store additions** (`internal/store/store.go` + `postgres.go`):
- `CountWatches(ctx) (total int, enabled int, err error)`
- `CountListings(ctx) (total int, err error)` — reuse existing
  `countListingsSelect`
- `CountUnextractedListings(ctx) (int, error)`
- `CountUnscoredListings(ctx) (int, error)`
- `CountPendingAlerts(ctx) (int, error)`

These are simple `SELECT COUNT(*)` queries against existing tables. The
`ListUnextractedListings` and `ListUnscoredListings` methods already exist
and fetch full rows — add count-only variants to avoid loading data we
don't need.

**Metrics registration** (`internal/metrics/metrics.go`):
- Add all 10 gauge variables.

**Engine integration** (`internal/engine/engine.go`):
- Add `SyncStateMetrics(ctx)` method that queries the store and sets
  all state gauges.
- Call `SyncStateMetrics()` at the end of `RunIngestion()` (after
  `SyncQuota()`).
- Call `SyncStateMetrics()` on startup in `buildEngine()`.

**Scheduler integration** (`internal/engine/scheduler.go` or
`internal/engine/engine.go`):
- After successful ingestion, set `spt_ingestion_last_success_timestamp`
  to `time.Now().Unix()`.
- After successful baseline refresh, set
  `spt_baseline_last_refresh_timestamp`.
- Expose a method or callback to read `scheduler.Entries()` and set the
  `next_*_timestamp` gauges. Call it after scheduler starts and after
  each job completes.

**Files:**
- `internal/store/store.go` — add count methods to interface
- `internal/store/postgres.go` — implement count queries
- `internal/store/queries.go` — add SQL constants
- `internal/metrics/metrics.go` — register new gauges
- `internal/engine/engine.go` — `SyncStateMetrics()` method
- `internal/engine/scheduler.go` — set timestamp gauges after jobs

**Tests:**
- Store: table-driven tests for each count method using mocks.
- Engine: test `SyncStateMetrics()` sets expected gauge values given
  mock store returns.
- Scheduler: test that timestamp gauges are updated after job runs.

## Phase 3 — Notification and Alert Metrics

### New metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `spt_notification_duration_seconds` | histogram | — | Discord webhook HTTP POST latency |
| `spt_notification_last_success_timestamp` | gauge | — | Unix epoch of last successful notification |
| `spt_notification_last_failure_timestamp` | gauge | — | Unix epoch of last notification failure |
| `spt_alerts_fired_by_watch` | counter | `watch` | Alerts fired broken down by watch name |

### Implementation

**Notification duration** (`internal/notify/discord.go`):
- Wrap the `http.Post()` call in `post()` method with a timer.
- Observe `metrics.NotificationDuration` on every call (success and
  failure).

**Notification timestamps** (`internal/engine/alert.go`):
- On successful `sendSingle()` / `sendBatch()`: set
  `metrics.NotificationLastSuccessTimestamp` to `time.Now().Unix()`.
- On failure in `ProcessAlerts()`: set
  `metrics.NotificationLastFailureTimestamp` to `time.Now().Unix()`.

**Alerts by watch** (`internal/engine/alert.go`):
- In `sendSingle()` and `sendBatch()`, after successful notification,
  increment `metrics.AlertsFiredByWatch.WithLabelValues(watchName)`.
- This is in addition to the existing `AlertsFiredTotal` counter.
- Keep cardinality bounded — watch names are user-defined but typically
  fewer than 20. Add a comment noting the cardinality assumption.

**Files:**
- `internal/metrics/metrics.go` — register new metrics
- `internal/notify/discord.go` — add duration observation
- `internal/engine/alert.go` — add timestamp and per-watch metrics

**Tests:**
- Discord: verify histogram sample count increments after `SendAlert()`.
- Alert processing: verify timestamp gauges are set and per-watch
  counter increments.

## Phase 4 — Baseline Coverage and Cold Start Metrics

The scoring system defaults the price factor to a neutral 50 (40% of
total weight) when a product key has fewer than 10 sold samples in its
baseline. Baselines are only created at all when 5+ samples exist (the
SQL `HAVING count(*) >= 5` in `recompute_baseline()`). This means there
are three maturity tiers:

| Tier | Sample Count | Baseline Record? | Price Factor Used? |
|------|-------------|------------------|--------------------|
| No baseline | 0–4 | No | No — neutral 50 |
| Cold | 5–9 | Yes | No — neutral 50 |
| Warm | 10+ | Yes | Yes — percentile-based |

Today there is no visibility into how many product keys or listings fall
into each tier. Without this, you can't tell whether scores are
meaningful or mostly coasting on the neutral default.

### New metrics — baseline maturity gauges

These are queried from the database and updated alongside the other
state gauges in `SyncStateMetrics()`.

| Metric | Type | Description |
|--------|------|-------------|
| `spt_baselines_total` | gauge | Total product keys with a baseline record (sample_count >= 5) |
| `spt_baselines_cold` | gauge | Baselines with 5–9 samples (exist but not trusted for pricing) |
| `spt_baselines_warm` | gauge | Baselines with 10+ samples (actively used for price scoring) |
| `spt_product_keys_no_baseline` | gauge | Distinct product keys in listings that have no baseline record |

### New metrics — scoring cold start counters

These are incremented at score time in `ScoreListing()` to track the
ratio of real vs. neutral price scores over time.

| Metric | Type | Description |
|--------|------|-------------|
| `spt_scoring_with_baseline_total` | counter | Listings scored using a warm baseline (>= 10 samples) |
| `spt_scoring_cold_start_total` | counter | Listings scored with neutral 50 price factor (no baseline or < 10 samples) |

The ratio `cold_start / (cold_start + with_baseline)` gives a real-time
"baseline coverage percentage." As the system matures and accumulates
sold data, this ratio should trend toward zero.

### Implementation

**Store additions** (`internal/store/store.go` + `postgres.go`):
- `CountBaselinesByMaturity(ctx) (cold int, warm int, err error)` — single
  query with conditional counting:
  ```sql
  SELECT
      COUNT(*) FILTER (WHERE sample_count < 10) AS cold,
      COUNT(*) FILTER (WHERE sample_count >= 10) AS warm
  FROM price_baselines
  ```
- `CountProductKeysWithoutBaseline(ctx) (int, error)` — count distinct
  product keys in listings that have no matching baseline:
  ```sql
  SELECT COUNT(DISTINCT l.product_key)
  FROM listings l
  LEFT JOIN price_baselines b ON l.product_key = b.product_key
  WHERE l.product_key != ''
    AND b.product_key IS NULL
  ```

**Metrics registration** (`internal/metrics/metrics.go`):
- Add 4 gauge variables and 2 counter variables.

**Engine integration** (`internal/engine/engine.go`):
- Add baseline maturity queries to `SyncStateMetrics()`.

**Scoring integration** (`internal/engine/score.go`):
- After the `score.Score()` call, check whether a warm baseline was
  used and increment the appropriate counter:
  ```go
  if scorerBaseline != nil && scorerBaseline.SampleCount >= 10 {
      metrics.ScoringWithBaselineTotal.Inc()
  } else {
      metrics.ScoringColdStartTotal.Inc()
  }
  ```

**Files:**
- `internal/store/store.go` — add count methods to interface
- `internal/store/postgres.go` — implement queries
- `internal/store/queries.go` — add SQL constants
- `internal/metrics/metrics.go` — register new metrics
- `internal/engine/engine.go` — update `SyncStateMetrics()`
- `internal/engine/score.go` — add cold start counters

**Tests:**
- Store: table-driven tests for `CountBaselinesByMaturity` and
  `CountProductKeysWithoutBaseline`.
- Engine: test `SyncStateMetrics()` sets baseline gauge values.
- Score: test that the correct counter increments for warm vs. cold
  baselines.

## Phase 5 — Dashboard and Recording Rule Updates

### New dashboard panels

**Overview row — add:**
- `Active Watches` stat panel — query `spt_watches_enabled`
- `Total Listings` stat panel — query `spt_listings_total`
- `Pending Alerts` stat panel — query `spt_alerts_pending` (yellow > 0,
  red > 10)

**Ingestion row — add:**
- `Last Ingestion` stat panel — query
  `time() - spt_ingestion_last_success_timestamp` formatted as duration
  (e.g., "12 min ago")
- `Next Ingestion` stat panel — query
  `spt_scheduler_next_ingestion_timestamp - time()` formatted as duration

**Extraction row — fix:**
- Extraction Duration panel should now have data after Phase 1.

**Scoring row — add:**
- `Baseline Coverage` stat panel — query
  `spt_baselines_warm / (spt_baselines_warm + spt_baselines_cold + spt_product_keys_no_baseline) * 100`
  formatted as percentage. Thresholds: green >= 80%, yellow >= 50%,
  red < 50%.
- `Baseline Maturity` bar gauge or stat panel — three values:
  - Warm (10+ samples): `spt_baselines_warm`
  - Cold (5–9 samples): `spt_baselines_cold`
  - No baseline: `spt_product_keys_no_baseline`
- `Cold Start Rate` timeseries — query
  `rate(spt_scoring_cold_start_total[5m]) / (rate(spt_scoring_cold_start_total[5m]) + rate(spt_scoring_with_baseline_total[5m])) * 100`
  formatted as percentage. Shows the real-time ratio of listings being
  scored without price data.

**Alerts row — add:**
- `Last Notification` stat panel — query
  `time() - spt_notification_last_success_timestamp` formatted as
  duration
- `Notification Latency (p95)` timeseries —
  `histogram_quantile(0.95, rate(spt_notification_duration_seconds_bucket[5m]))`

### Panel legend cleanup

Several panels currently show duplicate legend entries because they
query both a recording rule and the raw metric. Review and fix:
- API Calls Rate: shows "calls/s" twice
- Listings / min: shows "listings/min" twice
- Errors / min: shows "errors/min" twice
- Extraction Failures: shows "failures/s" twice
- Alerts Fired Rate: shows "alerts/s" twice

For each, determine whether the panel should use only the recording rule
or only the raw metric, and remove the duplicate query.

### New recording rules

| Rule | Expression |
|------|-----------|
| `spt:notification_duration:p95_5m` | `histogram_quantile(0.95, sum(rate(spt_notification_duration_seconds_bucket[5m])) by (le))` |

### New alert rules

| Alert | Expression | For | Severity |
|-------|-----------|-----|----------|
| `SptNoIngestionRecent` | `time() - spt_ingestion_last_success_timestamp > 1800` | 5m | warning |
| `SptPendingAlertBacklog` | `spt_alerts_pending > 50` | 10m | warning |
| `SptNotificationSilence` | `time() - spt_notification_last_success_timestamp > 86400` | 5m | warning |
| `SptLowBaselineCoverage` | `spt_baselines_warm / (spt_baselines_warm + spt_baselines_cold + spt_product_keys_no_baseline) < 0.5` | 30m | info |

**Files:**
- `tools/dashgen/panels/*.go` — new panels and legend fixes
- `tools/dashgen/panels/overview.go` — overview row additions
- `tools/dashgen/rules/alerts.go` — new alert rules
- `tools/dashgen/rules/recording.go` — new recording rule
- `tools/dashgen/config.go` — add new metrics to `KnownMetrics`
- `tools/dashgen/main.go` — wire new panels into rows
- `deploy/` — regenerated artifacts

## Phase 6 — Cleanup and Deprecation Removal

### Remove `spt_ebay_daily_usage` completely

If Phase 1b only deprecated the metric in code, this phase removes it
entirely:
- Delete any remaining references
- Ensure no dashboards, alerts, or recording rules reference it
- Remove from `KnownMetrics` if still present

### Unused `last_polled_at` column

The `watches.last_polled_at` column exists in the database schema
(`migrations/001_initial_schema.sql`) but is never read or written by
application code. Options:
- Add a migration to drop it (breaking change if external tools use it)
- Start populating it during ingestion and expose as a metric
  (`spt_watch_last_polled_timestamp` with `watch` label)

Recommend: start populating it — it would be useful for debugging
stale watches. However, this is lower priority and can be deferred.

## Verification

After each phase:

```bash
make test && make lint
```

After Phase 5 (dashboard changes):

```bash
cd tools/dashgen && go run . && cd ../..
make test  # staleness test catches drift
```

Deploy and verify:
- All new metrics appear on `/metrics` endpoint
- Dashboard panels show data (not "No data")
- No duplicate legend entries
- Alert rules evaluate correctly in Prometheus UI

## Summary

| Phase | Scope | New Metrics |
|-------|-------|-------------|
| 1 | Fix broken + remove deprecated | 0 (fix existing) |
| 2 | System state gauges | 10 |
| 3 | Notification metrics | 4 |
| 4 | Baseline coverage + cold start | 6 |
| 5 | Dashboard + rules | 0 (visualization) |
| 6 | Cleanup | 0 (removal) |
| **Total** | | **20 new metrics** |
