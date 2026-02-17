# Plan: Restart-Resilient eBay Quota Metrics

## Problem

The current eBay rate limiting implementation is entirely client-side. The
`RateLimiter` tracks a rolling 24-hour window starting from process start,
maintaining a daily counter in memory. This has two critical issues:

1. **Pod restarts reset everything.** The daily counter, window start, and
   reset time are all in-memory. Every deploy or crash resets the counter
   to 0 and starts a new 24h window. The `spt_ebay_daily_usage` gauge
   drops to 0, making dashboards and alerts unreliable.

2. **The window model is wrong.** Our rate limiter uses a rolling 24h
   window from process start. eBay's actual quota resets daily at midnight
   Pacific Time (08:00 UTC). After a restart, our window diverges from
   eBay's actual window.

## What we tested (2026-02-16)

### Browse API response headers

Tested with HTTPie against both sandbox and production. Neither environment
returns rate limit headers (`X-eBay-C-RateLimit-*`) on Browse API search
responses with client credentials auth. Community reports of these headers
could not be verified.

### Analytics API

The eBay Developer Analytics API (`getRateLimits`) works with our existing
app token (client credentials) and returns authoritative quota state:

```json
{
  "rateLimits": [{
    "apiContext": "buy",
    "apiName": "Browse",
    "apiVersion": "v1",
    "resources": [
      {
        "name": "buy.browse",
        "rates": [{
          "count": 110,
          "limit": 5000,
          "remaining": 4890,
          "reset": "2026-02-17T08:00:00.000Z",
          "timeWindow": 86400
        }]
      },
      {
        "name": "buy.browse.item.bulk",
        "rates": [{
          "count": 0,
          "limit": 5000,
          "remaining": 5000,
          "reset": "2026-02-17T08:00:00.000Z",
          "timeWindow": 86400
        }]
      }
    ]
  }]
}
```

Key findings:
- `count`, `limit`, `remaining` — all three present (eBay's source of truth)
- `reset` is `2026-02-17T08:00:00.000Z` — midnight Pacific, confirming
  community reports
- `timeWindow` is `86400` (24h) — daily limit
- `buy.browse` and `buy.browse.item.bulk` have separate 5000/day limits
- We only use `item_summary/search` which falls under `buy.browse`, so
  we only need to track that one resource

## Solution

Poll the eBay Analytics API to get authoritative quota state, and expose
it as Prometheus gauge metrics. The polling strategy:

1. **On startup** — immediately sync rate limiter state after a pod
   restart so dashboards and alerts have accurate data from the first
   Prometheus scrape
2. **After each ingestion cycle** — the ingestion loop (every 15 min) is
   where all Browse API calls happen, so one analytics call after each
   cycle keeps metrics current

This adds ~96 analytics calls/day (4/hour × 24h) which is negligible
against the 5000/day browse budget.

### New Prometheus metrics

| Metric | Type | Source | Description |
|--------|------|--------|-------------|
| `spt_ebay_rate_limit` | Gauge | `rates[].limit` | Total calls allowed in window |
| `spt_ebay_rate_remaining` | Gauge | `rates[].remaining` | Calls remaining in window |
| `spt_ebay_rate_reset_timestamp` | Gauge | `rates[].reset` | Unix epoch of window reset |

### What this enables

- **Actual usage from eBay:** `spt_ebay_rate_limit - spt_ebay_rate_remaining`
- **Usage percentage:** `(1 - spt_ebay_rate_remaining / spt_ebay_rate_limit) * 100`
- **Time until reset:** `spt_ebay_rate_reset_timestamp - time()`
- **Pod-restart resilient:** values re-sync on startup, persist in
  Prometheus between scrapes

### What stays the same

- `spt_ebay_api_calls_total` (Counter) — cumulative calls, useful for
  `rate()` and `increase()` calculations
- `spt_ebay_daily_limit_hits_total` (Counter) — tracks 429/limit events
- Token bucket per-second rate limiter — still useful for burst control

### What gets deprecated

- `spt_ebay_daily_usage` (Gauge) — replaced by analytics-derived metrics.
  Stays registered for now, marked deprecated, removed in a future PR.

### Rate limiter sync

After each analytics call, update the in-memory `RateLimiter`:
- Set `resetAt` from the analytics reset timestamp (eBay's actual reset)
- Set `daily` count from `count` (eBay's actual usage)
- Set `maxDaily` from `limit` (in case eBay changes it)

This keeps the client-side rate limiter accurate for pre-flight checks.

## Scope

This plan covers:
1. Adding an Analytics API client to poll `getRateLimits`
2. Defining 3 new Prometheus gauge metrics
3. Polling on startup and after each ingestion cycle
4. Syncing the `RateLimiter` with eBay's reported state
5. Updating dashboard panels and alert rules
6. Deprecating `spt_ebay_daily_usage`

Out of scope (future work):
- Persisting rate limiter state to the database
- Tracking `buy.browse.item.bulk` limits (we don't use that resource)
- Browse API response header parsing (headers not present)

## References

- [eBay API Call Limits](https://developer.ebay.com/develop/get-started/api-call-limits)
- [eBay Analytics API — getRateLimits](https://developer.ebay.com/api-docs/developer/analytics/resources/rate_limit/methods/getRateLimits)
- Analytics API test: 2026-02-16, production, confirmed working with
  client credentials auth
- Browse API header test: 2026-02-16, no rate limit headers found

---

## eBay Header Test Results (2026-02-16)

Tested with HTTPie against sandbox and production Browse API using client
credentials (app token) auth.

### Sandbox

```
HTTP/1.1 200 OK
cache-control: no-cache, no-store, max-age=0, must-revalidate
content-type: application/json
x-ebay-client-tls-version: TLSv1.3
x-ebay-pop-id: UFES2-LVSAZ01-apisandbox
x-envoy-upstream-service-time: 241
```

No rate limit headers.

### Production

```
HTTP/1.1 200 OK
Via: 1.1 varnish
X-Cache: MISS, MISS
X-Served-By: cache-cmh1290047-CMH
cache-control: no-cache, no-store, max-age=0, must-revalidate
content-type: application/json
x-CDN: Fastly
x-ebay-pop-id: UFES2-RNOAZ05-api
x-ebay-svc-tracking-data: <tracking data>
x-envoy-upstream-service-time: 404
```

No rate limit headers. Production responses route through Fastly CDN.

---

## Analytics API Test Results (2026-02-16)

Production response with `api_name=browse&api_context=buy`:

```json
{
  "rateLimits": [{
    "apiContext": "buy",
    "apiName": "Browse",
    "apiVersion": "v1",
    "resources": [
      {
        "name": "buy.browse",
        "rates": [{
          "count": 110,
          "limit": 5000,
          "remaining": 4890,
          "reset": "2026-02-17T08:00:00.000Z",
          "timeWindow": 86400
        }]
      },
      {
        "name": "buy.browse.item.bulk",
        "rates": [{
          "count": 0,
          "limit": 5000,
          "remaining": 5000,
          "reset": "2026-02-17T08:00:00.000Z",
          "timeWindow": 86400
        }]
      }
    ]
  }]
}
```

Key findings:
- Works with client credentials auth (no user token needed)
- `reset` at 08:00 UTC = midnight Pacific Time
- `buy.browse` is the resource for `item_summary/search`
- `buy.browse.item.bulk` is separate (we don't use it)
- `count` field present (undocumented) — direct usage count from eBay

---

## HTTPie Commands for Re-Testing Browse API Headers

```bash
TOKEN=$(http -a "$EBAY_APP_ID:$EBAY_CERT_ID" --form POST \
  https://api.ebay.com/identity/v1/oauth2/token \
  grant_type=client_credentials \
  scope=https://api.ebay.com/oauth/api_scope \
  | jq -r '.access_token')

http --print=h GET \
  "https://api.ebay.com/buy/browse/v1/item_summary/search" \
  q==DDR4+ECC+32GB limit==1 \
  Authorization:"Bearer $TOKEN" \
  X-EBAY-C-MARKETPLACE-ID:EBAY_US
```

---

## HTTPie Commands for Testing the Analytics API

### Get rate limits (production)

```bash
TOKEN="<your app token>"
http GET \
  "https://api.ebay.com/developer/analytics/v1_beta/rate_limit/" \
  Authorization:"Bearer $TOKEN" \
  api_name==browse api_context==buy
```

### Get rate limits (sandbox)

```bash
TOKEN="<your sandbox token>"
http GET \
  "https://api.sandbox.ebay.com/developer/analytics/v1_beta/rate_limit/" \
  Authorization:"Bearer $TOKEN" \
  api_name==browse api_context==buy
```

### Get rate limits for all APIs (no filters)

```bash
http GET \
  "https://api.ebay.com/developer/analytics/v1_beta/rate_limit/" \
  Authorization:"Bearer $TOKEN"
```
