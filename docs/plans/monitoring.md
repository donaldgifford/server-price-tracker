# Plan: Dashgen Tool — Grafana Dashboards and Prometheus Rules

## Context

The project has 15 Prometheus metrics (namespace `spt`) but no Grafana
dashboards or Prometheus alert/recording rules. Following the pattern from
[zfs_exporter/tools/dashgen](https://github.com/donaldgifford/zfs_exporter/tree/main/tools/dashgen),
we'll create a Go code generator that produces:

- 1 Grafana dashboard JSON (`spt-overview.json`)
- 1 Prometheus recording rules YAML (PrometheusRule CR)
- 1 Prometheus alert rules YAML (PrometheusRule CR)

Generated artifacts written to `deploy/grafana/data/` and `deploy/prometheus/`.

## Decisions

- **Datasource**: Template variable `${datasource}` with `type: prometheus`
- **Deploy**: Kustomize in `deploy/prometheus/` and `deploy/grafana/`
- **PrometheusRule namespace**: Omitted — Kustomize handles it

## Tool Structure

```
tools/dashgen/              # Standalone Go module
├── go.mod
├── go.sum
├── main.go                 # Entry point, -validate flag
├── generate.go             # //go:generate go run .
├── config.go               # KnownMetrics, Config, DefaultConfig
├── dashgen_test.go         # Config, build, staleness tests
├── panels/
│   ├── helpers.go          # PromQuery builder, DSRef, constants
│   ├── overview.go         # Health stats, quota gauge
│   ├── http.go             # Request rate, latency, error rate
│   ├── ebay.go             # API calls, daily usage, limit hits
│   ├── ingestion.go        # Listings rate, errors, duration
│   ├── extraction.go       # LLM duration, failures
│   ├── scoring.go          # Score distribution
│   └── alerts.go           # Alerts fired, notification failures
├── dashboards/
│   └── overview.go         # BuildOverview() → single dashboard
├── rules/
│   ├── types.go            # PrometheusRule YAML structs
│   ├── recording.go        # 6 recording rules
│   └── alerts.go           # 8 alert rules
└── validate/
    └── validate.go         # PromQL syntax + metrics cross-ref
```

## Output Artifacts

```
deploy/
├── grafana/
│   └── data/
│       └── spt-overview.json
└── prometheus/
    ├── spt-recording-rules.yaml
    └── spt-alerts.yaml
```

## Metrics Inventory

| Group | Metric | Type | Labels |
|-------|--------|------|--------|
| HTTP | `spt_http_request_duration_seconds` | histogram | method, path, status |
| HTTP | `spt_http_requests_total` | counter | method, path, status |
| Health | `spt_healthz_up` | gauge | — |
| Health | `spt_readyz_up` | gauge | — |
| Ingestion | `spt_ingestion_listings_total` | counter | — |
| Ingestion | `spt_ingestion_errors_total` | counter | — |
| Ingestion | `spt_ingestion_duration_seconds` | histogram | — |
| Extraction | `spt_extraction_duration_seconds` | histogram | — |
| Extraction | `spt_extraction_failures_total` | counter | — |
| Scoring | `spt_scoring_distribution` | histogram | — |
| eBay | `spt_ebay_api_calls_total` | counter | — |
| eBay | `spt_ebay_daily_usage` | gauge | — |
| eBay | `spt_ebay_daily_limit_hits_total` | counter | — |
| Alerts | `spt_alerts_fired_total` | counter | — |
| Alerts | `spt_notification_failures_total` | counter | — |

## Recording Rules

| Rule | Expression |
|------|-----------|
| `spt:http_requests:rate5m` | `sum(rate(spt_http_requests_total[5m]))` |
| `spt:http_errors:rate5m` | `sum(rate(spt_http_requests_total{status=~"5.."}[5m]))` |
| `spt:ingestion_listings:rate5m` | `rate(spt_ingestion_listings_total[5m])` |
| `spt:ingestion_errors:rate5m` | `rate(spt_ingestion_errors_total[5m])` |
| `spt:extraction_failures:rate5m` | `rate(spt_extraction_failures_total[5m])` |
| `spt:ebay_api_calls:rate5m` | `rate(spt_ebay_api_calls_total[5m])` |

## Alert Rules

| Alert | Expression | For | Severity |
|-------|-----------|-----|----------|
| `SptDown` | `absent(up{job="server-price-tracker"})` | 2m | critical |
| `SptReadinessDown` | `spt_readyz_up == 0` | 2m | critical |
| `SptHighErrorRate` | `spt:http_errors:rate5m / spt:http_requests:rate5m > 0.05` | 5m | warning |
| `SptIngestionErrors` | `spt:ingestion_errors:rate5m > 0` | 5m | warning |
| `SptExtractionFailures` | `spt:extraction_failures:rate5m > 0.1` | 5m | warning |
| `SptEbayQuotaHigh` | `spt_ebay_daily_usage > 4000` | 5m | warning |
| `SptEbayLimitReached` | `increase(spt_ebay_daily_limit_hits_total[5m]) > 0` | 0m | critical |
| `SptNotificationFailures` | `increase(spt_notification_failures_total[5m]) > 0` | 1m | warning |

## Dashboard Layout

Single dashboard: **SPT Overview** (UID: `spt-overview`, datasource: `${datasource}`)

| Row | Panels |
|-----|--------|
| **Overview** | Healthz (stat), Readyz (stat), eBay Quota % (gauge), Uptime (stat) |
| **HTTP** | Request Rate (timeseries), Latency p50/p95/p99 (timeseries), Error Rate % (timeseries) |
| **eBay API** | API Calls Rate (timeseries), Daily Usage vs Limit (timeseries), Limit Hits (stat) |
| **Ingestion** | Listings/min (timeseries), Errors/min (timeseries), Cycle Duration (timeseries) |
| **Extraction** | Duration p50/p95 (timeseries), Failure Rate (timeseries) |
| **Scoring** | Score Distribution (bar gauge or heatmap) |
| **Alerts** | Alerts Fired Rate (timeseries), Notification Failures (stat) |

---

## Phase 0: Scaffolding and Config

### Tasks

- [ ] Create `tools/dashgen/go.mod` (separate module)
- [ ] Add deps: `grafana-foundation-sdk/go`, `prometheus/prometheus`, `gopkg.in/yaml.v3`
- [ ] Create `tools/dashgen/generate.go` (`//go:generate go run .`)
- [ ] Create `tools/dashgen/config.go`:
  - `KnownMetrics` map (15 spt metrics + 6 recording rules)
  - `Config` struct with `OutputDir`, `DashboardEnabled`, `RulesEnabled`
  - `DefaultConfig` with output to `../../deploy`
  - `Validate()` method
- [ ] Create `tools/dashgen/main.go`:
  - `-validate` flag
  - Load config, validate, orchestrate build + write
- [ ] Run `go build ./tools/dashgen/`

### Success Criteria

- `go build` succeeds
- `go run ./tools/dashgen/ -validate` runs without error

### Files

- `tools/dashgen/go.mod`
- `tools/dashgen/generate.go`
- `tools/dashgen/config.go`
- `tools/dashgen/main.go`

---

## Phase 1: Prometheus Rules

### Tasks

- [ ] Create `tools/dashgen/rules/types.go`:
  - `PrometheusRule`, `RuleGroup`, `Rule` structs (YAML tags)
  - Kubernetes CR structure (apiVersion, kind, metadata, spec)
- [ ] Create `tools/dashgen/rules/recording.go`:
  - `RecordingRules() *PrometheusRule` returning 6 recording rules
- [ ] Create `tools/dashgen/rules/alerts.go`:
  - `AlertRules() *PrometheusRule` returning 8 alert rules
- [ ] Wire rules generation into `main.go`
- [ ] Run and verify YAML output in `deploy/prometheus/`

### Success Criteria

- Valid Kubernetes PrometheusRule CRs
- Syntactically correct PromQL expressions
- Files written to `deploy/prometheus/`

### Files

- `tools/dashgen/rules/types.go`
- `tools/dashgen/rules/recording.go`
- `tools/dashgen/rules/alerts.go`
- `tools/dashgen/main.go` (updated)
- `deploy/prometheus/spt-recording-rules.yaml` (generated)
- `deploy/prometheus/spt-alerts.yaml` (generated)

---

## Phase 2: Panel Builders

### Tasks

- [ ] Create `tools/dashgen/panels/helpers.go`:
  - `DSRef()` — `{"type": "prometheus", "uid": "${datasource}"}`
  - `PromQuery(expr, legend)` — Prometheus query target
  - Color/threshold constants
- [ ] Create panel files:
  - `panels/overview.go` — `HealthzStat()`, `ReadyzStat()`, `QuotaGauge()`, `UptimeStat()`
  - `panels/http.go` — `RequestRate()`, `LatencyPercentiles()`, `ErrorRate()`
  - `panels/ebay.go` — `APICallsRate()`, `DailyUsage()`, `LimitHits()`
  - `panels/ingestion.go` — `ListingsRate()`, `IngestionErrors()`, `CycleDuration()`
  - `panels/extraction.go` — `ExtractionDuration()`, `ExtractionFailures()`
  - `panels/scoring.go` — `ScoreDistribution()`
  - `panels/alerts.go` — `AlertsRate()`, `NotificationFailures()`
- [ ] Verify: `go build ./tools/dashgen/...`

### Success Criteria

- All panel builder functions compile
- Each returns a valid Grafana Foundation SDK panel builder

### Files

- `tools/dashgen/panels/*.go` (8 files)

---

## Phase 3: Dashboard Builder

### Tasks

- [ ] Create `tools/dashgen/dashboards/overview.go`:
  - `BuildOverview() *dashboard.DashboardBuilder`
  - Template variable: `datasource` (Prometheus type)
  - 7 collapsible rows using panel builders
  - UID: `spt-overview`, refresh 30s, tags: `["spt", "server-price-tracker"]`
- [ ] Wire dashboard build + JSON write into `main.go`
- [ ] Run and verify JSON output

### Success Criteria

- Valid Grafana dashboard JSON with all rows
- Importable in Grafana (manual check)

### Files

- `tools/dashgen/dashboards/overview.go`
- `tools/dashgen/main.go` (updated)
- `deploy/grafana/data/spt-overview.json` (generated)

---

## Phase 4: Validation + Tests

### Tasks

- [ ] Create `tools/dashgen/validate/validate.go`:
  - PromQL syntax check (replace `${datasource}` etc before parsing)
  - Known metrics cross-reference
  - Unique panel ID check
  - `Result{Errors, Warnings}` with `Ok()` method
- [ ] Wire validation into `main.go`
- [ ] Create `tools/dashgen/dashgen_test.go`:
  - `TestDefaultConfigValid`
  - `TestBuildOverviewDashboard` — build + validate
  - `TestRecordingRules` — expected rule count/names
  - `TestAlertRules` — expected alert count/names
  - `TestStaleness` — regenerate, compare with committed files
- [ ] Run: `cd tools/dashgen && go test ./...`

### Success Criteria

- All tests pass
- Staleness test catches uncommitted changes
- `-validate` flag exits 0

### Files

- `tools/dashgen/validate/validate.go`
- `tools/dashgen/dashgen_test.go`

---

## Phase 5: Make Targets

### Tasks

- [ ] Create `scripts/makefiles/dashgen.mk`:
  - `dashboards`: `cd tools/dashgen && go run .`
  - `dashboards-validate`: `cd tools/dashgen && go run . -validate`
  - `dashboards-test`: `cd tools/dashgen && go test ./...`
- [ ] Include `dashgen.mk` in root `Makefile`

### Success Criteria

- `make dashboards` generates all 3 artifacts
- `make dashboards-validate` works for CI

### Files

- `scripts/makefiles/dashgen.mk`
- `Makefile` (include added)
