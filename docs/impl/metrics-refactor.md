# Implementation: Metrics Refactor

## Context

The core Prometheus metrics work (eBay Analytics API quota in PR #22, job
label filters in PR #21). However, an audit of the current dashboard and
codebase reveals several gaps:

- `spt_extraction_duration_seconds` is registered but never observed.
- `spt_ebay_daily_usage` is deprecated but still populated.
- No gauges for system state (watches, listings, pending alerts, scheduler).
- No notification latency or last-sent timestamps.
- No baseline coverage or cold-start visibility.
- Several dashboard panels show duplicate legend entries from overlapping
  recording rule and raw metric queries.

See `docs/plans/metrics-refactor.md` for the full plan, rationale, and
dashboard observations.

---

## Phase 1: Fix Broken and Deprecated Metrics

### Tasks

- [x] **1a. Instrument `spt_extraction_duration_seconds`**
  - In `internal/engine/engine.go`, in `processListing()` (around line
    230), wrap the `eng.extractor.ClassifyAndExtract()` call with a
    timer:
    ```go
    start := time.Now()
    ct, attrs, extractErr := eng.extractor.ClassifyAndExtract(ctx, listing.Title, nil)
    metrics.ExtractionDuration.Observe(time.Since(start).Seconds())
    ```
  - Observe on both success and failure paths (the `Observe` call goes
    before the error check so LLM latency is always recorded).
- [x] **1a. Tests**
  - Add a test in `internal/engine/engine_test.go` that calls
    `processListing()` via the public ingestion path (or a test helper)
    and asserts `ptestutil.ToFloat64(metrics.ExtractionDuration)` has a
    sample count > 0 after the call. Use the delta pattern (capture
    before, assert after) since Prometheus globals persist across tests.
- [x] **1b. Remove `spt_ebay_daily_usage`**
  - In `internal/ebay/browse.go` (line 98), remove the
    `metrics.EbayDailyUsage.Set(float64(c.rateLimiter.DailyCount()))`
    call.
  - In `internal/metrics/metrics.go`, remove the `EbayDailyUsage`
    variable declaration (lines ~99-104). Remove the entire var entry.
  - In `tools/dashgen/config.go`, confirm `spt_ebay_daily_usage` is
    already absent from `KnownMetrics` (it was removed in PR #22
    Phase 5). If still present, remove it.
- [x] **1b. Tests**
  - Search for any test references to `EbayDailyUsage` or
    `spt_ebay_daily_usage` and remove them.
  - `make mocks` to regenerate mocks if the metrics import changed.
- [x] Run `make test && make lint` — all pass

### Success Criteria

- Extraction Duration dashboard panel populates with data when
  extraction runs.
- `spt_ebay_daily_usage` no longer appears in `/metrics` output.
- `make test` and `make lint` pass with zero issues.

### Files

- `internal/engine/engine.go`
- `internal/engine/engine_test.go`
- `internal/ebay/browse.go`
- `internal/metrics/metrics.go`
- `tools/dashgen/config.go` (verify only)

---

## Phase 2: System State Gauges

### Tasks

#### 2a. Register new metrics

- [x] In `internal/metrics/metrics.go`, add two new `var` blocks after
  the alert metrics section (after line 145):

  **System state metrics:**
  ```go
  // System state metrics.
  var (
      WatchesTotal = promauto.NewGauge(prometheus.GaugeOpts{
          Namespace: namespace,
          Name:      "watches_total",
          Help:      "Total number of watches.",
      })
      WatchesEnabled = promauto.NewGauge(prometheus.GaugeOpts{
          Namespace: namespace,
          Name:      "watches_enabled",
          Help:      "Number of enabled watches.",
      })
      ListingsTotal = promauto.NewGauge(prometheus.GaugeOpts{
          Namespace: namespace,
          Name:      "listings_total",
          Help:      "Total listings in the database.",
      })
      ListingsUnextracted = promauto.NewGauge(prometheus.GaugeOpts{
          Namespace: namespace,
          Name:      "listings_unextracted",
          Help:      "Listings without LLM extraction.",
      })
      ListingsUnscored = promauto.NewGauge(prometheus.GaugeOpts{
          Namespace: namespace,
          Name:      "listings_unscored",
          Help:      "Listings without a computed score.",
      })
      AlertsPending = promauto.NewGauge(prometheus.GaugeOpts{
          Namespace: namespace,
          Name:      "alerts_pending",
          Help:      "Alerts not yet sent as notifications.",
      })
  )
  ```

  **Scheduler timestamp metrics:**
  ```go
  // Scheduler metrics.
  var (
      SchedulerNextIngestionTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
          Namespace: namespace,
          Name:      "scheduler_next_ingestion_timestamp",
          Help:      "Unix epoch of the next scheduled ingestion run.",
      })
      SchedulerNextBaselineTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
          Namespace: namespace,
          Name:      "scheduler_next_baseline_timestamp",
          Help:      "Unix epoch of the next scheduled baseline refresh.",
      })
      IngestionLastSuccessTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
          Namespace: namespace,
          Name:      "ingestion_last_success_timestamp",
          Help:      "Unix epoch of the last successful ingestion cycle.",
      })
      BaselineLastRefreshTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
          Namespace: namespace,
          Name:      "baseline_last_refresh_timestamp",
          Help:      "Unix epoch of the last successful baseline refresh.",
      })
  )
  ```

#### 2b. Add Store count methods

- [x] In `internal/store/queries.go`, add SQL constants after the
  listing queries block (after line ~98):
  ```sql
  queryCountListings            = "SELECT COUNT(*) FROM listings"
  queryCountUnextractedListings = "SELECT COUNT(*) FROM listings WHERE component_type IS NULL"
  queryCountUnscoredListings    = "SELECT COUNT(*) FROM listings WHERE component_type IS NOT NULL AND score IS NULL"
  ```
  These WHERE clauses match the existing `queryListUnextractedListings`
  and `queryListUnscoredListings` queries exactly.
  After the watch queries block (after line ~150):
  ```sql
  queryCountWatches = "SELECT COUNT(*) AS total, COUNT(*) FILTER (WHERE enabled = true) AS enabled FROM watches"
  ```
  After the alert queries block (after line ~204):
  ```sql
  queryCountPendingAlerts = "SELECT COUNT(*) FROM alerts WHERE notified = false"
  ```
- [x] In `internal/store/store.go`, add count methods to the `Store`
  interface. Group them under a new `// Counts` section comment before
  `// Migrations`:
  ```go
  // Counts
  CountWatches(ctx context.Context) (total int, enabled int, err error)
  CountListings(ctx context.Context) (int, error)
  CountUnextractedListings(ctx context.Context) (int, error)
  CountUnscoredListings(ctx context.Context) (int, error)
  CountPendingAlerts(ctx context.Context) (int, error)
  ```
- [x] In `internal/store/postgres.go`, implement each method using
  `s.pool.QueryRow(ctx, queryXxx).Scan(&count)`. For `CountWatches`,
  scan into two variables (`&total, &enabled`). Follow the existing
  error wrapping pattern: `fmt.Errorf("counting xxx: %w", err)`.
- [x] Run `make mocks` to regenerate `MockStore` with new methods.

#### 2c. Add `SyncStateMetrics()` to Engine

- [x] In `internal/engine/engine.go`, add a new method after
  `SyncQuota()`:
  ```go
  func (eng *Engine) SyncStateMetrics(ctx context.Context) {
  ```
  This method:
  - Calls each store count method.
  - Sets the corresponding gauge: `metrics.WatchesTotal.Set(float64(total))`, etc.
  - Logs errors at Warn level but does not return them (best-effort,
    same pattern as `SyncQuota`).
- [x] Call `eng.SyncStateMetrics(ctx)` at the end of `RunIngestion()`
  after `eng.SyncQuota(ctx)` (around line 175).
- [x] Call `eng.SyncStateMetrics(context.Background())` on startup in
  `buildEngine()` in `serve.go`, after the existing
  `eng.SyncQuota(context.Background())` call (around line 300).

#### 2d. Add scheduler timestamp updates

- [x] In `internal/engine/scheduler.go`, add imports for
  `"github.com/donaldgifford/server-price-tracker/internal/metrics"`
  and `"time"`.
- [x] Add `ingestionEntryID` and `baselineEntryID` fields (type
  `cron.EntryID`) to the `Scheduler` struct. In `NewScheduler()`,
  capture the return values from `c.AddFunc()` instead of discarding
  with `_`:
  ```go
  ingestionID, err := c.AddFunc(...)
  // ...
  baselineID, err := c.AddFunc(...)
  // ...
  s.ingestionEntryID = ingestionID
  s.baselineEntryID = baselineID
  ```
- [x] In `runIngestion()`, after a successful `RunIngestion()` call
  (no error), set:
  ```go
  metrics.IngestionLastSuccessTimestamp.Set(float64(time.Now().Unix()))
  ```
- [x] In `runBaselineRefresh()`, after a successful
  `RunBaselineRefresh()` call (no error), set:
  ```go
  metrics.BaselineLastRefreshTimestamp.Set(float64(time.Now().Unix()))
  ```
- [x] Add a `SyncNextRunTimestamps()` method on `Scheduler` that looks
  up entries by stored ID:
  ```go
  func (s *Scheduler) SyncNextRunTimestamps() {
      ingestion := s.cron.Entry(s.ingestionEntryID)
      metrics.SchedulerNextIngestionTimestamp.Set(float64(ingestion.Next.Unix()))
      baseline := s.cron.Entry(s.baselineEntryID)
      metrics.SchedulerNextBaselineTimestamp.Set(float64(baseline.Next.Unix()))
  }
  ```
- [x] Call `SyncNextRunTimestamps()` from:
  - After `scheduler.Start()` in `serve.go` (to set initial values).
  - At the end of `runIngestion()` and `runBaselineRefresh()` (to
    update after each run).

#### 2e. Tests

- [x] **Store tests** — in `internal/store/postgres_test.go` (or a new
  `internal/store/count_test.go`), add table-driven tests:
  - `TestCountWatches`: insert 3 watches (2 enabled, 1 disabled),
    verify total=3, enabled=2.
  - `TestCountListings`: insert 2 listings, verify count=2.
  - `TestCountUnextractedListings`: insert 3 listings (1 with
    `component_type=""`, 1 with `component_type=NULL`, 1 extracted),
    verify count=2.
  - `TestCountUnscoredListings`: insert 2 listings (1 scored, 1 with
    `score=NULL`), verify count=1.
  - `TestCountPendingAlerts`: insert 3 alerts (2 pending, 1 notified),
    verify count=2.
  - Note: these tests may require integration test tags if they hit a
    real database. If unit-only, test the SQL query building or use
    mock expectations.
- [x] **Engine tests** — in `internal/engine/engine_test.go`:
  - `TestSyncStateMetrics_SetsAllGauges`: set up mock store to return
    known counts, call `SyncStateMetrics()`, assert all 6 gauges match
    using `ptestutil.ToFloat64()`. Use delta pattern.
  - `TestSyncStateMetrics_StoreErrorDoesNotPanic`: set up mock store
    to return errors, call `SyncStateMetrics()`, verify no panic and
    other gauges still set.
- [x] **Scheduler tests** — in `internal/engine/scheduler_test.go`:
  - `TestScheduler_SyncNextRunTimestamps`: create a scheduler, call
    `SyncNextRunTimestamps()`, verify both timestamp gauges are > 0.
  - `TestScheduler_IngestionSetsLastSuccessTimestamp`: run
    `runIngestion()` with a mock engine that succeeds, verify
    `IngestionLastSuccessTimestamp` gauge is set.
- [x] Run `make test && make lint` — all pass

### Success Criteria

- All 10 new gauges appear in `/metrics` output with correct values.
- After an ingestion cycle, state gauges reflect current DB counts.
- Scheduler timestamp gauges update after each job runs.
- `make test` and `make lint` pass.

### Files

- `internal/metrics/metrics.go`
- `internal/store/store.go`
- `internal/store/queries.go`
- `internal/store/postgres.go`
- `internal/store/mocks/mock_store.go` (regenerated)
- `internal/engine/engine.go`
- `internal/engine/engine_test.go`
- `internal/engine/scheduler.go`
- `internal/engine/scheduler_test.go`
- `cmd/server-price-tracker/cmd/serve.go`

---

## Phase 3: Notification and Alert Metrics

### Tasks

#### 3a. Register new metrics

- [x] In `internal/metrics/metrics.go`, add to the alert metrics
  section (or create a new `// Notification metrics.` block):
  ```go
  NotificationDuration = promauto.NewHistogram(prometheus.HistogramOpts{
      Namespace: namespace,
      Name:      "notification_duration_seconds",
      Help:      "Discord webhook HTTP POST latency in seconds.",
      Buckets:   []float64{0.1, 0.25, 0.5, 1, 2, 5, 10},
  })

  NotificationLastSuccessTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
      Namespace: namespace,
      Name:      "notification_last_success_timestamp",
      Help:      "Unix epoch of the last successful notification delivery.",
  })

  NotificationLastFailureTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
      Namespace: namespace,
      Name:      "notification_last_failure_timestamp",
      Help:      "Unix epoch of the last notification delivery failure.",
  })

  AlertsFiredByWatch = promauto.NewCounterVec(prometheus.CounterOpts{
      Namespace: namespace,
      Name:      "alerts_fired_by_watch",
      Help:      "Alerts fired broken down by watch name. Cardinality bounded by number of watches (typically <20).",
  }, []string{"watch"})
  ```

#### 3b. Add notification duration timing

- [x] In `internal/notify/discord.go`, in the `post()` method (line
  139), add timing around the HTTP call:
  ```go
  start := time.Now()
  resp, err := d.client.Do(req)
  metrics.NotificationDuration.Observe(time.Since(start).Seconds())
  ```
  This requires adding an import for
  `"github.com/donaldgifford/server-price-tracker/internal/metrics"`
  and `"time"` to discord.go.
  Observe on both success and failure (measures HTTP round-trip time
  regardless of response status).

#### 3c. Add notification timestamps and per-watch counter

- [x] In `internal/engine/alert.go`, in `sendSingle()` (around line
  96), after `metrics.AlertsFiredTotal.Inc()`, add:
  ```go
  metrics.NotificationLastSuccessTimestamp.Set(float64(time.Now().Unix()))
  metrics.AlertsFiredByWatch.WithLabelValues(watch.Name).Inc()
  ```
- [x] In `sendBatch()` (around line 128), after
  `metrics.AlertsFiredTotal.Add(...)`, add:
  ```go
  metrics.NotificationLastSuccessTimestamp.Set(float64(time.Now().Unix()))
  metrics.AlertsFiredByWatch.WithLabelValues(watch.Name).Add(float64(len(alertIDs)))
  ```
- [x] In `ProcessAlerts()` (around line 42), in the error branch where
  `metrics.NotificationFailuresTotal.Inc()` is called, add:
  ```go
  metrics.NotificationLastFailureTimestamp.Set(float64(time.Now().Unix()))
  ```
- [x] Add `"time"` import to `alert.go` if not already present.

#### 3d. Tests

- [x] **Discord notifier tests** — in
  `internal/notify/discord_test.go`:
  - `TestSendAlert_ObservesNotificationDuration`: capture histogram
    sample count before, call `SendAlert()` with a test HTTP server,
    assert sample count increased by 1.
- [x] **Alert processing tests** — in
  `internal/engine/alert_test.go`:
  - `TestProcessAlerts_SetsSuccessTimestamp`: process a single alert
    successfully, verify `NotificationLastSuccessTimestamp` gauge > 0.
  - `TestProcessAlerts_SetsFailureTimestamp`: mock notifier returns
    error, verify `NotificationLastFailureTimestamp` gauge > 0.
  - `TestProcessAlerts_IncrementsAlertsFiredByWatch`: process alerts
    for a watch named "test-watch", verify
    `AlertsFiredByWatch.WithLabelValues("test-watch")` counter value.
- [x] Run `make test && make lint` — all pass

### Success Criteria

- `spt_notification_duration_seconds` histogram populates when Discord
  webhooks fire.
- Success/failure timestamp gauges update on each notification attempt.
- Per-watch counter tracks which watches generate alerts.
- `make test` and `make lint` pass.

### Files

- `internal/metrics/metrics.go`
- `internal/notify/discord.go`
- `internal/notify/discord_test.go`
- `internal/engine/alert.go`
- `internal/engine/alert_test.go`

---

## Phase 4: Baseline Coverage and Cold Start Metrics

### Tasks

#### 4a. Register new metrics

- [x] In `internal/metrics/metrics.go`, add a new `// Baseline metrics.`
  block:
  ```go
  BaselinesTotal = promauto.NewGauge(...)       // "baselines_total"
  BaselinesCold  = promauto.NewGauge(...)       // "baselines_cold"
  BaselinesWarm  = promauto.NewGauge(...)       // "baselines_warm"
  ProductKeysNoBaseline = promauto.NewGauge(...) // "product_keys_no_baseline"
  ```
- [x] In the `// Scoring metrics.` block, add two counters:
  ```go
  ScoringWithBaselineTotal = promauto.NewCounter(...) // "scoring_with_baseline_total"
  ScoringColdStartTotal    = promauto.NewCounter(...) // "scoring_cold_start_total"
  ```

#### 4b. Add Store count methods for baselines

- [x] In `internal/store/queries.go`, add to the baseline queries
  section:
  ```sql
  queryCountBaselinesByMaturity = `SELECT
      COUNT(*) FILTER (WHERE sample_count < 10) AS cold,
      COUNT(*) FILTER (WHERE sample_count >= 10) AS warm
  FROM price_baselines`

  queryCountProductKeysWithoutBaseline = `SELECT COUNT(DISTINCT l.product_key)
  FROM listings l
  LEFT JOIN price_baselines b ON l.product_key = b.product_key
  WHERE l.product_key != '' AND l.product_key IS NOT NULL
    AND b.product_key IS NULL`
  ```
- [x] In `internal/store/store.go`, add to the `// Counts` section
  (created in Phase 2):
  ```go
  CountBaselinesByMaturity(ctx context.Context) (cold int, warm int, err error)
  CountProductKeysWithoutBaseline(ctx context.Context) (int, error)
  ```
- [x] In `internal/store/postgres.go`, implement both methods:
  - `CountBaselinesByMaturity`: `QueryRow` scanning into `&cold, &warm`.
  - `CountProductKeysWithoutBaseline`: `QueryRow` scanning into `&count`.
- [x] Run `make mocks` to regenerate `MockStore`.

#### 4c. Update `SyncStateMetrics()` for baseline gauges

- [x] In `internal/engine/engine.go`, extend `SyncStateMetrics()` to
  call the two new store methods and set:
  ```go
  metrics.BaselinesCold.Set(float64(cold))
  metrics.BaselinesWarm.Set(float64(warm))
  metrics.BaselinesTotal.Set(float64(cold + warm))
  metrics.ProductKeysNoBaseline.Set(float64(noBaseline))
  ```

#### 4d. Extract cold start threshold constant

- [x] In `pkg/scorer/scorer.go`, extract the magic number `10` on line
  72 into an exported constant:
  ```go
  // MinBaselineSamples is the minimum number of sold samples required
  // for a baseline to be used in price scoring. Below this threshold,
  // the price factor defaults to a neutral 50.
  const MinBaselineSamples = 10
  ```
  Update the existing `if baseline != nil && baseline.SampleCount >= 10`
  to use `MinBaselineSamples`.
- [x] Update `pkg/scorer/scorer_test.go` to reference
  `MinBaselineSamples` in the insufficient baseline test case comment
  (optional — improves clarity but not required).

#### 4e. Add cold start counters to scoring

- [x] In `internal/engine/score.go`, in `ScoreListing()` (around line
  55, after `score.Score()` returns and before `UpdateScore()`), add:
  ```go
  if scorerBaseline != nil && scorerBaseline.SampleCount >= score.MinBaselineSamples {
      metrics.ScoringWithBaselineTotal.Inc()
  } else {
      metrics.ScoringColdStartTotal.Inc()
  }
  ```
  Uses the exported constant from `pkg/scorer` to avoid duplicating
  the threshold value.

#### 4f. Tests

- [x] **Store tests**:
  - Deferred to integration test pass (SQL correctness validated via
    mock expectations in engine tests).
- [x] **Engine tests**:
  - Extended `TestSyncStateMetrics_SetsAllGauges` with mock returns for
    `CountBaselinesByMaturity` and `CountProductKeysWithoutBaseline`.
    Verifies all 4 baseline gauges (BaselinesCold, BaselinesWarm,
    BaselinesTotal, ProductKeysNoBaseline).
  - Updated `TestSyncStateMetrics_StoreErrorDoesNotPanic` with baseline
    error expectations.
- [x] **Score tests**:
  - `TestScoreListing_IncrementsWarmBaselineCounter`: score a listing
    with a baseline that has `SampleCount >= 10`, verify
    `ScoringWithBaselineTotal` increments.
  - `TestScoreListing_IncrementsColdStartCounter`: score a listing
    with no baseline (or `SampleCount < 10`), verify
    `ScoringColdStartTotal` increments.
- [x] Run `make test && make lint` — all pass

### Success Criteria

- Baseline maturity gauges reflect the actual database state.
- Cold start counters increment during scoring.
- The ratio `cold / (cold + warm)` is computable from `/metrics`.
- `make test` and `make lint` pass.

### Files

- `internal/metrics/metrics.go`
- `internal/store/store.go`
- `internal/store/queries.go`
- `internal/store/postgres.go`
- `internal/store/mocks/mock_store.go` (regenerated)
- `internal/engine/engine.go`
- `internal/engine/engine_test.go`
- `internal/engine/score.go`
- `internal/engine/score_test.go`
- `pkg/scorer/scorer.go`
- `pkg/scorer/scorer_test.go` (optional update)

---

## Phase 5: Dashboard and Recording Rule Updates

### Tasks

#### 5a. Add new metrics to `KnownMetrics`

- [ ] In `tools/dashgen/config.go`, add all new metric names to
  `KnownMetrics`:
  ```
  // System state.
  spt_watches_total
  spt_watches_enabled
  spt_listings_total
  spt_listings_unextracted
  spt_listings_unscored
  spt_alerts_pending

  // Scheduler.
  spt_scheduler_next_ingestion_timestamp
  spt_scheduler_next_baseline_timestamp
  spt_ingestion_last_success_timestamp
  spt_baseline_last_refresh_timestamp

  // Notification.
  spt_notification_duration_seconds
  spt_notification_last_success_timestamp
  spt_notification_last_failure_timestamp
  spt_alerts_fired_by_watch

  // Baseline coverage.
  spt_baselines_total
  spt_baselines_cold
  spt_baselines_warm
  spt_product_keys_no_baseline
  spt_scoring_with_baseline_total
  spt_scoring_cold_start_total

  // Recording rules.
  spt:notification_duration:p95_5m
  ```

#### 5b. Add new dashboard panels

- [ ] **Overview row** — in `tools/dashgen/panels/overview.go`, add:
  - `ActiveWatchesStat()` — stat panel, query `spt_watches_enabled`,
    Span(StatWidth), ThresholdsGreenOnly.
  - `TotalListingsStat()` — stat panel, query `spt_listings_total`,
    Span(StatWidth), ThresholdsGreenOnly.
  - `PendingAlertsStat()` — stat panel, query `spt_alerts_pending`,
    Span(StatWidth), ThresholdsGreenYellowRed(1, 10).
- [ ] **Ingestion row** — in `tools/dashgen/panels/ingestion.go`, add:
  - `LastIngestion()` — stat panel, query
    `time() - spt_ingestion_last_success_timestamp{job="server-price-tracker"}`,
    unit `s`, Span(StatWidth).
  - `NextIngestion()` — stat panel, query
    `spt_scheduler_next_ingestion_timestamp{job="server-price-tracker"} - time()`,
    unit `s`, Span(StatWidth).
- [ ] **Scoring row** — in `tools/dashgen/panels/scoring.go`, add:
  - `BaselineCoverage()` — stat panel, query
    `spt_baselines_warm / (spt_baselines_warm + spt_baselines_cold + spt_product_keys_no_baseline) * 100`,
    unit `percent`, ThresholdsGreenYellowRed with green >= 80,
    yellow >= 50.
  - `BaselineMaturity()` — stat or bar gauge panel showing warm, cold,
    and no-baseline counts as separate queries with legend labels.
  - `ColdStartRate()` — timeseries panel, query
    `rate(spt_scoring_cold_start_total[5m]) / (rate(spt_scoring_cold_start_total[5m]) + rate(spt_scoring_with_baseline_total[5m])) * 100`,
    unit `percent`, FillOpacity(10).
- [ ] **Alerts row** — in `tools/dashgen/panels/alerts.go`, add:
  - `LastNotification()` — stat panel, query
    `time() - spt_notification_last_success_timestamp{job="server-price-tracker"}`,
    unit `s`, Span(StatWidth).
  - `NotificationLatency()` — timeseries panel, query
    `histogram_quantile(0.95, rate(spt_notification_duration_seconds_bucket{job="server-price-tracker"}[5m]))`,
    unit `s`.

#### 5c. Wire new panels into dashboard rows

- [ ] In `tools/dashgen/dashboards/overview.go`, update `BuildOverview()`:
  - Overview row: add `ActiveWatchesStat()`, `TotalListingsStat()`,
    `PendingAlertsStat()` (adjust Span widths so the row totals 24).
  - Ingestion row: add `LastIngestion()`, `NextIngestion()`.
  - Scoring row: add `BaselineCoverage()`, `BaselineMaturity()`,
    `ColdStartRate()`.
  - Alerts row: add `LastNotification()`, `NotificationLatency()`.

#### 5d. Fix duplicate legend entries

  Each affected panel only has a single `WithTarget()` call. The
  duplicate legends are caused by Prometheus returning multiple time
  series when recording rules use bare `rate()` without `sum()`, or
  when raw queries match multiple label combinations. Fix by wrapping
  in `sum()`:

- [ ] In `tools/dashgen/rules/recording.go`, update 4 recording rules
  to use `sum(rate(...))` instead of bare `rate(...)`:
  - `spt:ingestion_listings:rate5m` → `sum(rate(spt_ingestion_listings_total[5m]))`
  - `spt:ingestion_errors:rate5m` → `sum(rate(spt_ingestion_errors_total[5m]))`
  - `spt:extraction_failures:rate5m` → `sum(rate(spt_extraction_failures_total[5m]))`
  - `spt:ebay_api_calls:rate5m` → `sum(rate(spt_ebay_api_calls_total[5m]))`
  - Note: `spt:http_requests:rate5m` and `spt:http_errors:rate5m`
    already use `sum()`.
- [ ] In `tools/dashgen/panels/alerts.go`, update `AlertsRate()` query
  to wrap in `sum()`:
  `sum(rate(spt_alerts_fired_total{job="server-price-tracker"}[5m]))`

#### 5e. Add new recording rule

- [ ] In `tools/dashgen/rules/recording.go`, add:
  ```go
  {Record: "spt:notification_duration:p95_5m", Expr: `histogram_quantile(0.95, sum(rate(spt_notification_duration_seconds_bucket[5m])) by (le))`},
  ```

#### 5f. Add new alert rules

- [ ] In `tools/dashgen/rules/alerts.go`, add 4 new rules:
  - `SptNoIngestionRecent`:
    expr `time() - spt_ingestion_last_success_timestamp > 1800`,
    for `5m`, severity `warning`,
    description "No successful ingestion cycle in the last 30 minutes."
  - `SptPendingAlertBacklog`:
    expr `spt_alerts_pending > 50`,
    for `10m`, severity `warning`,
    description "More than 50 alerts are pending notification delivery."
  - `SptNotificationSilence`:
    expr `time() - spt_notification_last_success_timestamp > 86400`,
    for `5m`, severity `warning`,
    description "No successful notification has been sent in 24 hours."
  - `SptLowBaselineCoverage`:
    expr `spt_baselines_warm / (spt_baselines_warm + spt_baselines_cold + spt_product_keys_no_baseline) < 0.5`,
    for `30m`, severity `info`,
    description "Fewer than 50% of product keys have warm baselines."

#### 5g. Regenerate artifacts and test

- [ ] Run `cd tools/dashgen && go run .` to regenerate:
  - `deploy/grafana/data/spt-overview.json`
  - `deploy/prometheus/spt-recording-rules.yaml`
  - `deploy/prometheus/spt-alerts.yaml`
- [ ] Update `dashgen_test.go` panel count assertion to match the new
  total (currently 19 — add the count of new panels).
- [ ] Run `make test && make lint` — all pass (including the staleness
  test that compares generated artifacts against committed files).

### Success Criteria

- Dashboard shows new panels with data in all rows.
- No duplicate legend entries in any timeseries panel.
- New alert rules appear in `spt-alerts.yaml`.
- New recording rule appears in `spt-recording-rules.yaml`.
- All dashgen tests pass (including staleness check).
- `make test` and `make lint` pass.

### Files

- `tools/dashgen/config.go`
- `tools/dashgen/panels/overview.go`
- `tools/dashgen/panels/ingestion.go`
- `tools/dashgen/panels/scoring.go`
- `tools/dashgen/panels/alerts.go`
- `tools/dashgen/dashboards/overview.go`
- `tools/dashgen/rules/recording.go`
- `tools/dashgen/rules/alerts.go`
- `tools/dashgen/dashgen_test.go`
- `deploy/grafana/data/spt-overview.json` (regenerated)
- `deploy/prometheus/spt-recording-rules.yaml` (regenerated)
- `deploy/prometheus/spt-alerts.yaml` (regenerated)

---

## Phase 6: Cleanup

### Tasks

- [ ] **Verify `spt_ebay_daily_usage` is fully removed** — grep the
  entire codebase for `EbayDailyUsage`, `ebay_daily_usage`, and
  `daily_usage`. Confirm zero references remain outside of git history
  or docs.
- [ ] **Verify `KnownMetrics` is complete** — compare the metrics
  registered in `internal/metrics/metrics.go` against the entries in
  `tools/dashgen/config.go`. Every non-deprecated metric should be
  present.
- [ ] **Verify all new metrics populate** — deploy locally, trigger an
  ingestion cycle, and verify on `/metrics`:
  - All state gauges show non-zero values (assuming data exists).
  - Timestamp gauges are recent Unix epochs.
  - Histogram buckets appear for extraction and notification durations.
  - Cold start counters increment during scoring.
- [ ] Run `make test && make lint` — final confirmation

### Success Criteria

- No references to deprecated metrics remain in application code.
- `KnownMetrics` matches the full set of registered metrics.
- All new metrics produce data in a running instance.
- `make test` and `make lint` pass.

### Files

- All files from previous phases (verification only)

---

## Open Questions

### Resolved

1. **Unextracted listing detection query** — **Resolved.** The existing
   `queryListUnextractedListings` uses `WHERE component_type IS NULL`
   (queries.go:83). The count query should use the same condition:
   `SELECT COUNT(*) FROM listings WHERE component_type IS NULL`.

2. **Unscored listing detection query** — **Resolved.** The existing
   `queryListUnscoredListings` uses
   `WHERE component_type IS NOT NULL AND score IS NULL` (queries.go:95).
   The count query should use the same condition to count listings that
   have been extracted but not yet scored.

3. **Duplicate legends root cause** — **Resolved.** Each panel only has
   one `WithTarget()` call. The duplicates are caused by Prometheus
   returning multiple time series for recording rules that don't use
   `sum()` — specifically `spt:ingestion_listings:rate5m`,
   `spt:ingestion_errors:rate5m`, `spt:extraction_failures:rate5m`, and
   `spt:ebay_api_calls:rate5m` all use bare `rate()` without `sum()`,
   so if there are multiple instances or label sets, they produce
   separate series. The `AlertsRate` panel uses a raw metric query
   `rate(spt_alerts_fired_total[5m])` which similarly returns one series
   per label combination. Fix by wrapping recording rule expressions in
   `sum()` in recording.go, or by wrapping the panel queries in `sum()`.

4. **Scheduler entry ordering assumption** — **Resolved.** Store the
   `cron.EntryID` returned by `AddFunc()` on the `Scheduler` struct
   (e.g., `ingestionEntryID` and `baselineEntryID` fields). Look up by
   ID in `SyncNextRunTimestamps()` via `s.cron.Entry(id).Next`. Do not
   assume index ordering.

5. **Cold start threshold constant** — **Resolved.** Extract a named
   constant `MinBaselineSamples = 10` in `pkg/scorer/scorer.go` and
   export it. Reference `score.MinBaselineSamples` from
   `internal/engine/score.go` instead of duplicating the magic number.

6. **`spt_alerts_fired_by_watch` cardinality** — **Resolved.** Use
   watch name as the label for dashboard readability. Document the
   cardinality assumption (<20 watches) in the metric Help text. If
   cardinality becomes a problem later, we can filter or switch to
   watch ID.

7. **Store count methods — unit vs. integration tests** — **Resolved.**
   Defer integration tests for store count methods. Unit tests with
   mock store expectations are sufficient for now. SQL correctness can
   be validated via integration tests in a future pass (consider
   datadog-sql or similar for DB-level observability).

8. **`last_polled_at` column** — **Resolved.** Defer to a separate PR.
   The column is unused and populating it adds per-watch UPDATE cost
   during ingestion. Not worth the complexity in this refactor.
