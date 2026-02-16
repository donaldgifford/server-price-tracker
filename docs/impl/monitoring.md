# Implementation: Dashgen Tool — Grafana Dashboards and Prometheus Rules

## Context

The project exposes 15 Prometheus metrics (namespace `spt`) but has no Grafana
dashboards or Prometheus alert/recording rules. Following the pattern from
[zfs_exporter/tools/dashgen](https://github.com/donaldgifford/zfs_exporter/tree/main/tools/dashgen),
we create a standalone Go code generator (`tools/dashgen/`) that produces:

- 1 Grafana dashboard JSON (`spt-overview.json`)
- 1 Prometheus recording rules YAML (PrometheusRule CR)
- 1 Prometheus alert rules YAML (PrometheusRule CR)

Generated artifacts are written to `deploy/grafana/data/` and
`deploy/prometheus/`. The tool uses the Grafana Foundation SDK for type-safe
dashboard construction and `prometheus/prometheus` for PromQL validation.

See `docs/plans/monitoring.md` for the high-level design and layout.

---

## Phase 0: Scaffolding and Config

### Tasks

- [x] Create `tools/dashgen/go.mod` as a separate Go module:
  - Module path: `github.com/donaldgifford/server-price-tracker/tools/dashgen`
  - Go version: match root module (`go 1.25.7`)
  - Dependencies:
    - `github.com/grafana/grafana-foundation-sdk/go` (latest v0.x)
    - `github.com/prometheus/prometheus` (for PromQL parser in validation)
    - `gopkg.in/yaml.v3` (for PrometheusRule YAML output)
    - `github.com/stretchr/testify` (for tests)
  - Run `go mod tidy` to resolve transitive deps
- [x] Create `tools/dashgen/generate.go`:
  - Single line: `//go:generate go run .`
  - Package `main`
- [x] Create `tools/dashgen/config.go`:
  - `KnownMetrics` — `map[string]bool` containing all 15 `spt_*` metrics
    plus the 6 recording rule names (`spt:http_requests:rate5m`, etc.)
  - `Config` struct with fields:
    - `OutputDir string` — base output directory (default `../../deploy`)
    - `DashboardEnabled bool` — whether to generate the dashboard JSON
    - `RulesEnabled bool` — whether to generate recording/alert rules
  - `DefaultConfig()` — returns config with `OutputDir: "../../deploy"`,
    both enabled
  - `Validate() error` — checks `OutputDir` is non-empty, at least one
    output enabled
- [x] Create `tools/dashgen/main.go`:
  - `-validate` flag — run validation only, no file writes
  - `-output` flag — override output directory
  - Load `DefaultConfig()`, apply flag overrides
  - Call `config.Validate()`
  - Orchestrate: build dashboard + rules, write to disk, optionally validate
  - Print summary of generated files
  - Exit 1 on any error
- [x] Run `cd tools/dashgen && go build .` to verify compilation

### Success Criteria

- `go build ./tools/dashgen/` succeeds with no errors
- `go run ./tools/dashgen/ -validate` runs and exits 0 (with stub/empty
  builders returning nil at this stage — wire actual builders in later phases)
- `go vet ./tools/dashgen/...` clean

### Files

- `tools/dashgen/go.mod`
- `tools/dashgen/go.sum`
- `tools/dashgen/generate.go`
- `tools/dashgen/config.go`
- `tools/dashgen/main.go`

---

## Phase 1: Prometheus Rules

### Tasks

- [x] Create `tools/dashgen/rules/types.go`:
  - `PrometheusRule` struct — Kubernetes CR with `apiVersion`, `kind`,
    `metadata`, `spec` fields (YAML tags)
  - `PrometheusRuleMetadata` struct — `name`, `labels` fields
  - `PrometheusRuleSpec` struct — `groups []RuleGroup`
  - `RuleGroup` struct — `name`, `interval` (optional), `rules []Rule`
  - `Rule` struct — `record` (for recording), `alert` (for alerts),
    `expr`, `for` (optional), `labels`, `annotations` fields
  - `RuleFile` struct — standalone (non-CR) wrapper with `groups` field,
    for raw Prometheus rule files if needed
  - All structs must have `yaml:"..."` tags, with `omitempty` on optional
    fields
- [x] Create `tools/dashgen/rules/recording.go`:
  - `RecordingRules() PrometheusRule` returning a CR with 6 recording rules:
    - `spt:http_requests:rate5m` — `sum(rate(spt_http_requests_total[5m]))`
    - `spt:http_errors:rate5m` — `sum(rate(spt_http_requests_total{status=~"5.."}[5m]))`
    - `spt:ingestion_listings:rate5m` — `rate(spt_ingestion_listings_total[5m])`
    - `spt:ingestion_errors:rate5m` — `rate(spt_ingestion_errors_total[5m])`
    - `spt:extraction_failures:rate5m` — `rate(spt_extraction_failures_total[5m])`
    - `spt:ebay_api_calls:rate5m` — `rate(spt_ebay_api_calls_total[5m])`
  - CR metadata: `name: spt-recording-rules`, no namespace (Kustomize handles
    it), label `prometheus: system-rules-prometheus`
  - Rule group name: `spt-recording`
- [x] Create `tools/dashgen/rules/alerts.go`:
  - `AlertRules() PrometheusRule` returning a CR with 8 alert rules:
    - `SptDown` — `absent(up{job="server-price-tracker"})`, for 2m, critical
    - `SptReadinessDown` — `spt_readyz_up == 0`, for 2m, critical
    - `SptHighErrorRate` — `spt:http_errors:rate5m / spt:http_requests:rate5m > 0.05`,
      for 5m, warning
    - `SptIngestionErrors` — `spt:ingestion_errors:rate5m > 0`, for 5m, warning
    - `SptExtractionFailures` — `spt:extraction_failures:rate5m > 0.1`, for 5m, warning
    - `SptEbayQuotaHigh` — `spt_ebay_daily_usage > 4000`, for 5m, warning
    - `SptEbayLimitReached` — `increase(spt_ebay_daily_limit_hits_total[5m]) > 0`,
      for 0m, critical
    - `SptNotificationFailures` — `increase(spt_notification_failures_total[5m]) > 0`,
      for 1m, warning
  - Each alert must have `summary` and `description` annotations
  - CR metadata: `name: spt-alerts`, label `prometheus: system-rules-prometheus`
  - Rule group name: `spt-alerts`
- [x] Wire rules generation into `main.go`:
  - Call `rules.RecordingRules()` and `rules.AlertRules()`
  - Marshal each to YAML with `yaml.Marshal`
  - Prepend `# Code generated by tools/dashgen; DO NOT EDIT.\n` header
  - Write to `<outputDir>/prometheus/spt-recording-rules.yaml` and
    `<outputDir>/prometheus/spt-alerts.yaml`
  - Create `deploy/prometheus/` directory if it doesn't exist
- [x] Run `cd tools/dashgen && go run .` and inspect YAML output

### Success Criteria

- Generated YAML files are valid Kubernetes PrometheusRule CRs
  (`apiVersion: monitoring.coreos.com/v1`, `kind: PrometheusRule`)
- All PromQL expressions are syntactically valid (verified by
  `promparser.ParseExpr` in Phase 4)
- `kubectl apply --dry-run=client -f deploy/prometheus/spt-recording-rules.yaml`
  would succeed (valid YAML structure)
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

- [x] Create `tools/dashgen/panels/helpers.go`:
  - `DSRef() dashboard.DataSourceRef` — returns
    `{Type: "prometheus", UID: "${datasource}"}`
  - `PromQuery(expr, legend string) *prometheus.Dataquery` — builds a
    Prometheus query target with `Expr`, `LegendFormat`, and datasource ref
  - `TimeseriesPanel(title string, queries ...*prometheus.Dataquery) *dashboard.Panel`
    — convenience wrapper for timeseries panel with standard defaults
    (unit, legend placement, etc.)
  - `StatPanel(title string, query *prometheus.Dataquery) *dashboard.Panel`
    — stat panel builder
  - `GaugePanel(title string, query *prometheus.Dataquery, min, max float64) *dashboard.Panel`
    — gauge panel builder with configurable range
  - Color/threshold constants for reuse across panels (e.g., red/green
    for health, warning thresholds)
  - Panel ID counter — auto-incrementing `uint32` to assign unique IDs
    (can use a package-level counter or accept an `*idGen` parameter)
- [x] Create `tools/dashgen/panels/overview.go`:
  - `HealthzStat() cog.Builder[dashboard.Panel]` — stat panel showing
    `spt_healthz_up`, green=1/red=0 thresholds
  - `ReadyzStat() cog.Builder[dashboard.Panel]` — stat panel showing
    `spt_readyz_up`, green=1/red=0 thresholds
  - `QuotaGauge() cog.Builder[dashboard.Panel]` — gauge showing
    `spt_ebay_daily_usage / 5000 * 100` as percentage, thresholds at
    80% (warning) and 95% (critical)
  - `UptimeStat() cog.Builder[dashboard.Panel]` — stat showing
    `time() - process_start_time_seconds{job="server-price-tracker"}` or
    similar uptime query
- [x] Create `tools/dashgen/panels/http.go`:
  - `RequestRate() cog.Builder[dashboard.Panel]` — timeseries,
    `spt:http_requests:rate5m`, unit: reqps
  - `LatencyPercentiles() cog.Builder[dashboard.Panel]` — timeseries with
    3 queries for p50/p95/p99:
    `histogram_quantile(0.50, rate(spt_http_request_duration_seconds_bucket[5m]))`,
    etc.
  - `ErrorRate() cog.Builder[dashboard.Panel]` — timeseries,
    `spt:http_errors:rate5m / spt:http_requests:rate5m * 100`, unit: percent
- [x] Create `tools/dashgen/panels/ebay.go`:
  - `APICallsRate() cog.Builder[dashboard.Panel]` — timeseries,
    `spt:ebay_api_calls:rate5m`
  - `DailyUsage() cog.Builder[dashboard.Panel]` — timeseries,
    `spt_ebay_daily_usage` with a constant threshold line at 5000
  - `LimitHits() cog.Builder[dashboard.Panel]` — stat,
    `increase(spt_ebay_daily_limit_hits_total[24h])`
- [x] Create `tools/dashgen/panels/ingestion.go`:
  - `ListingsRate() cog.Builder[dashboard.Panel]` — timeseries,
    `spt:ingestion_listings:rate5m * 60` (listings/min)
  - `IngestionErrors() cog.Builder[dashboard.Panel]` — timeseries,
    `spt:ingestion_errors:rate5m * 60` (errors/min)
  - `CycleDuration() cog.Builder[dashboard.Panel]` — timeseries,
    `histogram_quantile(0.95, rate(spt_ingestion_duration_seconds_bucket[5m]))`
- [x] Create `tools/dashgen/panels/extraction.go`:
  - `ExtractionDuration() cog.Builder[dashboard.Panel]` — timeseries with
    p50 and p95 quantiles from `spt_extraction_duration_seconds`
  - `ExtractionFailures() cog.Builder[dashboard.Panel]` — timeseries,
    `spt:extraction_failures:rate5m`
- [x] Create `tools/dashgen/panels/scoring.go`:
  - `ScoreDistribution() cog.Builder[dashboard.Panel]` — bar gauge or
    heatmap showing `spt_scoring_distribution_bucket` histogram
- [x] Create `tools/dashgen/panels/alerts.go`:
  - `AlertsRate() cog.Builder[dashboard.Panel]` — timeseries,
    `rate(spt_alerts_fired_total[5m])`
  - `NotificationFailures() cog.Builder[dashboard.Panel]` — stat,
    `increase(spt_notification_failures_total[24h])`
- [x] Verify: `cd tools/dashgen && go build ./...`

### Success Criteria

- All panel builder functions compile without error
- Each function returns a valid Grafana Foundation SDK panel builder
- `go vet ./tools/dashgen/...` clean
- All panel IDs are unique (enforced by the ID counter approach)

### Files

- `tools/dashgen/panels/helpers.go`
- `tools/dashgen/panels/overview.go`
- `tools/dashgen/panels/http.go`
- `tools/dashgen/panels/ebay.go`
- `tools/dashgen/panels/ingestion.go`
- `tools/dashgen/panels/extraction.go`
- `tools/dashgen/panels/scoring.go`
- `tools/dashgen/panels/alerts.go`

---

## Phase 3: Dashboard Builder

### Tasks

- [ ] Create `tools/dashgen/dashboards/overview.go`:
  - `BuildOverview() *dashboard.DashboardBuilder`
  - Dashboard properties:
    - UID: `spt-overview`
    - Title: `SPT Overview`
    - Tags: `["spt", "server-price-tracker"]`
    - Refresh: `30s`
    - Timezone: `browser`
    - Editable: `true`
  - Template variable: `datasource` — type `prometheus`, label
    `Datasource`, regex empty (shows all Prometheus datasources)
  - 7 collapsible rows, each using panel builders from Phase 2:
    1. **Overview** — HealthzStat, ReadyzStat, QuotaGauge, UptimeStat
    2. **HTTP** — RequestRate, LatencyPercentiles, ErrorRate
    3. **eBay API** — APICallsRate, DailyUsage, LimitHits
    4. **Ingestion** — ListingsRate, IngestionErrors, CycleDuration
    5. **Extraction** — ExtractionDuration, ExtractionFailures
    6. **Scoring** — ScoreDistribution
    7. **Alerts** — AlertsRate, NotificationFailures
  - Panel grid positioning: use `GridPos` with appropriate `H`, `W`, `X`,
    `Y` values for a 24-column layout
- [ ] Wire dashboard build + JSON write into `main.go`:
  - Call `dashboards.BuildOverview()`
  - Build the dashboard via `.Build()`
  - Marshal to JSON with `json.MarshalIndent` (2-space indent)
  - Prepend no header (JSON doesn't support comments — add a top-level
    `_generated` key or rely on the file being in a `data/` directory)
  - Write to `<outputDir>/grafana/data/spt-overview.json`
  - Create `deploy/grafana/data/` directory if it doesn't exist
- [ ] Run `cd tools/dashgen && go run .` and inspect JSON output
- [ ] Manually import `deploy/grafana/data/spt-overview.json` into a
  Grafana instance to verify it loads without errors (optional manual check)

### Success Criteria

- Valid Grafana dashboard JSON with all 7 rows and expected panels
- Dashboard has the `datasource` template variable
- JSON is well-formed and importable by Grafana 10+
- UID is `spt-overview`, refresh is `30s`
- All panels reference `${datasource}` correctly

### Files

- `tools/dashgen/dashboards/overview.go`
- `tools/dashgen/main.go` (updated)
- `deploy/grafana/data/spt-overview.json` (generated)

---

## Phase 4: Validation and Tests

### Tasks

- [ ] Create `tools/dashgen/validate/validate.go`:
  - `Result` struct with `Errors []string` and `Warnings []string`
  - `Ok() bool` — returns true if no errors
  - `Dashboard(dash dashboard.Dashboard) Result` — validates a built dashboard:
    - **PromQL syntax check**: parse every panel target expression with
      `promparser.ParseExpr` after replacing Grafana template variables
      (`${datasource}`, `$pool`, etc.) with `.*` placeholders
    - **Known metrics cross-reference**: extract metric names from PromQL
      AST, warn if any are not in `KnownMetrics`
    - **Unique panel ID check**: verify no two panels share the same ID
  - `FormatResult(name string, r Result) string` — human-readable output
  - Internal helpers:
    - `collectPanels(dash) []panel` — flatten panels including those
      inside collapsed row panels
    - `extractPanel(p dashboard.Panel) panel` — extract title, ID, targets
    - `checkPromQL(r, title, panels)` — parse and validate expressions
    - `checkMetricNames(r, title, panels)` — cross-reference metric names
    - `checkUniqueIDs(r, title, panels)` — check for duplicate IDs
    - `grafanaVarRe` — regex `\$\{?\w+\}?` for template variable matching
- [ ] Wire validation into `main.go`:
  - After building dashboard + rules, run `validate.Dashboard()` on the
    built dashboard
  - If `-validate` flag is set, print results and exit
  - On errors, exit 1
  - On warnings only, print and continue
- [ ] Create `tools/dashgen/dashgen_test.go`:
  - `TestDefaultConfigValid` — `DefaultConfig()` passes `Validate()`
  - `TestBuildOverviewDashboard`:
    - Build the dashboard via `dashboards.BuildOverview()`
    - Run `validate.Dashboard()` on the result
    - Assert `result.Ok()` is true
    - Assert no warnings (or expected warnings only)
    - Assert expected panel count (sum of all panels across rows)
    - Assert dashboard UID is `spt-overview`
  - `TestRecordingRules`:
    - Call `rules.RecordingRules()`
    - Assert exactly 6 rules in the group
    - Assert each rule has the expected `record` name
    - Marshal to YAML and verify it's valid
  - `TestAlertRules`:
    - Call `rules.AlertRules()`
    - Assert exactly 8 rules in the group
    - Assert each rule has the expected `alert` name
    - Assert severity labels are set
    - Assert annotations contain `summary` and `description`
  - `TestStaleness`:
    - Run the full generation pipeline (dashboard + rules)
    - Compare generated bytes with the committed files in
      `deploy/grafana/data/spt-overview.json`,
      `deploy/prometheus/spt-recording-rules.yaml`,
      `deploy/prometheus/spt-alerts.yaml`
    - Assert byte-for-byte equality — if they differ, the committed
      artifacts are stale and need regeneration
    - This test catches cases where someone edits a panel builder but
      forgets to re-run `go generate`
- [ ] Run: `cd tools/dashgen && go test ./...`

### Success Criteria

- All tests pass
- `-validate` flag exits 0 with clean output
- Staleness test catches uncommitted changes (fails if artifacts differ
  from regenerated output)
- PromQL validation covers all panel expressions and rule expressions
- Known metrics cross-reference catches typos in metric names

### Files

- `tools/dashgen/validate/validate.go`
- `tools/dashgen/dashgen_test.go`

---

## Phase 5: Make Targets

### Tasks

- [ ] Create `scripts/makefiles/dashgen.mk`:
  - `dashboards` target: `cd tools/dashgen && go run .`
    - Help text: `Generate Grafana dashboards and Prometheus rules`
  - `dashboards-validate` target: `cd tools/dashgen && go run . -validate`
    - Help text: `Validate generated dashboards and rules`
  - `dashboards-test` target: `cd tools/dashgen && go test ./...`
    - Help text: `Run dashgen tests`
  - All targets should be `.PHONY`
  - Follow the existing makefile pattern from other `.mk` files
    (section header with `##@`, help annotations with `##`)
- [ ] Update root `Makefile`:
  - Add `include scripts/makefiles/dashgen.mk` after the existing includes
- [ ] Verify:
  - `make dashboards` generates all 3 artifacts
  - `make dashboards-validate` exits 0
  - `make dashboards-test` runs tests
  - `make help` shows the new targets

### Success Criteria

- `make dashboards` generates `deploy/grafana/data/spt-overview.json`,
  `deploy/prometheus/spt-recording-rules.yaml`, and
  `deploy/prometheus/spt-alerts.yaml`
- `make dashboards-validate` is suitable for CI (exits non-zero on errors)
- `make dashboards-test` runs all dashgen tests
- New targets appear in `make help` output

### Files

- `scripts/makefiles/dashgen.mk`
- `Makefile` (updated)

---

## Decisions (Resolved)

1. **Grafana Foundation SDK version**: Pin to `v0.0.7` to match the
   zfs_exporter reference. Upgrade later if needed.

2. **Panel return types**: Use the builder pattern
   (`*stat.PanelBuilder`, `*timeseries.PanelBuilder`, etc.) to match
   the reference implementation and get compile-time type safety.

3. **`process_start_time_seconds` for uptime**: Include
   `process_start_time_seconds` and `up` in `KnownMetrics` as standard
   Prometheus metrics referenced by dashboard panels.

4. **Deploy directory structure**: Directories created manually.
   Generator writes files directly; committed artifacts are the
   deployment source of truth. Staleness test ensures they stay in sync.

5. **PrometheusRule label selector**: Use
   `prometheus: system-rules-prometheus` to match the existing cluster
   convention. Override via Kustomize overlay if needed.

6. **eBay daily limit constant**: Define `const EbayDailyLimit = 5000`
   in `panels/helpers.go`. Referenced by both the quota gauge panel and
   alert threshold to keep values in sync.
