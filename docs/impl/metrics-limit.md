# Implementation: Restart-Resilient eBay Quota Metrics

## Context

The `spt_ebay_daily_usage` gauge resets to 0 on every pod restart because
it's an in-memory counter. Dashboard panels and alert rules that reference
this metric become unreliable across deploys.

The eBay Browse API does **not** return rate limit headers — verified
2026-02-16. However, the eBay Developer Analytics API (`getRateLimits`)
works with our existing app token and returns authoritative quota state
including `count`, `limit`, `remaining`, `reset`, and `timeWindow`.

We only use `item_summary/search` which falls under the `buy.browse`
resource. The `buy.browse.item.bulk` resource has its own separate limit
but we don't use it, so we only track `buy.browse`.

**Polling strategy:** call the Analytics API on startup (to sync after
pod restarts) and after each ingestion cycle (to keep metrics current).
This adds ~96 calls/day which is negligible.

See `docs/plans/metrics-limit.md` for the full plan and test results.

---

## Phase 0: Define New Prometheus Metrics

### Tasks

- [x] Add 3 new gauge metrics to `internal/metrics/metrics.go`:
  - `spt_ebay_rate_limit` (Gauge) — total calls allowed in the current
    window, from Analytics API `rates[].limit`
  - `spt_ebay_rate_remaining` (Gauge) — calls remaining in the current
    window, from Analytics API `rates[].remaining`
  - `spt_ebay_rate_reset_timestamp` (Gauge) — Unix epoch seconds of
    when the quota window resets, from Analytics API `rates[].reset`
  - Group them under a new `// eBay rate limit metrics (from Analytics API).`
    var block after the existing eBay metrics block
- [x] Run `make test` and `make lint` to verify no regressions

### Success Criteria

- `make test` passes
- `make lint` passes
- New metrics are registered and appear in `/metrics` output (as 0 values
  until an Analytics API call is made)

### Files

- `internal/metrics/metrics.go`

---

## Phase 1: Add Analytics API Client

### Tasks

- [x] Add Analytics API URL constant to `internal/ebay/analytics.go`:
  ```
  https://api.ebay.com/developer/analytics/v1_beta/rate_limit/
  ```
- [x] Define response types matching the actual production response
  (unexported types `rateLimitResponse`, `rateLimitEntry`, `resource`,
  `quotaRate` to avoid collision with `golang.org/x/time/rate` package)
- [x] Add `GetBrowseQuota(ctx context.Context) (*QuotaState, error)` on
  `AnalyticsClient` that calls the Analytics API filtered to
  `api_context=buy&api_name=browse`, finds the `buy.browse` resource,
  and returns parsed `QuotaState` with `Count`, `Limit`, `Remaining`,
  `ResetAt`, `TimeWindow`
- [x] Created separate `AnalyticsClient` struct (not added to `EbayClient`
  interface — different concern from Browse API)
- [x] Add tests (9 cases in table-driven test):
  - successful response (verifies all parsed fields)
  - resource not found (only `buy.browse.item.bulk` present)
  - empty rate limits array
  - empty rates array for `buy.browse`
  - 401 unauthorized
  - 500 server error
  - token provider error
  - invalid JSON response
  - malformed reset timestamp
- [x] Run `make test` and `make lint`

### Success Criteria

- Analytics API response is correctly parsed
- `buy.browse` resource is extracted from the response
- API errors are handled gracefully
- All tests pass

### Files

- `internal/ebay/analytics.go` (new)
- `internal/ebay/analytics_test.go` (new)
- `internal/ebay/client.go` (interface update, if needed)

---

## Phase 2: Poll on Startup and After Ingestion

### Tasks

- [x] On startup (in `buildEngine()` in `serve.go`):
  - Call `SyncQuota()` once after engine is created
  - Sets the 3 Prometheus gauge metrics from the response
  - Syncs the in-memory `RateLimiter` state
  - Logs at Debug level on success, Warn on failure
  - If the call fails, logs a warning and continues (doesn't block startup)
- [x] After each ingestion cycle (in `RunIngestion()` in `engine.go`):
  - Calls `SyncQuota()` after alert processing completes
  - Sets the 3 Prometheus gauge metrics
  - Syncs the in-memory `RateLimiter` state
  - Logs at Debug level (runs every 15 min, doesn't spam logs)
  - If the call fails, logs a warning and continues
- [x] Added `SyncQuota()` helper method on `Engine` that encapsulates
  "call analytics, set metrics, sync rate limiter" — used by both
  startup and post-cycle
- [x] Added `WithAnalyticsClient()` and `WithRateLimiter()` engine options
- [x] Added `AnalyticsURL` config field with default
- [x] Added tests (3 cases):
  - `TestSyncQuota_SetsMetricsAndSyncsRateLimiter`: verifies metrics
    and rate limiter state after successful analytics call
  - `TestSyncQuota_AnalyticsFailureDoesNotPanic`: verifies failure
    doesn't panic and rate limiter is unchanged
  - `TestSyncQuota_NilAnalyticsClientIsNoOp`: verifies no-op when
    analytics client is nil
- [x] Run `make test` and `make lint`

### Success Criteria

- On startup, metrics reflect eBay's actual quota state
- After each ingestion cycle, metrics are updated
- Failures don't block startup or ingestion
- All tests pass

### Files

- `cmd/server-price-tracker/cmd/serve.go`
- `internal/engine/engine.go`
- `internal/engine/engine_test.go`
- `internal/config/config.go`

---

## Phase 3: Sync Rate Limiter with Analytics State

### Tasks

- [x] Add a `Sync(count, limit int64, resetAt time.Time)` method to
  `RateLimiter` that updates the internal state:
  - Set `r.maxDaily = limit` (in case eBay changes the limit)
  - Set `r.daily.Store(count)` (eBay's actual usage count)
  - Set `r.resetAt = resetAt` under the mutex (eBay's actual reset time)
  - Set `r.windowStart = resetAt.Add(-24 * time.Hour)` to keep the
    window consistent
- [x] Add tests to `internal/ebay/ratelimit_test.go`:
  - `TestRateLimiter_Sync`: call Sync with known values, verify
    `DailyCount()`, `MaxDaily()`, `Remaining()`, and `ResetAt()` all
    reflect the synced state
  - `TestRateLimiter_Sync_UpdatesLimit`: verify that if eBay reports a
    different limit (e.g., 10000), `MaxDaily()` updates accordingly
  - `TestRateLimiter_Sync_ThenWait`: sync with count=100, then call
    Wait() and verify the count increments from the synced baseline
- [x] Run `make test` and `make lint`

### Success Criteria

- After `Sync()`, the rate limiter's state matches eBay's reported values
- The rate limiter continues to work correctly after sync
- All existing rate limiter tests continue to pass

### Files

- `internal/ebay/ratelimit.go`
- `internal/ebay/ratelimit_test.go`

---

## Phase 4: Update Dashboard Panels and Alert Rules

### Tasks

- [ ] Update `tools/dashgen/panels/overview.go`:
  - `QuotaBar`: change query to use analytics-derived metrics:
    `(spt_ebay_rate_limit - spt_ebay_rate_remaining) / spt_ebay_rate_limit * 100`
  - `DailyUsageStat`: change query to:
    `spt_ebay_rate_limit - spt_ebay_rate_remaining`
- [ ] Update `tools/dashgen/panels/ebay.go`:
  - `DailyUsage` timeseries: change from `spt_ebay_daily_usage` to
    `spt_ebay_rate_limit - spt_ebay_rate_remaining` for the usage line,
    and add a second query `spt_ebay_rate_limit` for the limit line
  - Add a new `ResetCountdown()` stat panel showing time until reset:
    `spt_ebay_rate_reset_timestamp - time()` with unit `s`
- [ ] Update `tools/dashgen/rules/alerts.go`:
  - `SptEbayQuotaHigh`: change expr from `spt_ebay_daily_usage > 4000`
    to `spt_ebay_rate_remaining < 1000` (less than 1000 calls remaining)
- [ ] Update `tools/dashgen/config.go`:
  - Add the 3 new metrics to `KnownMetrics`
- [ ] Regenerate artifacts: `cd tools/dashgen && go run .`
- [ ] Run `make dashboards-test` and `make dashboards-validate`

### Success Criteria

- Dashboard panels show eBay's authoritative quota data
- Alert rules fire based on eBay's reported remaining calls
- All dashgen tests pass

### Files

- `tools/dashgen/panels/overview.go`
- `tools/dashgen/panels/ebay.go`
- `tools/dashgen/rules/alerts.go`
- `tools/dashgen/config.go`
- `deploy/grafana/data/spt-overview.json` (regenerated)
- `deploy/prometheus/spt-alerts.yaml` (regenerated)

---

## Phase 5: Deprecate `spt_ebay_daily_usage`

### Tasks

- [ ] In `internal/metrics/metrics.go`, update the `EbayDailyUsage` Help
  text to include `(DEPRECATED)`
- [ ] Remove `spt_ebay_daily_usage` from `KnownMetrics` in
  `tools/dashgen/config.go`
- [ ] Verify no dashboard panels or alert rules reference
  `spt_ebay_daily_usage`
- [ ] Regenerate artifacts: `cd tools/dashgen && go run .`
- [ ] Run full test suite: `make test && make lint && make dashboards-test`

### Success Criteria

- Metric is marked deprecated but still registered (no breaking change)
- No dashboard or alert references the deprecated metric
- All tests pass

### Files

- `internal/metrics/metrics.go`
- `tools/dashgen/config.go`
- `deploy/grafana/data/spt-overview.json` (regenerated)

See `docs/plans/metrics-limit.md` for full test results, raw API
responses, and HTTPie commands.
