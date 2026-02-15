# Implementation: eBay Rate Limiter, Paginator, and Quota Tracking

## Context

The eBay Browse API has a 5,000 calls/day production limit. The codebase
has a `RateLimiter` (token bucket + daily counter) and a `Paginator`
(multi-page search with early stopping) that are fully built and tested
but never wired into the application. The engine currently makes a single
unpaginated `Search()` call per watch with no rate limiting, no daily
quota tracking, and no Prometheus metrics for API usage. Moving to
production eBay requires the app to know its budget, enforce limits, and
stop gracefully when the daily quota is exhausted.

See `docs/plans/rate-limit.md` for the high-level design.

---

## Phase 0: Config and Sentinel Errors

### Tasks

- [x] Add `RateLimitConfig` struct to `internal/config/config.go`
  - Fields: `PerSecond float64`, `Burst int`, `DailyLimit int64`
  - YAML tags: `per_second`, `burst`, `daily_limit`
- [x] Add `RateLimit RateLimitConfig` field to `EbayConfig`
- [x] Add `applyRateLimitDefaults()` function
  - `PerSecond`: 5.0, `Burst`: 10, `DailyLimit`: 5000
  - Call from `applyEbayDefaults()`
- [x] Add test case to `TestLoad` in `internal/config/config_test.go`
  - Verify defaults are applied when `rate_limit` section is absent
  - Verify custom values are loaded when `rate_limit` section is present
- [x] Add `ErrDailyLimitReached` sentinel error to `internal/ebay/ratelimit.go`
  - `var ErrDailyLimitReached = errors.New("daily API limit reached")`
  - Update `Wait()` to return `fmt.Errorf("%w (%d/%d)", ErrDailyLimitReached, count, max)`
- [x] Add getters to `RateLimiter` in `internal/ebay/ratelimit.go`
  - `MaxDaily() int64`
  - `Remaining() int64` (maxDaily - DailyCount, floored at 0)
  - `ResetAt() time.Time` (mutex-protected read)
- [x] Add tests for new `RateLimiter` methods in `internal/ebay/ratelimit_test.go`
  - `TestRateLimiter_MaxDaily`
  - `TestRateLimiter_Remaining`
  - `TestRateLimiter_ResetAt`
  - `TestRateLimiter_ErrDailyLimitReached` (verify `errors.Is` works)
- [x] Run `make test && make lint`

### Success Criteria

- `config.Load()` populates `RateLimitConfig` with defaults when absent
  and custom values when present
- `ErrDailyLimitReached` is a sentinel that `errors.Is()` matches after
  `Wait()` returns it
- All getter methods return correct values
- All tests pass, zero lint issues

### Files

- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/ebay/ratelimit.go`
- `internal/ebay/ratelimit_test.go`

---

## Phase 1: Inject RateLimiter into BrowseClient

### Tasks

- [x] Add `rateLimiter *RateLimiter` field to `BrowseClient` struct
      in `internal/ebay/browse.go`
- [x] Add `WithRateLimiter(r *RateLimiter) BrowseOption` function
- [x] Update `Search()` to call `c.rateLimiter.Wait(ctx)` before the
      HTTP request when the rate limiter is non-nil
  - If `Wait()` returns an error, return it wrapped:
    `fmt.Errorf("rate limit: %w", err)`
- [x] Add tests in `internal/ebay/browse_test.go`:
  - `TestBrowseClient_Search_RateLimited`: Inject a rate limiter with
    `maxDaily=1`, make 2 calls, second returns `ErrDailyLimitReached`
  - `TestBrowseClient_Search_NoRateLimiter`: Verify existing behavior
    when no rate limiter is set (nil check works)
- [x] Run `make test && make lint`

### Success Criteria

- Every `BrowseClient.Search()` call goes through `Wait()` when a rate
  limiter is injected
- Second call after daily limit returns `ErrDailyLimitReached`
  (testable via `errors.Is`)
- Existing tests pass unchanged (no rate limiter = no behavior change)

### Files

- `internal/ebay/browse.go`
- `internal/ebay/browse_test.go`

---

## Phase 2: eBay Prometheus Metrics

### Tasks

- [x] Add eBay metrics section to `internal/metrics/metrics.go`:
  - `EbayAPICallsTotal` — Counter, cumulative eBay API calls
  - `EbayDailyUsage` — Gauge, current daily call count
  - `EbayDailyLimitHits` — Counter, times daily limit was reached
- [x] Update `BrowseClient.Search()` in `internal/ebay/browse.go`:
  - After successful `Wait()`: increment `EbayAPICallsTotal`
  - After successful `Wait()`: set `EbayDailyUsage` from
    `rateLimiter.DailyCount()`
  - When `Wait()` returns `ErrDailyLimitReached`: increment
    `EbayDailyLimitHits`
  - Guard all metric updates behind `rateLimiter != nil` check
- [x] Run `make test && make lint`

### Success Criteria

- `/metrics` endpoint exposes `spt_ebay_api_calls_total`,
  `spt_ebay_daily_usage`, and `spt_ebay_daily_limit_hits_total`
- Metrics update correctly on each `Search()` call
- No metrics emitted when rate limiter is nil

### Files

- `internal/metrics/metrics.go`
- `internal/ebay/browse.go`

---

## Phase 3: Quota API Endpoint

### Tasks

- [x] Create `internal/api/handlers/quota.go`:
  - `QuotaHandler` struct with `rl *ebay.RateLimiter` dependency
  - `NewQuotaHandler(rl *ebay.RateLimiter) *QuotaHandler` constructor
  - `QuotaOutput` struct with `Body` containing:
    - `DailyLimit int64` (json `daily_limit`, doc, example tags)
    - `DailyUsed int64` (json `daily_used`)
    - `Remaining int64` (json `remaining`)
    - `ResetAt time.Time` (json `reset_at`)
  - `GetQuota(ctx, *struct{}) (*QuotaOutput, error)` handler method
  - When `rl` is nil, return all zeroes (no rate limiter configured)
  - `RegisterQuotaRoutes(api huma.API, h *QuotaHandler)` wiring func
  - Register `GET /api/v1/quota` with operation ID `get-quota`,
    tag `"ebay"`, summary `"Get eBay API quota status"`
- [x] Create `internal/api/handlers/quota_test.go`:
  - Test with real `RateLimiter` (not mock): create one with known
    `maxDaily`, call `Wait()` a few times, verify response counts
  - Test with nil rate limiter: returns all zeroes
- [x] Run `make test && make lint`

### Success Criteria

- `GET /api/v1/quota` returns JSON with `daily_limit`, `daily_used`,
  `remaining`, `reset_at`
- Endpoint appears in `/openapi.json` spec under tag `"ebay"`
- Nil rate limiter returns graceful zeroes, not an error
- Tests pass

### Files

- `internal/api/handlers/quota.go` (new)
- `internal/api/handlers/quota_test.go` (new)

---

## Phase 4: Wire Paginator into Engine

### Tasks

- [x] Add `paginator *ebay.Paginator` field to `Engine` struct in
      `internal/engine/engine.go`
- [x] Add `WithPaginator(p *ebay.Paginator) EngineOption`
- [x] Add `maxCallsPerCycle int` field with default 50
- [x] Add `WithMaxCallsPerCycle(n int) EngineOption`
- [x] Rewrite `processWatch()`:
  - Change signature to return `(int, error)` where int is pages used
  - Build `ebay.SearchRequest` from watch fields
  - If `eng.paginator != nil`: call `eng.paginator.Paginate(ctx, req, false)`
    and iterate over `result.NewListings`
  - If `eng.paginator == nil`: fall back to current single `Search()`
    call (backward compat for tests that don't set paginator)
  - Log `PagesUsed`, `TotalSeen`, `StoppedAt` from `PaginateResult`
  - Return `result.PagesUsed` (or 1 for fallback path)
- [x] Update `RunIngestion()`:
  - Track `totalPages` across all watches
  - Before each watch: check `totalPages >= eng.maxCallsPerCycle`,
    break with warning log if exceeded
  - After `processWatch()`: check if error wraps
    `ebay.ErrDailyLimitReached` — if so, log warning and break
    (don't count as an ingestion error)
  - Always run `ProcessAlerts()` even if budget/daily limit was hit
- [x] Update `internal/engine/engine_test.go`:
  - Update `TestRunIngestion` tests to account for `processWatch`
    returning `(int, error)` instead of `error`
  - Add `TestRunIngestion_DailyLimitHit`: mock `Search()` to return
    `ErrDailyLimitReached`, verify engine stops gracefully
  - Add `TestRunIngestion_CycleBudgetExhausted`: set
    `maxCallsPerCycle=1`, verify only first watch processes
- [x] Run `make test && make lint`

### Success Criteria

- Engine uses Paginator when set (paginated, dedup'd results)
- Engine falls back to raw `Search()` when paginator is nil (tests)
- Cycle budget stops ingestion across watches when exhausted
- Daily limit error causes graceful early exit, not a failure
- Alert processing always runs regardless of budget/limit
- All existing engine tests pass with updated signatures

### Files

- `internal/engine/engine.go`
- `internal/engine/engine_test.go`

---

## Phase 5: Wire Everything in serve.go

### Tasks

- [x] Update `buildEbayClient()` signature to return
      `(ebay.EbayClient, *ebay.RateLimiter)`:
  - Create `RateLimiter` from `cfg.Ebay.RateLimit.*`
  - Pass to `NewBrowseClient` via `WithRateLimiter(rl)`
  - Return both client and rate limiter
  - When credentials are missing, return `(nil, nil)`
- [x] Update `startServer()` to capture the rate limiter:
  - `ebayClient, rateLimiter := buildEbayClient(cfg, slogger)`
- [x] Update `buildEngine()` to accept the store for Paginator creation:
  - Create `ebay.NewPaginator(ebayClient, pgStore, ebay.WithPaginatorLogger(logger))`
  - Pass to engine via `engine.WithPaginator(paginator)`
  - Pass `engine.WithMaxCallsPerCycle(cfg.Ebay.MaxCallsPerCycle)`
  - Only create paginator when both `ebayClient` and `pgStore` are non-nil
- [x] Update `registerRoutes()` to accept `*ebay.RateLimiter`:
  - Create `QuotaHandler` and register quota routes
  - Register even when rate limiter is nil (handler returns zeroes)
- [x] Log rate limiter configuration at startup:
  - `"rate limiter configured"`, `per_second`, `burst`, `daily_limit`
- [x] Run `make build && make test && make lint`

### Success Criteria

- Server starts with rate limiter wired into eBay client
- Paginator is created with store as `ListingChecker`
- Quota endpoint is registered and accessible
- Graceful degradation when eBay credentials are missing
- Build, tests, and lint all pass

### Files

- `cmd/server-price-tracker/cmd/serve.go`

---

## Phase 6: Config Files and Helm Chart

### Tasks

- [ ] Update `configs/config.example.yaml`:
  - Add `rate_limit` section under `ebay` with comments
- [ ] Update `configs/config.dev.yaml`:
  - Add `rate_limit` section with dev values (daily_limit: 5000)
- [ ] Update `charts/server-price-tracker/values.yaml`:
  - Add `rate_limit` under `config.ebay`:
    `per_second: 5`, `burst: 10`, `daily_limit: 5000`
- [ ] Update `charts/server-price-tracker/templates/configmap.yaml`:
  - Add `rate_limit` rendering block under `ebay` section:
    ```yaml
    rate_limit:
      per_second: {{ .Values.config.ebay.rate_limit.per_second }}
      burst: {{ .Values.config.ebay.rate_limit.burst }}
      daily_limit: {{ .Values.config.ebay.rate_limit.daily_limit }}
    ```
- [ ] Run `make helm-test` to verify chart renders correctly
- [ ] Run `make helm-lint`

### Success Criteria

- Helm template renders `rate_limit` section in ConfigMap
- All helm unit tests pass
- Config files have documented rate limit settings

### Files

- `configs/config.example.yaml`
- `configs/config.dev.yaml`
- `charts/server-price-tracker/values.yaml`
- `charts/server-price-tracker/templates/configmap.yaml`

---

## Phase 7: Documentation

### Tasks

- [ ] Update `docs/OPERATIONS.md`:
  - Add "Quota Monitoring" section under Ongoing Operations
  - Document `GET /api/v1/quota` endpoint
  - Document Prometheus metrics: `spt_ebay_api_calls_total`,
    `spt_ebay_daily_usage`, `spt_ebay_daily_limit_hits_total`
  - Add Grafana alert suggestion for daily limit approaching
- [ ] Update `CLAUDE.md`:
  - Add `/api/v1/quota` to Key API Endpoints section
  - Note rate limiter in Architecture section
- [ ] Run `make lint-md` (if available) or review formatting

### Success Criteria

- Operations guide documents quota monitoring and metrics
- CLAUDE.md reflects the new endpoint

### Files

- `docs/OPERATIONS.md`
- `CLAUDE.md`

---

## Open Questions

1. **First-run detection for Paginator**: The `Paginator.Paginate()`
   accepts an `isFirstRun bool` that caps pages at 5 for initial polls.
   The current plan always passes `false`. Should we track first-run
   state per watch (e.g., a `last_ingested_at` timestamp on the Watch
   domain model), or is always using the full `maxPages` acceptable for
   now?

   **Decision:** Track each watch individually so we know which watches
   cost the most API calls. Introduce a global `maxPages` hard limit
   that each watch respects. This lays groundwork for a smarter
   scheduling system that can shift windows and adjust `maxPages` per
   watch to maximize coverage within the 24-hour budget. Future work:
   build scheduling that calculates how to fit the most watches into
   the rolling window by adjusting per-watch page limits dynamically.

2. **Rate limiter reset timezone**: `nextMidnight()` uses the server's
   local timezone (which is UTC in Kubernetes). eBay's daily quota
   resets on a rolling 24-hour window, not at midnight. Should we track
   a rolling 24-hour window instead of midnight reset? This would
   require changing `checkDailyReset()` to use `time.Now().Add(-24h)`
   instead of midnight.

   **Decision:** Track eBay's rolling 24-hour window as the source of
   truth (not midnight reset). Change `checkDailyReset()` to track
   when the window started and expire after 24 hours. Expose the window
   reset timestamp in Prometheus so Grafana dashboards can display it
   in localized format and metrics accurately reflect the eBay rolling
   window boundary.

3. **Manual search handler budget**: The `/api/v1/search` endpoint also
   calls `BrowseClient.Search()` and will go through the rate limiter.
   This means manual searches count against the daily quota. Is that the
   desired behavior, or should manual searches bypass the rate limiter?

   **Decision:** All API calls count toward the budget. Manual searches
   via `/api/v1/search` go through the rate limiter and count against
   the daily quota. All requests that cost toward the 5,000/day limit
   are tracked in Prometheus — manual or automated. This feeds into the
   24-hour budget calculation and enables anomaly detection: if at the
   12-hour mark the system estimates it can't complete the current watch
   schedule in the remaining budget, it can alert (and eventually
   trigger an automatic recalculation of the schedule).

4. **Paginator fallback in engine**: The plan keeps a fallback to raw
   `Search()` when paginator is nil (for backward compat in existing
   tests). An alternative is to always require the paginator and update
   all engine tests to provide one. Which approach is preferred?

   **Decision:** Always require the Paginator (no nil fallback). Remove
   the nil fallback path in the engine and update all engine tests to
   provide a Paginator. This catches unseen errors that the fallback
   would mask and protects against runaway searches that could eat the
   API limit without pagination controls.
