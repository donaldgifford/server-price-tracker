# Implementation: Skip Operational Metrics and Reduce Health Log Noise

## Context

The `/metrics`, `/healthz`, and `/readyz` endpoints are hit frequently by
Kubernetes probes (liveness every 15s, readiness every 10s) and Prometheus
scrapes (every 30s). The current middleware applies uniformly to all routes,
which creates two problems:

1. **Metric cardinality noise** — `spt_http_request_duration_seconds` and
   `spt_http_requests_total` create histogram bucket and counter label
   combinations for `/healthz`, `/readyz`, and `/metrics` that add no
   operational insight. These dominate the `/metrics` output.

2. **Log noise** — Every probe hit generates a structured log line. At
   ~10 requests/minute from probes alone, these drown out meaningful
   request activity in the logs.

The fix: skip these paths from HTTP histogram/counter metrics (but expose
simple up/down gauges for health), and suppress repetitive success logs
for health endpoints while always logging failures.

See `docs/plans/logs-metrics-chore.md` for the high-level design.

---

## Phase 0: Health Status Gauges

### Tasks

- [ ] Add health status gauges to `internal/metrics/metrics.go`:
  - `HealthzUp` — Gauge, `spt_healthz_up`,
    help: `"Health check status (1 = ok, 0 = failing)."`
  - `ReadyzUp` — Gauge, `spt_readyz_up`,
    help: `"Readiness check status (1 = ready, 0 = not ready)."`
  - Place in a new `// Health metrics.` var block after the existing
    HTTP metrics block
- [ ] Run `make test && make lint`

### Success Criteria

- `spt_healthz_up` and `spt_readyz_up` are registered in the default
  Prometheus registry (visible at `/metrics` after server restart)
- No existing tests break
- Zero lint issues

### Files

- `internal/metrics/metrics.go`

---

## Phase 1: Skip Operational Paths from HTTP Metrics

### Tasks

- [ ] Add `metricsSkipPaths` set to `internal/api/middleware/metrics.go`:
  - Package-level `var metricsSkipPaths = map[string]struct{}{...}`
  - Entries: `/metrics`, `/healthz`, `/readyz`
- [ ] Update `Metrics()` middleware function:
  - Resolve path early (same `c.Path()` fallback to `c.Request().URL.Path`)
  - Check `metricsSkipPaths` membership
  - For `/healthz`: call `next(c)`, then set `metrics.HealthzUp` to
    `1` (status 200–299) or `0`, then return
  - For `/readyz`: call `next(c)`, then set `metrics.ReadyzUp` to
    `1` (status 200–299) or `0`, then return
  - For `/metrics`: call `next(c)` and return (no gauge update)
  - For all skip paths: do not record `HTTPRequestDuration` or
    `HTTPRequestsTotal`
  - Non-skip paths: unchanged behavior
- [ ] Update `internal/api/middleware/metrics_test.go`:
  - Add `wantSkipped bool` field to the test table struct
  - Change existing `/healthz` test case: set `wantSkipped: true`
  - Add test case: `"skips /readyz from HTTP metrics"` with
    `wantSkipped: true`
  - Add test case: `"skips /metrics from HTTP metrics"` with
    `wantSkipped: true`
  - Add `getCounterValue(t, method, path, status) float64` helper
    that reads the current counter value via `GetMetricWithLabelValues`
    and `Write()` (needed because `promauto` metrics are global
    singletons — must use delta-based assertions)
  - For `wantSkipped` cases: capture counter before request, assert
    counter delta is 0 after request
  - For non-skipped cases: keep existing `assert.Greater` assertions
  - Add `TestMetricsMiddleware_HealthzGauge`: send 200, assert
    `metrics.HealthzUp` is `1`; send 503, assert gauge is `0`
  - Add `TestMetricsMiddleware_ReadyzGauge`: send 200, assert
    `metrics.ReadyzUp` is `1`; send 503, assert gauge is `0`
- [ ] Run `make test && make lint`

### Success Criteria

- `/healthz`, `/readyz`, `/metrics` requests do not create
  `spt_http_request_duration_seconds` or `spt_http_requests_total`
  label combinations
- `spt_healthz_up` reflects 1 or 0 based on last health check status
- `spt_readyz_up` reflects 1 or 0 based on last readiness check status
- `/metrics` endpoint itself is fully skipped (no gauge, no counter)
- Non-operational paths (`/api/v1/*`, etc.) still record full metrics
- All tests pass, zero lint issues

### Files

- `internal/api/middleware/metrics.go`
- `internal/api/middleware/metrics_test.go`

---

## Phase 2: Suppress Repetitive Health Check Logs

### Tasks

- [ ] Add `logSuppressPaths` set to `internal/api/middleware/requestlog.go`:
  - Package-level `var logSuppressPaths = map[string]struct{}{...}`
  - Entries: `/healthz`, `/readyz`, `/metrics`
- [ ] Update `RequestLog()` middleware function:
  - In the outer constructor closure (not per-request), initialize a
    `map[string]*sync.Once` with one entry per suppress path
  - Add `"sync"` to imports
  - After `err := next(c)`, build the log attrs slice once
  - Check if `urlPath` is in the `firstLogged` map:
    - **Success (2xx)**: wrap the `log.Info(...)` call in
      `once.Do(func() { ... })` — logs only the first success
    - **Failure (non-2xx)**: always log at `log.Warn(...)` level
  - Non-suppress paths: unchanged `log.Info(...)` every request
- [ ] Add tests to `internal/api/middleware/requestlog_test.go`:
  - `TestRequestLog_HealthzFirstSuccessLogged`: create middleware
    once, send 3 GET `/healthz` requests returning 200 — assert
    first request produces log output, second and third do not
    (compare `buf.Len()` before/after)
  - `TestRequestLog_HealthzFailureAlwaysLogged`: send 2 GET `/readyz`
    requests returning 503 — assert both produce log output and log
    at `WARN` level
  - `TestRequestLog_ReadyzFirstSuccessThenFailure`: send success
    (logged), success (suppressed), failure (logged at WARN) — mixed
    scenario with a stateful handler tracking call count
  - `TestRequestLog_NonHealthPathAlwaysLogged`: send 2 GET
    `/api/v1/watches` requests returning 200 — assert both produce
    log output (non-suppress paths always log)
- [ ] Run `make test && make lint`

### Success Criteria

- First successful `/healthz` and `/readyz` request is logged at INFO
- Subsequent successful health/ready requests produce no log output
- Failed health/ready requests (non-2xx) are always logged at WARN
- Non-health API paths continue to log every request at INFO
- Request ID generation and propagation still works for all paths
  (suppressed paths still set the header and context value)
- All tests pass, zero lint issues

### Files

- `internal/api/middleware/requestlog.go`
- `internal/api/middleware/requestlog_test.go`

---

## Open Questions (Resolved)

1. **Should `/metrics` also suppress logs?**

   **Decision:** Yes. Add `/metrics` to `logSuppressPaths`. Prometheus
   scrape noise is just as bad as probe noise. Scrape failures will
   still be logged every time at WARN level.

2. **`sync.Once` cannot reset after failures.**

   **Decision:** Use `sync.Once` (no `atomic.Bool`). The health gauges
   (`spt_healthz_up`, `spt_readyz_up`) serve as the recovery signal —
   Grafana/Prometheus will show the gauge flip from 0 back to 1. No
   need to duplicate that information in logs.

3. **Log level for health failures.**

   **Decision:** Failures log at `Warn`. Health check failures are
   genuinely noteworthy and should stand out from normal request traffic.
