# Plan: Wire eBay Rate Limiter, Paginator, and Quota Tracking

## Context

The eBay Browse API has a 5,000 calls/day production limit. The codebase
has a `RateLimiter` (token bucket + daily counter) and a `Paginator`
(multi-page search with early stopping) that are fully built and tested
but never wired into the application. The engine currently makes a single
unpaginated `Search()` call per watch with no rate limiting, no daily
quota tracking, and no Prometheus metrics for API usage. Moving to
production eBay requires the app to know its budget, enforce limits, and
stop gracefully when the daily quota is exhausted.

## Changes

### 1. Add rate limit config fields

**File:** `internal/config/config.go`

Add fields to `EbayConfig`:

```go
type EbayConfig struct {
    // ... existing fields ...
    RateLimit RateLimitConfig `yaml:"rate_limit"`
}

type RateLimitConfig struct {
    PerSecond  float64 `yaml:"per_second"`   // default: 5.0
    Burst      int     `yaml:"burst"`         // default: 10
    DailyLimit int64   `yaml:"daily_limit"`   // default: 5000
}
```

Add `applyRateLimitDefaults()` in the defaults chain. Update
`configs/config.example.yaml` and `configs/config.dev.yaml` with the new
section. Update Helm `values.yaml` and `configmap.yaml` template.

### 2. Inject RateLimiter into BrowseClient

**File:** `internal/ebay/browse.go`

Add a `rateLimiter` field to `BrowseClient` and a `WithRateLimiter`
option:

```go
type BrowseClient struct {
    // ... existing fields ...
    rateLimiter *RateLimiter
}

func WithRateLimiter(r *RateLimiter) BrowseOption {
    return func(c *BrowseClient) { c.rateLimiter = r }
}
```

In `Search()`, call `r.rateLimiter.Wait(ctx)` before the HTTP request
if the rate limiter is set. This makes every API call go through the
limiter automatically — both engine-driven and manual search handler
calls.

### 3. Add getters to RateLimiter

**File:** `internal/ebay/ratelimit.go`

Add methods needed by the quota endpoint and metrics:

```go
func (r *RateLimiter) MaxDaily() int64    { return r.maxDaily }
func (r *RateLimiter) ResetAt() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.resetAt }
func (r *RateLimiter) Remaining() int64   { ... } // maxDaily - daily count, min 0
```

### 4. Add eBay Prometheus metrics

**File:** `internal/metrics/metrics.go`

Add new metrics under an "eBay API metrics" section:

```go
EbayAPICallsTotal = promauto.NewCounter(prometheus.CounterOpts{
    Namespace: namespace,
    Name:      "ebay_api_calls_total",
    Help:      "Total eBay Browse API calls made.",
})

EbayDailyUsage = promauto.NewGauge(prometheus.GaugeOpts{
    Namespace: namespace,
    Name:      "ebay_daily_usage",
    Help:      "Current daily eBay API call count.",
})

EbayDailyLimitHits = promauto.NewCounter(prometheus.CounterOpts{
    Namespace: namespace,
    Name:      "ebay_daily_limit_hits_total",
    Help:      "Times the daily eBay API limit was reached.",
})
```

Update `BrowseClient.Search()` to increment `EbayAPICallsTotal` on each
call and set `EbayDailyUsage` from `rateLimiter.DailyCount()` after
each call. When the rate limiter returns a daily limit error, increment
`EbayDailyLimitHits`.

### 5. Add quota API endpoint

**File:** `internal/api/handlers/quota.go` (new)

Huma handler that exposes current rate limiter state:

```go
type QuotaOutput struct {
    Body struct {
        DailyLimit int64     `json:"daily_limit"`
        DailyUsed  int64     `json:"daily_used"`
        Remaining  int64     `json:"remaining"`
        ResetAt    time.Time `json:"reset_at"`
    }
}
```

The handler takes a `*ebay.RateLimiter` dependency. If nil (no rate
limiter configured), returns zeroes.

Register as `GET /api/v1/quota` with tag `"ebay"`.

**File:** `internal/api/handlers/quota_test.go` (new) — unit tests with
humatest.

### 6. Wire Paginator into Engine

**File:** `internal/engine/engine.go`

Replace the raw `ebay.Search()` call in `processWatch()` with the
existing `Paginator`:

- Add `paginator *ebay.Paginator` field to `Engine`
- Add `WithPaginator` option
- In `processWatch()`: call `paginator.Paginate(ctx, req, isFirstRun)`
  instead of `eng.ebay.Search()`
- Iterate over `result.NewListings` instead of all items (paginator
  already filters known listings)
- Log `PagesUsed`, `TotalSeen`, `StoppedAt` per watch
- Return `PagesUsed` from `processWatch()` for budget tracking

Determine `isFirstRun` by checking if the store has any listings for
the watch's search query (or simply pass `false` for now and add first-
run detection later).

### 7. Enforce MaxCallsPerCycle budget in Engine

**File:** `internal/engine/engine.go`

In `RunIngestion()`, track total API pages used across all watches:

```go
var totalPages int
for i := range watches {
    if totalPages >= eng.maxCallsPerCycle {
        eng.log.Warn("cycle budget exhausted", "used", totalPages, "max", eng.maxCallsPerCycle)
        break
    }
    pages, err := eng.processWatch(ctx, w)
    totalPages += pages
    // ...
}
```

Add `maxCallsPerCycle int` field to Engine with `WithMaxCallsPerCycle`
option. Wired from `cfg.Ebay.MaxCallsPerCycle` in serve.go.

### 8. Handle daily limit exhaustion gracefully

**File:** `internal/engine/engine.go`

When `processWatch()` gets a rate limiter error containing "daily API
limit reached", the engine should:

1. Log a warning with the current daily count
2. Break out of the watch loop (don't process remaining watches)
3. Still run alert processing for whatever was ingested
4. Return `nil` (not an error) — the limit being hit is expected

Define a sentinel: `var ErrDailyLimitReached` in `internal/ebay/ratelimit.go`
that `Wait()` returns, so the engine can check with `errors.Is()`.

### 9. Wire everything in serve.go

**File:** `cmd/server-price-tracker/cmd/serve.go`

Update `buildEbayClient()`:

```go
func buildEbayClient(cfg *config.Config, logger *slog.Logger) (ebay.EbayClient, *ebay.RateLimiter) {
    // ... existing credential check ...
    rl := ebay.NewRateLimiter(
        cfg.Ebay.RateLimit.PerSecond,
        cfg.Ebay.RateLimit.Burst,
        cfg.Ebay.RateLimit.DailyLimit,
    )
    client := ebay.NewBrowseClient(
        tokenProvider,
        ebay.WithBrowseURL(cfg.Ebay.BrowseURL),
        ebay.WithMarketplace(cfg.Ebay.Marketplace),
        ebay.WithRateLimiter(rl),
    )
    return client, rl
}
```

Return the `*RateLimiter` so it can be passed to:
- The quota handler (for the API endpoint)
- The engine doesn't need it directly (it gets rate limiting via the
  BrowseClient which the Paginator wraps)

Update `buildEngine()` to create the Paginator:

```go
paginator := ebay.NewPaginator(
    ebayClient,
    pgStore,  // implements ListingChecker
    ebay.WithPaginatorLogger(logger),
)
eng := engine.NewEngine(
    s, ebayClient, extractor, notifier,
    engine.WithLogger(logger),
    engine.WithPaginator(paginator),
    engine.WithMaxCallsPerCycle(cfg.Ebay.MaxCallsPerCycle),
    // ... existing options ...
)
```

Update `registerRoutes()` to register the quota endpoint.

### 10. Update config files and documentation

- `configs/config.example.yaml` — add `rate_limit` section under `ebay`
- `configs/config.dev.yaml` — add `rate_limit` with dev-appropriate
  values (lower daily limit for sandbox)
- `charts/server-price-tracker/values.yaml` — add `rate_limit` under
  `config.ebay`
- `charts/server-price-tracker/templates/configmap.yaml` — render
  `rate_limit` fields
- `docs/OPERATIONS.md` — add quota monitoring section
- `CLAUDE.md` — add quota endpoint to Key API Endpoints

## Files Changed

| File | Action |
|------|--------|
| `internal/config/config.go` | Add `RateLimitConfig` struct and defaults |
| `internal/ebay/ratelimit.go` | Add `ErrDailyLimitReached`, getters |
| `internal/ebay/browse.go` | Add `rateLimiter` field, `WithRateLimiter` option, call `Wait()` in `Search()` |
| `internal/metrics/metrics.go` | Add 3 eBay metrics |
| `internal/api/handlers/quota.go` | **New** — quota endpoint handler |
| `internal/api/handlers/quota_test.go` | **New** — quota handler tests |
| `internal/engine/engine.go` | Add paginator, cycle budget, daily limit handling |
| `cmd/server-price-tracker/cmd/serve.go` | Wire rate limiter, paginator, quota handler |
| `configs/config.example.yaml` | Add `rate_limit` section |
| `configs/config.dev.yaml` | Add `rate_limit` section |
| `charts/server-price-tracker/values.yaml` | Add `rate_limit` values |
| `charts/server-price-tracker/templates/configmap.yaml` | Render `rate_limit` |
| `docs/OPERATIONS.md` | Add quota monitoring |
| `CLAUDE.md` | Add `/api/v1/quota` endpoint |

## Verification

```bash
make build          # both binaries compile
make test           # all unit tests pass (including new quota handler tests)
make lint           # zero lint issues
make helm-test      # helm unit tests pass

# Manual verification against running server:
curl https://spt.fartlab.dev/api/v1/quota
# {"daily_limit":5000,"daily_used":0,"remaining":5000,"reset_at":"2026-02-16T00:00:00Z"}

curl https://spt.fartlab.dev/metrics | grep ebay
# spt_ebay_api_calls_total 0
# spt_ebay_daily_usage 0
# spt_ebay_daily_limit_hits_total 0
```
