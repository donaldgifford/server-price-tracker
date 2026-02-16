# Skip Operational Endpoints from Metrics and Reduce Health Check Log Noise

## Context

The `/metrics`, `/healthz`, and `/readyz` endpoints are hit frequently by
Kubernetes probes (~10 req/min) and Prometheus scrapes. This creates:

1. Unnecessary histogram bucket cardinality in Prometheus metrics
2. Noisy logs that drown out meaningful request activity

## Changes

### 1. Metrics middleware: skip operational paths + health gauges

**File: `internal/metrics/metrics.go`**

- Add two new gauges:
  - `HealthzUp` — Gauge, `spt_healthz_up`, 1 = ok, 0 = failing
  - `ReadyzUp` — Gauge, `spt_readyz_up`, 1 = ready, 0 = not ready

**File: `internal/api/middleware/metrics.go`**

- Add a `metricsSkipPaths` set (`map[string]struct{}`) containing `/metrics`,
  `/healthz`, `/readyz`
- Resolve the path early, check membership, and `return next(c)` immediately for
  skipped paths — but first update the health gauges:
  - `/healthz`: set `metrics.HealthzUp` to 1 (2xx) or 0
  - `/readyz`: set `metrics.ReadyzUp` to 1 (2xx) or 0
  - `/metrics`: skip entirely (no gauge needed)

**File: `internal/api/middleware/metrics_test.go`**

- Add `wantSkipped bool` field to test table
- Change the existing `/healthz` case to `wantSkipped: true`
- Add cases for `/readyz` and `/metrics` as skipped
- Use a delta-based assertion (capture counter before/after) since Prometheus
  metrics are global singletons across test runs
- Add a `getCounterValue` helper for the delta check
- Add tests verifying `HealthzUp` and `ReadyzUp` gauge values

### 2. Request log middleware: suppress repetitive health logs

**File: `internal/api/middleware/requestlog.go`**

- Add a `logSuppressPaths` set for `/healthz` and `/readyz`
- In the `RequestLog` constructor closure, create a `map[string]*sync.Once` for
  each suppress path
- On success (2xx): use `sync.Once.Do()` to log only the first hit
- On failure (non-2xx): always log at `Warn` level
- Non-suppress paths: unchanged (`Info` level every request)

**File: `internal/api/middleware/requestlog_test.go`**

- Add `TestRequestLog_HealthzFirstSuccessLogged` — first request logged, second
  and third suppressed
- Add `TestRequestLog_HealthzFailureAlwaysLogged` — failures always produce log
  output
- Add `TestRequestLog_ReadyzFirstSuccessThenFailure` — mixed scenario: first
  success logged, second suppressed, then failure logged
- Add `TestRequestLog_NonHealthPathAlwaysLogged` — confirm normal API paths
  still log every request

## Verification

```bash
make test && make lint
```

Then deploy and confirm:

- `curl /metrics | grep spt_http` shows no `/healthz`, `/readyz`, `/metrics`
  label values in histogram/counter metrics
- `curl /metrics | grep spt_healthz_up` shows `1`
- `curl /metrics | grep spt_readyz_up` shows `1`
- Logs show one initial health check line, then silence on subsequent probe hits
