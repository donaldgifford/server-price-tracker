---
id: IMPL-0019
title: "DESIGN-0016 OpenTelemetry Clickhouse and Langfuse instrumentation phase plan"
status: Draft
author: Donald Gifford
created: 2026-05-03
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0019: DESIGN-0016 OpenTelemetry Clickhouse and Langfuse instrumentation phase plan

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-05-03

<!--toc:start-->
- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Guiding Principles](#guiding-principles)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: OTel SDK foundation + config wiring](#phase-1-otel-sdk-foundation--config-wiring)
  - [Phase 2: Pipeline span instrumentation](#phase-2-pipeline-span-instrumentation)
  - [Phase 3: Langfuse client + LLM-call generations](#phase-3-langfuse-client--llm-call-generations)
  - [Phase 4: Trace deep-links + dismissal-as-score in alert review UI](#phase-4-trace-deep-links--dismissal-as-score-in-alert-review-ui)
  - [Phase 5: Async LLM-as-judge worker](#phase-5-async-llm-as-judge-worker)
  - [Phase 6: Golden dataset + operator-run regression](#phase-6-golden-dataset--operator-run-regression)
  - [Phase 7: Production rollout + Grafana panels](#phase-7-production-rollout--grafana-panels)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Wire OpenTelemetry traces and metrics, Langfuse LLM-call observability,
operator-dismissal-as-score capture, and an async LLM-as-judge worker
into `server-price-tracker`. End state: any single fired alert is
clickable from the alert review UI back to the full trace
(ingestion → classify → extract → normalise → score → notify), and a
judge LLM has independently scored its quality on a 0.0–1.0 rubric.

Everything is config-gated and defaults off — a deployment with all
observability subtrees disabled behaves identically to today's
`v0.8.x` release.

**Implements:** DESIGN-0016

## Scope

### In Scope

- OTel Go SDK setup (`go.opentelemetry.io/otel`) with OTLP/gRPC
  exporter pointing at an externally-deployed Collector. App emits
  100% of spans (`AlwaysSample`); the Collector applies tail
  sampling.
- Span instrumentation for the pipeline stages (ingestion,
  pre-classify, classify, extract, normalise, validate, score,
  alert-eval, notify).
- Trace ID propagation through the extraction queue
  (new `extraction_queue.trace_id` and `alerts.trace_id` columns).
- **In-house** Langfuse HTTP client (no third-party SDK) wrapping
  `LLMBackend.Generate` via decorator so every LLM call becomes a
  Langfuse generation.
- Async + bounded buffer for Langfuse writes so transient outages
  don't block the extract path; 4 Prometheus metrics expose buffer
  health.
- Auto-score on extraction self-confidence; binary score on operator
  dismiss; judge score on fired alerts.
- Async judge worker as a new cron job in
  `internal/engine/scheduler.go`. Default `15m` interval, `6h`
  lookback. Hard `$10/day` budget cutoff. Judge LLM tracks the
  extract backend's model.
- `judge_scores` Postgres table mirrors what we send to Langfuse so
  the alert review UI keeps working when Langfuse is unreachable.
- Operator-facing CLI: `spt judge run --since <duration>`.
- Operator-facing HTTP: `GET /api/v1/alerts/{id}/trace`.
- Alert review UI: "View trace" button + judge-score column.
- Dual-emit: keep current Prometheus metrics; add OTel metrics in
  parallel.
- Three-mode config (`observability.otel`, `observability.langfuse`,
  `observability.judge`), each independently disable-able.
- Golden dataset of ~100 labelled listings + operator-run regression
  script (`make test-regression`). No CI workflow; the operator (or
  a Claude Code session under operator instruction) runs it on
  demand.

### Out of Scope

- Deploying Clickhouse or Langfuse — assumed to exist as platform
  infrastructure (separate Helm charts, separate ownership).
- Replacing or rebuilding the existing Grafana dashboards (additive
  only).
- Gating Discord notifications on judge score (v2 conversation; v1
  is async-only).
- Replacing slog or removing existing Prometheus metrics.
- App-side sampling tuning. Sampling lives in the Collector's
  `tail_sampling` processor; configured platform-side at deploy
  time.
- Prompt management in Langfuse UI — prompts stay in code, with
  commit SHA tagged on every generation.
- A GitHub Actions regression workflow. Phase 6 is operator-run
  tooling only; no API keys in CI.

## Guiding Principles

1. **Default off, fully optional.** Every subtree
   (`otel` / `langfuse` / `judge`) is independently disable-able.
   Existing deployments must work unchanged after the upgrade until
   they opt in.
2. **No-op fallbacks.** When a backend is unconfigured, the SDK
   wrapper returns a no-op tracer / no-op generation client.
   Hot-path code never branches on "is observability enabled" — it
   just calls the wrapper and the wrapper handles the off case.
3. **Prompts stay in code.** Tag every Langfuse generation with the
   git commit SHA at build time
   (`-ldflags "-X main.commitSHA=..."`).
4. **Dual-emit indefinitely.** OTel metrics ship alongside Prometheus,
   not instead of. Existing Grafana dashboards stay green.
5. **One change per PR.** Each phase ships as its own PR with the
   appropriate semver label
   (Phases 1-2 `minor`, Phase 3+ `minor`, fixes `patch`).

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all
its tasks are checked off and its success criteria are met.

---

### Phase 1: OTel SDK foundation + config wiring

Stand up the OTel Go SDK plumbing and the three-subtree config block.
No spans are emitted yet — this is the scaffolding that later phases
hang spans off. **Defaults must keep the app behaviour identical to
today.**

#### Tasks

- [ ] Add `observability` block to `internal/config/config.go`:
      `Otel`, `Langfuse`, `Judge` sub-structs each with an `Enabled
      bool` and backend-specific fields (endpoint, sampling,
      timeouts).
- [ ] Mirror in `configs/config.example.yaml` and
      `configs/config.dev.yaml` with `enabled: false` defaults.
- [ ] Add `internal/observability/otel.go`:
      `Init(ctx, cfg) (shutdown func(context.Context) error, err
      error)`. Returns no-op tracer + no-op meter when
      `cfg.Otel.Enabled = false`.
- [ ] Wire OTLP/gRPC trace exporter with `sdktrace.AlwaysSample()`.
      App emits 100% of spans; sampling decisions happen in the
      Collector's `tail_sampling` processor (platform-side
      requirement). Service name, version, and commit SHA attached
      as resource attributes.
- [ ] Document the Collector deployment requirement in
      `docs/OPERATIONS.md`: `tail_sampling` processor must be
      configured with policies (1) keep 100% of traces that produced
      an alert, (2) keep 100% of error traces, (3) keep 100% of
      extract spans, (4) sample N% of clean ingestion-only traces
      (operator-tunable, suggested 10%).
- [ ] Call `observability.Init` from `cmd/server-price-tracker`
      `serve` action; defer shutdown.
- [ ] Add `internal/observability/otel_test.go`:
      verify Init with `Enabled=false` returns no-op + no error;
      Init with `Enabled=true` against a fake collector emits one
      smoke span successfully.
- [ ] Update `Makefile`: add `-ldflags "-X
      github.com/donaldgifford/server-price-tracker/internal/version.CommitSHA=$(git
      rev-parse HEAD)"` to `make build` so commit SHA is available
      at runtime.
- [ ] Add a `internal/version` package with `CommitSHA string`
      (overridden via ldflags), default `"dev"`.
- [ ] Run `make lint` + `make test`.

#### Success Criteria

- `make build` succeeds; binary embeds commit SHA.
- `make test` passes including new `observability_test.go`.
- `make lint` is clean.
- Starting the server with `observability.otel.enabled: false`
  (default) emits zero OTLP traffic and produces zero new log lines.
- Starting the server with `observability.otel.enabled: true` against
  a stood-up local collector successfully exports one resource-only
  smoke span (no pipeline instrumentation yet).

---

### Phase 2: Pipeline span instrumentation

Wrap each pipeline stage in an OTel span. Trace IDs propagate through
the extraction queue and into the alerts table so future deep-links
can resolve to the right trace.

#### Tasks

- [ ] **Verify migration number is still free.** Run
      `ls migrations/`; if anything has landed since IMPL-0019 was
      written, bump the reserved number to the next free slot and
      update Phase 5 + the File Changes table accordingly.
- [ ] Add migration `012_add_trace_ids.sql` (or next-free):
      `ALTER TABLE extraction_queue ADD COLUMN trace_id TEXT NULL;`
      `ALTER TABLE alerts ADD COLUMN trace_id TEXT NULL;`
      Drop+create copy in `internal/store/migrations/`.
- [ ] Update `Listing`, `Alert`, `ExtractionQueueRow` domain types +
      scan functions to carry `TraceID *string`.
- [ ] Update queue `EnqueueExtraction` and `ClaimExtraction` to
      persist + read `trace_id`.
- [ ] Add a tracer to `engine.Engine`; wrap `runIngestion` /
      `runScoring` / `runReExtraction` cron entries in spans named
      `engine.ingest`, `engine.score`, `engine.reextract`.
- [ ] Wrap inner stages with child spans:
      `ebay.browse_call`, `extract.preclassify_title`,
      `extract.preclassify_specifics`, `extract.classify`,
      `extract.extract`, `extract.normalize`, `extract.validate`,
      `score.lookup_baseline`, `score.compute_breakdown`,
      `alert.evaluate`, `notify.discord`.
- [ ] Each span sets attributes: `listing.id`, `watch.id`,
      `component.type`, `product.key`, `extraction.confidence`,
      `score.composite`. Use semconv constants where they exist;
      otherwise prefix with `spt.`.
- [ ] Bridge queue propagation: when enqueuing an extraction, capture
      the active span's trace ID and store on the queue row. When the
      worker claims a row, restore the trace context as the parent of
      the worker's root span (use a manual `trace.SpanContext`).
- [ ] Carry `traceID` from listing → alert at insert time so
      `alerts.trace_id` is populated even when the engine creates
      alerts on a different goroutine than the extraction.
- [ ] Add OTel meter for two new metrics emitted in parallel with
      Prometheus: `spt.extraction.duration` (histogram, by
      component_type), `spt.alert.eval.duration` (histogram).
      Existing Prometheus counters stay unchanged.
- [ ] Tests:
      - In-memory `tracetest.SpanRecorder` asserts span tree shape
        for one ingestion → extract → score → alert path.
      - Migration test (in `internal/store/migrate_test.go`) verifies
        `012` applies idempotently.
      - Update affected `scanListing` / `scanAlert` / queue scan
        tests for the new column.
- [ ] Run `make lint` + `make test`.

#### Success Criteria

- `make test` passes; new `tracetest` integration covers the full
  span tree.
- Migration applies cleanly on a fresh DB and on a DB pre-migrated
  through `011`.
- With `otel.enabled: true` and a real collector, the operator can
  open Clickhouse and find one trace per recently-extracted listing
  with the expected nested span structure.
- With `otel.enabled: false`, no span emission happens and existing
  metrics (`spt_extraction_tokens_total`, `spt_alerts_fired_total`)
  remain unchanged in shape and value.

---

### Phase 3: Langfuse client + LLM-call generations

Build an in-house Langfuse HTTP client (no third-party SDK — keeps
our AI/LLM dependency surface uniform with Ollama/Anthropic/OpenAI-
compat backends). Wrap `LLMBackend.Generate` via decorator so every
LLM call becomes a Langfuse generation linked to the active trace.
Async + bounded buffer protects the extract path from Langfuse
outages. Auto-score the self-reported `confidence`. No human-in-the-
loop yet.

#### Tasks

- [ ] Define `pkg/observability/langfuse/Client` interface:
      `LogGeneration(ctx, GenerationRecord) error`,
      `Score(ctx, traceID, name string, value float64, comment
      string) error`,
      `Trace(ctx, name string) (TraceHandle, error)`,
      `CreateDatasetItem(ctx, datasetID string, item Item) error`,
      `CreateDatasetRun(ctx, runRequest RunRequest) error`.
      Surface is small (~5 endpoints) and matches the Langfuse REST
      API directly.
- [ ] Implement `pkg/observability/langfuse/http_client.go`:
      authenticated HTTP client (public+secret keys via Basic auth),
      JSON request/response, retry-with-backoff on 5xx. Mirrors the
      patterns in `pkg/extract/anthropic.go`.
- [ ] Implement `pkg/observability/langfuse/noop_client.go`:
      satisfies the interface, every method returns nil. Used when
      `langfuse.enabled: false`.
- [ ] Implement async buffer:
      `pkg/observability/langfuse/buffered_client.go` wraps the HTTP
      client with a bounded channel (default capacity 1000) and a
      drain goroutine. On full buffer, drop oldest entry and
      increment drop counter. Drain goroutine exits cleanly on
      shutdown context.
- [ ] Wire 4 Prometheus metrics on the buffered client:
      `spt_langfuse_buffer_depth` (gauge),
      `spt_langfuse_buffer_drops_total` (counter),
      `spt_langfuse_writes_total{result}` (counter),
      `spt_langfuse_write_duration_seconds` (histogram).
- [ ] Add `pkg/extract/langfuse_backend.go`: decorator wrapping
      `LLMBackend` that:
      1. Reads active span context from `ctx`.
      2. Calls inner `Generate`.
      3. Pushes a `generation` to the buffered Langfuse client with
         prompt, completion, model, token usage, cost, latency,
         parent trace ID, commit SHA tag.
      4. Returns inner response unchanged.
- [ ] Construction-time wiring: `NewLLMExtractor` accepts an
      optional `langfuse.Client`. When nil (or noop), no decorator
      is applied. Config flips this on/off.
- [ ] Auto-score: after a successful extract, push a Langfuse score
      `extraction_self_confidence = attrs["confidence"]` on the
      extract trace.
- [ ] Token cost calculation: pull per-model rates from config
      (`observability.langfuse.model_costs`) so we don't hardcode
      Anthropic/Ollama pricing. Default empty → Langfuse handles its
      own cost lookup.
- [ ] Tests:
      - Mock `Client` interface; assert decorator calls
        `LogGeneration` exactly once per `Generate` with correct
        fields.
      - Verify when `Client` is nil/noop, behaviour matches today
        byte-for-byte (no extra calls, no extra latency).
      - Buffer test: fill the channel, assert drops counter
        increments and oldest entries are evicted.
      - HTTP client test against `httptest.Server` for auth header,
        retry behaviour, error mapping.
      - Table-driven test for cost calculation.
- [ ] Update `make mocks` to regenerate Langfuse client mock.
- [ ] Run `make lint` + `make test`.

#### Success Criteria

- `make test` passes; mock-Client test confirms exactly-one
  generation per Generate.
- With `langfuse.enabled: true` and a real Langfuse instance, every
  classify/extract call appears in the Langfuse UI under the parent
  trace, with prompt, response, tokens, cost, latency, commit SHA
  tag.
- With `langfuse.enabled: false`, OTel traces still emit (Phase 2
  unaffected) and the noop client is used.
- Self-confidence scores appear on extract generations in the
  Langfuse UI.
- Killing the Langfuse pod and continuing to extract for 5 minutes
  shows: extract latency unchanged; `spt_langfuse_buffer_depth`
  rises then drops to zero (entries evicted) without affecting any
  Postgres data.

---

### Phase 4: Trace deep-links + dismissal-as-score in alert review UI

Surface trace + judge data in the operator UI. Capture operator
dismissals as Langfuse scores so they become labelled training data.
Judge column added but blank until Phase 5 lights it up.

#### Tasks

- [ ] Add `GET /api/v1/alerts/{id}/trace` Huma handler. Returns
      `{ "trace_url": "https://langfuse.example/trace/<id>" }`.
      404 when Langfuse disabled or `alerts.trace_id IS NULL`.
- [ ] Templ component update: alert row gains a "View trace" button
      (only rendered when `cfg.Observability.Langfuse.Enabled`).
- [ ] Add a `judge_score` column to the alert review UI table
      (rendered when `cfg.Observability.Judge.Enabled`). Empty cell
      until Phase 5.
- [ ] Wire dismiss action: existing dismiss endpoint also calls
      `langfuse.Score(traceID, "operator_dismissed", 1.0, reason)`.
      Best-effort — failure to score doesn't fail the dismiss.
- [ ] Add an "undo dismiss" action that posts
      `operator_dismissed = 0`. Optional but cheap.
- [ ] Tests:
      - Templ render test covers both feature-flag states.
      - Handler test asserts 404 when Langfuse disabled.
      - Mock Langfuse client asserts `Score` called on dismiss.
- [ ] Add CHANGELOG-style note in `docs/OPERATIONS.md` describing the
      new UI elements.
- [ ] Run `make templ-generate`, `make lint`, `make test`.

#### Success Criteria

- `make test` + `make templ-generate` clean.
- With Langfuse enabled, clicking "View trace" on an alert opens the
  Langfuse trace in a new tab.
- Dismissing an alert produces a Langfuse score visible in the
  Langfuse UI within seconds.
- With Langfuse disabled, the UI hides both the button and the
  judge-score column; no degraded experience for users not opted in.

---

### Phase 5: Async LLM-as-judge worker

The system actually starts grading itself here. New cron job runs
every 15 minutes, finds fired alerts from the last 6 hours without
judge scores, calls the judge LLM (defaults to whatever the extract
backend is configured to use — currently Haiku 4.5), writes scores
to both Postgres `judge_scores` and Langfuse. Hard daily budget
cutoff prevents runaway spend. Operator-triggered backfill via
`spt judge run`.

#### Tasks

- [ ] **Verify migration number is still free.** Run
      `ls migrations/`; bump the reserved `013` to next free if
      needed. Update File Changes table accordingly.
- [ ] Add `pkg/judge/judge.go`:
      `Judge` interface with
      `EvaluateAlert(ctx, AlertContext) (Verdict, error)`.
      `AlertContext` carries title, condition, price, baseline
      p25/p50, score, component_type. `Verdict` is
      `{Score float64, Reason string}`.
- [ ] Implementation `pkg/judge/llm_judge.go` builds a prompt from a
      template + few-shot examples (loaded from
      `pkg/judge/examples.json` — operator-curated, ~10 each of
      `deal` / `noise` / `edge`). Hardcoded for v1; revisit
      Langfuse-fetched examples in v2.
- [ ] Judge LLM backend selection: defaults to the extract backend's
      configured model so a Haiku upgrade in `extract.anthropic`
      auto-applies. Operator can override via
      `observability.judge.backend` if they want a different model
      for judging (e.g., a stronger model for higher-quality grade).
- [ ] Cold-start: write `tools/judge-bootstrap/main.go` that pulls a
      stratified sample of recent alerts from the DB and prompts the
      operator for `deal/noise/edge` labels (~30 total), then writes
      `pkg/judge/examples.json`.
- [ ] Wire judge into scheduler: new entry in
      `internal/engine/scheduler.go::NewScheduler`, interval from
      `observability.judge.interval` (default `15m`). Skip entirely
      when `judge.enabled = false`.
- [ ] Worker: select alerts where
      `created_at > NOW() - judge.lookback` (default `6h`) AND not
      already in `judge_scores`. Track judged alerts in a new table
      `judge_scores (alert_id, score, reason, model, input_tokens,
      output_tokens, cost_usd, judged_at)` — migration
      `013_add_judge_scores.sql` (or next free).
- [ ] **Daily budget enforcement**: before each judge call, query
      `SUM(cost_usd) FROM judge_scores WHERE judged_at >= today`.
      If sum >= `observability.judge.daily_budget_usd` (default
      `10.00`), skip remaining alerts and log a slog warning. Emit
      `spt_judge_budget_exhausted_total` counter when triggered.
      Skipped alerts get caught up next day when budget resets.
- [ ] For each alert: call judge, persist to `judge_scores` for the
      UI column, push to Langfuse via
      `Client.Score(traceID, "judge_alert_quality", verdict.Score,
      verdict.Reason)`. Postgres write is the durable source; Lang-
      fuse write is best-effort (already buffered from Phase 3).
- [ ] CLI: `spt judge run --since 24h [--limit N] [--dry-run]` —
      reaches a new HTTP endpoint `POST /api/v1/judge/run` that
      triggers the worker out-of-band. Respects daily budget.
- [ ] Update Phase 4's UI judge-score column to read from
      `judge_scores` and render the score (verdict reason as
      tooltip).
- [ ] Prometheus metrics:
      `spt_judge_evaluations_total{verdict}` (counter),
      `spt_judge_score{component_type}` (histogram),
      `spt_judge_cost_usd_total{model}` (counter — for budget
      dashboards),
      `spt_judge_budget_exhausted_total` (counter).
      Dual-emit OTel counterparts.
- [ ] Tests:
      - Mock `LLMBackend` for judge; table-driven tests on
        `AlertContext → prompt`.
      - End-to-end test with mock store + mock LLM + mock Langfuse
        client: insert 3 alerts, run worker, assert all three got
        scored, persisted, and Langfuse received `Score` calls.
      - Budget enforcement test: pre-seed `judge_scores` with rows
        summing to `daily_budget_usd`, verify worker skips and emits
        counter.
      - CLI test for `spt judge run --dry-run`.
- [ ] Document the cold-start labelling workflow in
      `docs/OPERATIONS.md` — how to refresh `examples.json` when
      new ComponentTypes land. Document the budget knob and how to
      raise it.
- [ ] Run `make lint` + `make test`.

#### Success Criteria

- `make test` passes including end-to-end mock judge flow + budget
  enforcement test.
- With `judge.enabled: true`, the worker fires every 15m and
  `judge_scores` table fills with rows; alert review UI shows the
  scores within ~15 minutes of an alert firing.
- Langfuse UI shows judge scores on the corresponding traces.
- Alert review UI displays the judge score; hovering shows the
  verdict reason.
- `spt judge run --since 24h --limit 10` successfully backfills 10
  alerts on demand.
- With `judge.enabled: false`, the cron entry is not registered and
  no judge LLM calls are made.
- Daily budget cutoff verified by pre-seeding spend up to the cap
  in dev — worker correctly skips the next batch and emits
  `spt_judge_budget_exhausted_total`.
- After 7 days of running, operator can pull a Langfuse report
  comparing `judge_alert_quality` distribution to
  `operator_dismissed` rate; see if the judge tracks operator
  intuition.

---

### Phase 6: Golden dataset + operator-run regression

Avoid the IMPL-0017/0018 ship-then-discover cycle. Curate a golden
dataset of ~100 listings spanning all ComponentTypes. **No CI
workflow** — operator runs the regression script on demand
(directly, or by instructing a Claude Code session). This sidesteps
fork-PR security concerns and any chance of API key exfiltration
from CI.

#### Tasks

- [ ] Build the golden dataset:
      `tools/dataset-bootstrap/main.go` selects ~100 listings
      stratified by ComponentType + extraction confidence. Operator
      labels with correct `component_type` + `product_key`. Output:
      `testdata/golden_classifications.json`.
- [ ] Upload dataset to Langfuse as `golden_classifications:v1` via
      the in-house client's `CreateDatasetItem` (Phase 3 endpoint).
      Document the upload step in OPERATIONS.md.
- [ ] Add `tools/regression-runner/main.go`: standalone Go CLI that
      reads `testdata/golden_classifications.json`, runs each title
      through the configured backend (`--backend` flag,
      defaults to whatever `configs/config.dev.yaml` specifies),
      computes accuracy (overall and per-component-type), prints a
      per-listing diff for any mismatches. Output is structured
      (table for humans, `--json` flag for Claude-Code-friendly
      summarisation).
- [ ] New Make target: `make test-regression` — convenience wrapper
      around the above. Requires whatever credentials the chosen
      backend needs; nothing in CI.
- [ ] Add `pkg/extract/regression_test.go` with
      `//go:build regression` build tag — same logic but invokable
      via `go test -tags regression`. Mirrors the standard pattern
      for integration tests.
- [ ] Backend comparison: extend `tools/regression-runner` with
      `--backends ollama,anthropic,openai` flag — runs the dataset
      against each in turn, prints a comparison table
      (accuracy, p50 latency, $/1k extractions, error rate).
- [ ] When prompts change in a PR, the workflow becomes:
      operator runs `make test-regression` locally (or asks Claude
      Code to run it), pastes the accuracy delta into the PR
      description. Add a checkbox to `.github/PULL_REQUEST_TEMPLATE.md`
      ("Did you run `make test-regression` if `pkg/extract/`
      changed? Paste accuracy delta:").
- [ ] Add a CLAUDE.md note: when working in `pkg/extract/`,
      always run `make test-regression` before requesting review
      and paste the result.
- [ ] Push a `classify_prompt:<sha>` annotation to Langfuse on each
      regression run via `CreateDatasetRun`, so operators can
      compare runs by commit SHA in the Langfuse UI.
- [ ] Run `make lint` + `make test`.

#### Success Criteria

- `make test-regression` passes locally against the configured
  backend; output is human-readable and `--json` works for piping.
- Dataset is uploaded to Langfuse and visible in the UI under
  `golden_classifications:v1`.
- A deliberate prompt-regression (e.g., remove the workstation
  rules locally) is correctly caught by `make test-regression` with
  a visible accuracy drop printed to stdout.
- Backend comparison run produces a table that the operator can use
  to make a model-selection decision.
- PR template + CLAUDE.md note are in place so the manual step is
  not silently skipped.

---

### Phase 7: Production rollout + Grafana panels

Operationalise. Roll sampling rates up. Add Grafana panels for the
new OTel-derived data. Document the operator workflow.

#### Tasks

- [ ] Add `tools/dashgen` panels for:
      - `JudgeScoreDistribution` — histogram of `spt_judge_score`
        by component_type.
      - `JudgeVsOperatorAgreement` — overlay of
        `judge_alert_quality < 0.5` rate vs `operator_dismissed`
        rate over time.
      - `LangfuseGenerationCost` — `$` spent per backend per day
        (derived from generation cost field exported via Langfuse
        webhook or polling).
      - `TraceVolumeByPipelineStage` — span count per stage per
        minute.
- [ ] Bump `totalPanels` in `tools/dashgen/dashgen_test.go`.
- [ ] Register all new metric names in
      `tools/dashgen/config.go::KnownMetrics`.
- [ ] Run `make dashboards` to regenerate
      `deploy/grafana/data/spt-overview.json` + Prometheus rules.
- [ ] Review Collector tail-sampling effectiveness after 7 days:
      confirm the platform-side `tail_sampling` policies (kept
      alert/error/extract traces, sampled clean ingestion) are
      producing the data we need without exceeding the storage
      budget. Hand off any policy tweaks to the platform side.
- [ ] Document the new operator workflow in `docs/OPERATIONS.md`:
      - Reading judge scores in the UI.
      - Refreshing `examples.json` when adding a new ComponentType.
      - Pulling weekly judge-vs-dismiss alignment report from
        Langfuse.
      - Renewing dataset labels every quarter.
- [ ] Update CLAUDE.md with the observability config sketch + the
      "judge component is config-gated, fully optional" reminder so
      it survives compaction.
- [ ] Run `make lint`, `make test`, `make ci`.

#### Success Criteria

- `make ci` passes end-to-end.
- New Grafana panels render correctly in the running dashboard.
- After 7 days of production runtime, operator confirms:
  - Trace volume in Clickhouse is within projected storage budget.
  - Judge-score distribution is plausible (not all 1.0 or all 0.0).
  - At least one judge-flagged alert has been validated by operator
    inspection.
- Operator runbook in `docs/OPERATIONS.md` is complete enough that a
  new operator can answer "how do I find why this alert was noisy?"
  without external help.

---

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `internal/config/config.go` | Modify | Add `Observability` block: `Otel`, `Langfuse`, `Judge` sub-structs |
| `configs/config.example.yaml` | Modify | Add observability defaults (all disabled) |
| `configs/config.dev.yaml` | Modify | Same |
| `internal/observability/otel.go` | Create | OTel SDK init, no-op fallback |
| `internal/observability/otel_test.go` | Create | Init smoke tests |
| `pkg/observability/langfuse/client.go` | Create | Client interface (~5 endpoints) |
| `pkg/observability/langfuse/http_client.go` | Create | In-house HTTP impl, no third-party SDK |
| `pkg/observability/langfuse/noop_client.go` | Create | Used when langfuse.enabled=false |
| `pkg/observability/langfuse/buffered_client.go` | Create | Async + bounded buffer with 4 metrics |
| `pkg/observability/langfuse/*_test.go` | Create | Mock + httptest-based tests |
| `internal/version/version.go` | Create | `CommitSHA` ldflags-overridden |
| `cmd/server-price-tracker/main.go` | Modify | Wire `observability.Init` + shutdown |
| `migrations/012_add_trace_ids.sql` | Create | trace_id columns |
| `internal/store/migrations/012_*.sql` | Create | Embedded copy |
| `migrations/013_add_judge_scores.sql` | Create | judge_scores table |
| `internal/store/migrations/013_*.sql` | Create | Embedded copy |
| `pkg/types/types.go` | Modify | Add `TraceID *string` to `Listing`, `Alert` |
| `internal/store/postgres.go` | Modify | Update scans + queue read/write |
| `internal/engine/engine.go` | Modify | Tracer field, span wrapping in cron jobs |
| `internal/engine/scheduler.go` | Modify | Register judge cron entry (config-gated) |
| `pkg/extract/extractor.go` | Modify | Span wrapping for ClassifyAndExtract stages |
| `pkg/extract/langfuse_backend.go` | Create | Decorator over `LLMBackend` |
| `pkg/extract/langfuse_backend_test.go` | Create | Decorator tests |
| `pkg/judge/judge.go` | Create | Judge interface + AlertContext + Verdict |
| `pkg/judge/llm_judge.go` | Create | LLM-backed implementation |
| `pkg/judge/judge_test.go` | Create | Table-driven tests |
| `pkg/judge/examples.json` | Create | Cold-start few-shot examples |
| `internal/api/handlers/alert_trace.go` | Create | `GET /alerts/{id}/trace` handler |
| `internal/api/handlers/judge_run.go` | Create | `POST /judge/run` handler |
| `internal/api/web/components/alert_row.templ` | Modify | View-trace button + judge column |
| `cmd/spt/judge.go` | Create | `spt judge run` Cobra command |
| `tools/judge-bootstrap/main.go` | Create | Operator few-shot labelling CLI |
| `tools/dataset-bootstrap/main.go` | Create | Golden-dataset builder |
| `tools/regression-runner/main.go` | Create | Operator-run accuracy + backend comparison CLI |
| `testdata/golden_classifications.json` | Create | ~100 labelled listings |
| `pkg/extract/regression_test.go` | Create | `//go:build regression` accuracy gate |
| `.github/PULL_REQUEST_TEMPLATE.md` | Modify | Add "ran make test-regression?" checkbox |
| `tools/dashgen/panels/observability.go` | Create | Judge/cost/trace panels |
| `tools/dashgen/dashboards/overview.go` | Modify | Wire new panels |
| `tools/dashgen/config.go` | Modify | Register new metric names |
| `Makefile` | Modify | Add `-ldflags` commit SHA injection |
| `scripts/makefiles/go.mk` | Modify | Add `make test-regression` target |
| `docs/OPERATIONS.md` | Modify | New operator workflow sections |
| `CLAUDE.md` | Modify | Observability section + judge config flag note |

## Testing Plan

- **Unit:** every new package gets table-driven tests with mock
  Langfuse client + mock LLM backend. >85% coverage target.
- **Integration:** `internal/observability/otel_test.go` uses an
  in-memory exporter (`tracetest.SpanRecorder`) to assert span tree
  shape. Phase 5 adds an end-to-end engine test with mock store +
  mock LLM + mock Langfuse client covering one full ingest →
  judge cycle.
- **Regression (Phase 6):** `//go:build regression` accuracy gate
  against the golden dataset. CI runs on PRs touching
  `pkg/extract/`.
- **Dev validation:** mirror IMPL-0018 Phase 5 — deploy to dev,
  watch traces in Clickhouse, watch generations in Langfuse, watch
  judge worker output for ≥24 hours before promoting.
- **Smoke:** every phase finishes with `make ci` green and a manual
  verify against a real (or fixture) Langfuse + Clickhouse.

## Dependencies

- **External (assumed deployed):** Clickhouse cluster reachable on
  cluster network; Langfuse instance with public+secret keys
  provisioned via Kubernetes Secret.
- **Go modules (new):**
  - `go.opentelemetry.io/otel` + exporter `otlptracegrpc`
  - `go.opentelemetry.io/otel/sdk/trace/tracetest` (test-only)
  - **No Langfuse SDK** — in-house HTTP client lives in
    `pkg/observability/langfuse/`.
- **Existing dependencies to use:**
  - `robfig/cron/v3` for the judge worker entry.
  - `templ` for the alert review UI changes.
  - `pgx` for migration application (no new DB dependency).
- **CI dependencies:** Anthropic API key in Actions secrets (for
  regression workflow). Langfuse credentials only needed in dev/prod
  deployments; not required for unit tests.

## Open Questions

All twelve original open questions resolved during operator review
on 2026-05-03; recording the decisions here for the record.

1. **Which Langfuse Go SDK?** **Resolved: in-house HTTP client.**
   Matches the rest of the AI/LLM stack — Ollama, Anthropic, and
   OpenAI-compat backends are all in-house HTTP clients with no
   third-party SDK. Keeps the dependency surface uniform and the
   auth/retry/error patterns consistent. Langfuse REST surface for
   our needs is ~5 endpoints; maintainable in-house.

2. **Judge cadence and lookback window.** **Resolved: `15m / 6h`.**
   Sub-5-minute freshness on a quality-grade column isn't worth
   the cron noise; the 6h lookback gives margin if the worker
   misses a tick. (Original framing assumed ~30–50 alerts/day —
   actual volume is much higher, which is the whole reason this
   design exists. `15m / 6h` is defensible at any volume.)

3. **Per-alert judge cost ceiling.** **Resolved: hard cutoff at
   `observability.judge.daily_budget_usd: 10.00`.** At Haiku 4.5
   rates (~$1/M input, ~$5/M output) that's roughly 10k judges/day
   — generous for normal operation, protective against runaway.
   Skipped alerts get judged tomorrow when budget resets. Judge
   LLM defaults to whatever the extract backend is configured to
   use, so any model upgrade auto-applies.

4. **What to do when Langfuse is unreachable mid-extract.**
   **Resolved: option (d) async + bounded buffer.** Decorator
   pushes generations onto a channel (default capacity 1000); a
   drain goroutine ships to Langfuse with retry. Listing data is
   never at risk — only Langfuse-side observability degrades during
   outages. Four Prometheus metrics expose buffer health:
   `spt_langfuse_buffer_depth`,
   `spt_langfuse_buffer_drops_total`,
   `spt_langfuse_writes_total{result}`,
   `spt_langfuse_write_duration_seconds`.

5. **Trace sampling strategy: head vs tail.** **Resolved: tail
   sampling at the Collector from day one**, treated as a
   Clickhouse/Langfuse deployment requirement. App emits 100% of
   spans (`AlwaysSample`); Collector applies `tail_sampling`
   processor with policies (keep all alert traces, all error
   traces, all extract spans, sample N% of clean ingestion). Cross-
   team handoff documented in Phase 1 OPERATIONS.md task.

6. **`judge_scores` in app DB or Langfuse only?** **Resolved:
   both.** Postgres `judge_scores` table is the durable source for
   the alert review UI (page loads stay fast, UI keeps rendering
   when Langfuse is unreachable). Langfuse write is best-effort
   (already buffered per Q4). Schema cost is trivial — one small
   table.

7. **Few-shot examples in judge prompt: hardcoded or fetched?**
   **Resolved: hardcoded `pkg/judge/examples.json` for v1.**
   Examples change rarely (probably once per quarter). Code review
   on label changes is actually valuable. Promote to Langfuse-
   fetched in v2 if iteration speed becomes the bottleneck.

8. **Operator-dismissal score schema: binary or categorised?**
   **Resolved: binary for v1.** With high alert volume, every
   extra click in the dismiss flow compounds operator fatigue.
   Phase 6 prompt iteration can still derive value from binary
   labels. Schema is forward-compatible — Langfuse score has a
   `comment` field we can repurpose later for free-text reasons
   without a migration.

9. **CI regression workflow cost + fork-PR security wrinkle.**
   **Resolved: no CI workflow at all.** Operator runs
   `make test-regression` on demand (or instructs a Claude Code
   session to). Sidesteps fork-PR security entirely; also avoids
   any risk of accidental API key commits to CI configs. Safety
   net is a PR template checkbox + CLAUDE.md reminder so it isn't
   silently skipped when working in `pkg/extract/`.

10. **Backwards compat for `LLMBackend.Generate`?** **Resolved:
    not needed now, kept as tracking marker.** Decorator pattern
    is sufficient for everything in this design — `ctx
    context.Context` already carries the active OTel span, so the
    decorator extracts the trace ID without a signature change.
    A `traceCtx` parameter would only matter for streaming
    responses or backend-initiated child generations, neither of
    which we do today. Documented here so a future contributor
    knows we considered it and explicitly chose not to.

11. **Migration ordering relative to in-flight work.** **Resolved:
    defensive policy.** First task of Phase 2 and first task of
    Phase 5 is to `ls migrations/` and bump the reserved number
    (`012`, `013`) to the next free slot if anything has landed in
    the meantime. Same proven check used during IMPL-0017 and
    IMPL-0018.

12. **Phase ordering — Phase 6 (datasets) before Phase 5 (judge)?**
    **Resolved: keep current order.** Judge ships value on its own
    (UI score column, Langfuse alert-quality scores). Cold-start
    with ~30 examples in `examples.json` is enough to make the
    judge useful immediately. The full ~100-listing golden dataset
    (Phase 6) deepens the prompt iteration loop later without
    blocking judge delivery. Operator labelling time for the
    smaller examples set is ~15 min; for the full dataset ~50 min
    — keeping the larger ask off the critical path is the right
    call.

## References

- DESIGN-0016 — OpenTelemetry, Clickhouse, and Langfuse
  instrumentation (this implementation's parent design)
- DESIGN-0007 — LLM Token Metrics (the Prometheus telemetry this
  layers on top of)
- DESIGN-0010 — Alert review UI (the UI surface Phase 4 extends)
- DESIGN-0011 — Reduce alert noise via scoring + accessory
  pre-classifier (the recurring problem this implementation
  addresses)
- IMPL-0017 — GPU ComponentType (8-touchpoint pattern reference)
- IMPL-0018 — Workstation/desktop ComponentType (iterative-fix
  pattern reference)
- OpenTelemetry Go SDK: <https://opentelemetry.io/docs/languages/go/>
- OTel Collector Clickhouse exporter:
  <https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/clickhouseexporter>
- Langfuse self-host: <https://langfuse.com/docs/deployment/self-host>
- Langfuse LLM-as-judge:
  <https://langfuse.com/docs/scores/model-based-evaluations>
- Langfuse datasets + evals:
  <https://langfuse.com/docs/datasets/overview>
