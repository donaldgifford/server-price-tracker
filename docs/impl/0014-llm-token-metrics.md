---
id: IMPL-0014
title: "LLM Token Metrics"
status: Draft
author: Donald Gifford
created: 2026-04-25
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0014: LLM Token Metrics

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-04-25

<!--toc:start-->
- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Backend Token Usage Population](#phase-1-backend-token-usage-population)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: Register Prometheus Metrics](#phase-2-register-prometheus-metrics)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Wire Token Telemetry into the Extractor](#phase-3-wire-token-telemetry-into-the-extractor)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
  - [Phase 4: Manual Validation and Doc Closeout](#phase-4-manual-validation-and-doc-closeout)
    - [Tasks](#tasks-3)
    - [Success Criteria](#success-criteria-3)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [Follow-up Work](#follow-up-work)
- [References](#references)
<!--toc:end-->

## Objective

Implement the LLM token telemetry described in DESIGN-0007: emit per-backend,
per-model token counters from the extractor so a Grafana dashboard can compare
LLM token volume across Ollama, Anthropic, and OpenAI-compat backends, and fix
the Ollama backend to actually populate its token usage (today it returns
zeroes).

**Implements:** DESIGN-0007

## Scope

### In Scope

- Ollama backend: parse `prompt_eval_count` and `eval_count` from the API response
  and populate `GenerateResponse.Usage`.
- Two new Prometheus metrics in `internal/metrics/metrics.go`:
  `extraction_tokens_total` (counter vec) and `extraction_tokens_per_request`
  (histogram vec), both labeled `{backend, model}` (and `direction` on the counter).
- Wire metric emission into `pkg/extract/extractor.go`'s `Classify` and `Extract`
  methods after each successful `Generate` call.
- Unit tests for the Ollama parsing fix and for the extractor-level metric emission.
- Status update on DESIGN-0007 to `Implemented`.

### Out of Scope

- Anthropic prompt-cache token capture (`cache_read_input_tokens`,
  `cache_creation_input_tokens`). Out of scope per DESIGN-0007 — caching does not
  apply at current prompt sizes.
- Dollar-cost computation in app code. Dollars are derived in PromQL.
- Anything related to the deferred batch path in DESIGN-0006 / ADR-0001.
- Restructuring or relabeling the existing `extraction_duration_seconds` /
  `extraction_failures_total` metrics. Both stay as-is.
- Building or modifying Grafana dashboards. Validation only confirms the metrics
  appear at `/metrics`; dashboard work happens separately (likely under the
  existing `tools/dashgen/` flow).
- Adding metrics for the `failed call` path. `extraction_failures_total` already
  covers failures.

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all tasks are
checked off and its success criteria are met. Phases 1 and 2 are independent and
could run in parallel; phase 3 depends on phase 2; phase 4 depends on all of 1–3.

---

### Phase 1: Backend Token Usage Population

Establish a uniform contract across all three backends: every successful
`Generate` returns a `GenerateResponse` whose `Usage` reflects the API
response's token counts. Ollama needs a real parser fix; Anthropic and
OpenAI-compat already work but get assertion-only updates so the contract is
explicit at the test level. All three changes are self-contained from the
metrics work and make sense to ship together.

All tests use existing `httptest.NewServer` fixtures (verified — see e.g.
`pkg/extract/anthropic_test.go:27` and `pkg/extract/openai_compat_test.go:28`).
**No real API keys required to run any of these tests.**

#### Tasks

**Ollama (parser fix):**

- [x] In `pkg/extract/ollama.go`, extend `ollamaResponse` struct to include:
  - `PromptEvalCount int  \`json:"prompt_eval_count"\``
  - `EvalCount       int  \`json:"eval_count"\``
- [x] In `pkg/extract/ollama.go`, populate `Usage` in the returned
      `GenerateResponse`:
  - `PromptTokens:     ollamaResp.PromptEvalCount`
  - `CompletionTokens: ollamaResp.EvalCount`
  - `TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount`
- [x] In `pkg/extract/ollama_test.go`, update one or more existing fixtures to
      include `"prompt_eval_count": 250, "eval_count": 30` (or similar
      plausible values) in the mock JSON body.
- [x] Add at least one explicit assertion that
      `resp.Usage.PromptTokens == <fixture value>` and
      `resp.Usage.CompletionTokens == <fixture value>`.

**Anthropic (parity assertion):**

- [x] In `pkg/extract/anthropic_test.go`, locate the existing successful-call
      test that uses the fixture with
      `"usage": {"input_tokens": 10, "output_tokens": 1}` (around line 27) and
      add assertions on the parsed `GenerateResponse`:
  - `assert.Equal(t, 10, resp.Usage.PromptTokens)`
  - `assert.Equal(t, 1, resp.Usage.CompletionTokens)`
  - `assert.Equal(t, 11, resp.Usage.TotalTokens)`
- [x] No fixture changes; no new test cases.

**OpenAI-compat (parity assertion):**

- [ ] In `pkg/extract/openai_compat_test.go`, locate the existing
      successful-call test that uses the fixture with
      `"usage": {"prompt_tokens": 10, "completion_tokens": 1, "total_tokens": 11}`
      (around line 28) and add the equivalent assertions on the parsed
      `GenerateResponse.Usage`.
- [ ] No fixture changes; no new test cases.

**Verification:**

- [ ] Verify `make test ./pkg/extract/...` passes.
- [ ] Verify `make lint` passes (no new warnings on the modified files).

#### Success Criteria

- `ollamaResponse` struct exposes `PromptEvalCount` and `EvalCount`, and
  `OllamaBackend.Generate` returns a `GenerateResponse` whose `Usage` reflects
  the response body's token counts.
- All three backend unit test files contain at least one explicit assertion on
  `resp.Usage.PromptTokens` and `resp.Usage.CompletionTokens` for a successful
  `Generate` call.
- Existing Ollama / Anthropic / OpenAI-compat tests that did not previously
  assert on `Usage` continue to pass unchanged.
- No new dependencies, no real API keys required to run any test.

---

### Phase 2: Register Prometheus Metrics

Define the new metric variables next to the existing `Extraction*` metrics in
the metrics package. They are inert until phase 3 wires emission, but registering
them first means the rest of the codebase compiles even before emission lands.

#### Tasks

- [ ] In `internal/metrics/metrics.go`, in the `// Extraction metrics.` block (or
      a new `// LLM token metrics.` block immediately after it), add:

  ```go
  ExtractionTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
      Namespace: namespace,
      Name:      "extraction_tokens_total",
      Help:      "Total LLM tokens consumed by extraction, broken down by backend, model, and direction.",
  }, []string{"backend", "model", "direction"})

  ExtractionTokensPerRequest = promauto.NewHistogramVec(prometheus.HistogramOpts{
      Namespace: namespace,
      Name:      "extraction_tokens_per_request",
      Help:      "Distribution of total tokens (input+output) per LLM call, by backend and model.",
      Buckets:   []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000, 20000},
  }, []string{"backend", "model"})
  ```

- [ ] Verify `go build ./...` succeeds.
- [ ] Verify `make lint` passes.
- [ ] Confirm the metrics appear in `/metrics` (with no series until they are
      written to) by running the server briefly and grepping the endpoint —
      `curl -s localhost:8080/metrics | grep spt_extraction_tokens` should print
      the `# HELP` and `# TYPE` lines.

#### Success Criteria

- Both `ExtractionTokensTotal` and `ExtractionTokensPerRequest` are exported from
  the `metrics` package and registered with the global Prometheus registry via
  `promauto`.
- `/metrics` exposes `# HELP` and `# TYPE` lines for `spt_extraction_tokens_total`
  and `spt_extraction_tokens_per_request` even with zero observations.
- Build and lint pass.

---

### Phase 3: Wire Token Telemetry into the Extractor

Emit metrics from `LLMExtractor.Classify` and `LLMExtractor.Extract` after each
successful backend `Generate` call. Add unit tests that verify increments using
`prometheus/testutil`.

#### Tasks

- [ ] In `pkg/extract/extractor.go`, **cache the backend name at construction
      time** so we don't call `Name()` on every `Generate`:
  - [ ] Add `backendName string` field to `LLMExtractor`.
  - [ ] In `NewLLMExtractor`, set `e.backendName = backend.Name()` once.
- [ ] In `pkg/extract/extractor.go`, immediately after each successful
      `e.backend.Generate(...)` call (one in `Classify`, one in `Extract`),
      emit:

  ```go
  metrics.ExtractionTokensTotal.WithLabelValues(e.backendName, resp.Model, "input").Add(float64(resp.Usage.PromptTokens))
  metrics.ExtractionTokensTotal.WithLabelValues(e.backendName, resp.Model, "output").Add(float64(resp.Usage.CompletionTokens))
  metrics.ExtractionTokensPerRequest.WithLabelValues(e.backendName, resp.Model).Observe(float64(resp.Usage.TotalTokens))
  ```

  Place the emission *before* the JSON parse / validation steps so that even
  when the response payload is unusable, we still record the tokens we paid for
  (decision recorded in resolved open question #2 — spend-tracking semantics).
- [ ] Failures (i.e., `e.backend.Generate` returned a non-nil error) must NOT
      emit token metrics — `extraction_failures_total` already covers that case.
- [ ] **Update every existing test in `pkg/extract/extractor_test.go`** that
      constructs an `LLMExtractor` from a `MockLLMBackend` to add a single
      `m.EXPECT().Name().Return("...").Once()` expectation. (Mockery's strict
      mode panics on unexpected calls, and `NewLLMExtractor` now invokes
      `Name()` once at construction.)
- [ ] Add new metric-assertion test cases (extend the existing table tests or
      add a dedicated `TestLLMExtractor_TokenMetrics` test):
  - [ ] Use **unique label values per test case** — e.g.,
        `backend := "test-" + t.Name()` and
        `model := "model-" + t.Name()` — so each test reads its own corner of
        the metric vec without colliding with other parallel tests.
  - [ ] Configure
        `m.EXPECT().Name().Return(backend).Once()` and
        `m.EXPECT().Generate(...)` returning a `GenerateResponse` with
        `Model: model` and a known
        `Usage{PromptTokens: 250, CompletionTokens: 5, TotalTokens: 255}`.
  - [ ] After invoking the method under test, assert via
        `testutil.ToFloat64(metrics.ExtractionTokensTotal.WithLabelValues(backend, model, "input"))`
        that it equals the expected value (e.g., 250 after one call).
  - [ ] **Do NOT call `Reset()` and DO keep `t.Parallel()`** — per-test unique
        labels avoid the cross-test pollution that `Reset()` was meant to
        guard against.
- [ ] Verify `make test ./pkg/extract/...` passes (including all existing test
      cases now updated with `Name()` expectations).
- [ ] Verify `make lint` passes.

#### Success Criteria

- A successful `Classify` call increments `extraction_tokens_total` by the
  response's `PromptTokens` (direction=input) and `CompletionTokens`
  (direction=output) for the active `{backend, model}` labels.
- A successful `Extract` call increments the same metrics likewise.
- A failing `Generate` call (returns error) increments neither token metric.
- `extraction_tokens_per_request` records one observation per successful
  `Generate` call with the total token count.
- New extractor unit tests use `testutil.ToFloat64` and reset metric state
  between cases; existing tests continue to pass without modification.

---

### Phase 4: Manual Validation and Doc Closeout

End-to-end check that the metrics actually work in a running deployment, then
close the design.

#### Tasks

- [ ] Run the service locally against the Ollama backend (`make dev-setup` then
      `make run`).
- [ ] Trigger at least one extraction (either via `/api/v1/ingest`,
      `/api/v1/reextract`, or `/api/v1/extract` for a one-off).
- [ ] Confirm `/metrics` exposes non-zero values for:
  - `spt_extraction_tokens_total{backend="ollama",model="<configured>",direction="input"}`
  - `spt_extraction_tokens_total{backend="ollama",model="<configured>",direction="output"}`
  - `spt_extraction_tokens_per_request_count{backend="ollama",model="<configured>"}`
- [ ] (Optional, if an Anthropic key is at hand) Switch
      `config.llm.backend: anthropic` in a dev config, restart, run an extraction,
      confirm the same series appear with `backend="anthropic"`.
- [ ] Update `docs/design/0007-llm-token-metrics.md` status from `Draft` to
      `Implemented` (frontmatter and body). Run `docz update design`.
- [ ] Update this IMPL's status from `Draft` to `Completed`. Run `docz update impl`.
- [ ] `make lint-md` passes.

#### Success Criteria

- After at least one ingestion + extraction cycle, the three series above are
  present and non-zero in `/metrics`.
- DESIGN-0007 status is `Implemented`.
- IMPL-0014 status is `Completed`.

---

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `pkg/extract/ollama.go` | Modify | Add `PromptEvalCount` / `EvalCount` to `ollamaResponse`; populate `Usage` |
| `pkg/extract/ollama_test.go` | Modify | Fixture and assertions for new `Usage` fields |
| `pkg/extract/anthropic_test.go` | Modify | Add `Usage` parity assertions on existing fixture |
| `pkg/extract/openai_compat_test.go` | Modify | Add `Usage` parity assertions on existing fixture |
| `internal/metrics/metrics.go` | Modify | Register `ExtractionTokensTotal` + `ExtractionTokensPerRequest` |
| `pkg/extract/extractor.go` | Modify | Emit token metrics after each successful `Generate` |
| `pkg/extract/extractor_test.go` | Modify | Verify metric increments using `testutil.ToFloat64` |
| `docs/design/0007-llm-token-metrics.md` | Modify | Status: `Draft` → `Implemented` |
| `docs/impl/0014-llm-token-metrics.md` | Modify | Status: `Draft` → `Completed` |

## Testing Plan

- [ ] Unit tests pass: `make test ./pkg/extract/...` and
      `make test ./internal/metrics/...`.
- [ ] Full unit suite passes: `make test`.
- [ ] Lint clean: `make lint`.
- [ ] Markdown lint clean: `make lint-md`.
- [ ] Mocks regenerated only if needed (`LLMBackend` interface unchanged, so
      `make mocks` is **not** required).
- [ ] Manual validation per Phase 4: `/metrics` shows non-zero token series after
      a real extraction.
- [ ] No regression in `extraction_duration_seconds` /
      `extraction_failures_total` (existing dashboards keep working).

## Dependencies

- No new Go dependencies. `prometheus/client_golang` and its `testutil` subpackage
  are already imported by the codebase.
- No schema migrations.
- No Helm chart changes.
- Ollama parser fix has no operational dependency — the
  `prompt_eval_count` / `eval_count` fields have been part of Ollama's
  non-streaming `/api/generate` response for years and the codebase already
  passes `stream: false`.

## Open Questions

1. ~~**Histogram bucket boundaries.**~~ **Resolved: keep top bucket at 20000.**
   Current and near-future workload tops out around 3,500 tokens per request
   (output capped at 512 by `WithMaxTokens` default; worst-case combined input
   ~2,900). The `+Inf` bucket already catches outliers; if it starts counting
   non-trivially, that's the trigger to extend.

2. ~~**Where to place the metric emission relative to JSON parse.**~~
   **Resolved: emit *before* parse.** This metric exists for spend tracking;
   we record the tokens we paid for, including ones the model returned in
   unparseable form. The metric `Help` text reflects this so future readers
   know it's "tokens billed," not "tokens that produced useful extractions."

3. ~~**Test isolation under parallelism.**~~ **Resolved: per-test unique labels +
   cache `backendName` at construction.** Phase 3 was originally going to
   `Reset()` the metric vec between cases, which would have collided with
   `t.Parallel()`. Reframed: each metric-assertion test uses unique label
   values derived from `t.Name()` (e.g., `backend := "test-" + t.Name()`,
   `model := "model-" + t.Name()`), so no two parallel tests share a label
   space. `Reset()` is unnecessary and `t.Parallel()` is preserved.
   Additionally, `LLMExtractor` now caches `backend.Name()` at construction
   so the hot path doesn't call `Name()` on every `Generate`, and existing
   tests only need to add a single `m.EXPECT().Name().Once()` per
   `NewLLMExtractor` call rather than one per request.

4. ~~**OpenAI-compat verification.**~~ **Resolved: add parity assertions for
   both Anthropic and OpenAI-compat in Phase 1.** Confirmed both test files
   already use `httptest.NewServer` with inline fixtures containing `usage`
   blocks, so no real API keys are needed. Phase 1 was renamed "Backend Token
   Usage Population" to cover all three backends — Ollama gets the parser fix,
   Anthropic and OpenAI-compat get assertion-only updates that lock in the
   `Usage`-is-populated contract at the test level.

5. ~~**Failure-by-backend metric.**~~ **Resolved: defer to its own DESIGN +
   IMPL.** Today's `extraction_failures_total` is a single unlabeled counter.
   Adding `{backend, model, reason}` labels needs a real design pass to
   enumerate the `reason` taxonomy (transport error, rate-limit, empty
   response, malformed JSON, schema validation, component-specific validation)
   — that is a design conversation, not just "add labels." The user also
   flagged this as a likely starting point for a broader metrics-package
   refactor. Tracked under [Follow-up Work](#follow-up-work).

6. ~~**Grafana dashboard work.**~~ **Resolved: defer to the same DESIGN+IMPL
   as the failure-by-backend metric (#5).** The token panel is small but it's
   a different kind of work — visualization, not instrumentation. Sequencing
   matters too: ship the metrics first, let real data populate, then design
   the panel against actual shape rather than speculation. Bundling with the
   metrics-refactor work avoids having two tiny dashboard PRs and groups the
   dashboard pass with the broader effort. Tracked under
   [Follow-up Work](#follow-up-work).

## Follow-up Work

These items were considered during open-question review and deferred so this
IMPL stays scoped to "make tokens visible per backend." They are not part of
this implementation; tracked here so we don't lose them.

- **Metrics refactor (DESIGN + IMPL).** Likely starting point for a broader
  metrics-package pass. Scope expected to include:
  - Replace or extend `extraction_failures_total` with
    `{backend, model, reason}` labels. Needs the `reason` taxonomy designed
    first (transport error, rate-limit, empty response, malformed JSON,
    schema validation failure, component-specific validation failure), and a
    decision on deprecating the old unlabeled counter vs adding a parallel
    one.
  - Add `{backend, model}` labels to `extraction_duration_seconds` (cheap
    once the precedent in this IMPL is in place).
  - Add the headline Grafana panel —
    `rate(spt_extraction_tokens_total[5m])` stacked by backend, plus
    derived dollar-cost panels using PromQL price tables — to the
    dashboards generated by `tools/dashgen/` (see IMPL-0009). Building the
    panel after the metrics have run for a few cycles means designing it
    against real data rather than speculation.
- **Anthropic prompt-cache token capture.** When prompt caching becomes
  applicable (preconditions in DESIGN-0006), add `cache_read` and
  `cache_creation` values for the `direction` label so cached reads (10×
  cheaper) are visible separately from fresh input. Independent of the
  metrics refactor above.

## References

- [DESIGN-0007: LLM Token Metrics](../design/0007-llm-token-metrics.md)
- [DESIGN-0006: Anthropic Batch Extraction](../design/0006-anthropic-batch-extraction.md)
  — this IMPL is the prerequisite for ever revisiting that deferred design.
- [ADR-0001: Anthropic Batch API for Extraction](../adr/0001-anthropic-batch-api-for-extraction.md)
- [IMPL-0008: Metrics Refactor](0008-metrics-refactor.md) — prior work on the
  metrics package; useful pattern reference.
- [IMPL-0009: Dashgen Tool / Grafana Dashboards](0009-dashgen-tool-grafana-dashboards.md)
  — relevant if dashboard panels become part of the rollout.
- `internal/metrics/metrics.go` — existing Prometheus registry.
- `pkg/extract/backend.go` — `LLMBackend.Name()` and `TokenUsage` struct.
- `pkg/extract/extractor.go` — wiring point for emission (lines ~78 and ~111
  on `main` at time of writing).
- `pkg/extract/ollama.go` — `ollamaResponse` struct (lines ~62–65) needing the
  parser fix.
