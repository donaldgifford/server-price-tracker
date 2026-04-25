---
id: DESIGN-0007
title: "LLM Token Metrics"
status: Implemented
author: Donald Gifford
created: 2026-04-25
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0007: LLM Token Metrics

**Status:** Implemented
**Author:** Donald Gifford
**Date:** 2026-04-25

<!--toc:start-->
- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [New Prometheus Metrics](#new-prometheus-metrics)
  - [Cardinality](#cardinality)
  - [Wiring Point](#wiring-point)
  - [Per-Backend Token Plumbing](#per-backend-token-plumbing)
- [API / Interface Changes](#api--interface-changes)
  - [Public Surface](#public-surface)
  - [Configuration](#configuration)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Example PromQL](#example-promql)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Emit Prometheus metrics for LLM token consumption labeled by backend and model so
deployments can compare token volume across Ollama, Anthropic, and the OpenAI-compatible
backend in Grafana. Most of the plumbing is already in place — the `GenerateResponse`
struct already carries a `TokenUsage` field — but the values are discarded today and
the Ollama backend doesn't even populate them. This design wires the existing data
through a new metric and fixes Ollama's omission.

## Goals and Non-Goals

### Goals

- Make LLM input/output token volume visible in Prometheus, labeled by backend and
  active model, so a Grafana dashboard can plot Ollama vs cloud spend after a backend
  swap.
- Fix Ollama so its token counts are non-zero (currently it doesn't parse the response
  fields).
- Stay backend-agnostic: the metric, the wiring, and the label scheme work identically
  for all three current backends and any future one.
- Keep label cardinality bounded and well below the levels that would cause Prometheus
  pain.

### Non-Goals

- Computing dollar costs in application code. Dollars are derived in Grafana/PromQL
  using a per-dashboard price table — see ADR-0001 / DESIGN-0006 for the reasoning.
- Per-request, per-listing, or per-user attribution. The metric is aggregate.
- Anthropic prompt-cache token tracking (`cache_read_input_tokens`,
  `cache_creation_input_tokens`). Caching does not apply at current prompt sizes
  (see DESIGN-0006); revisit when it does.
- Anything related to the deferred batch implementation in DESIGN-0006. This work is
  the prerequisite, not part of it.
- Replacing or restructuring the existing `extraction_duration_seconds` /
  `extraction_failures_total` metrics. Both stay as-is.

## Background

Current state of LLM observability in the codebase (verified by reading the source on
this branch):

- `internal/metrics/metrics.go` registers two extraction-related metrics:
  `extraction_duration_seconds` (histogram, no labels) and `extraction_failures_total`
  (counter, no labels). Neither is broken down by backend or model.
- `pkg/extract/backend.go` defines `TokenUsage{PromptTokens, CompletionTokens,
  TotalTokens}` on `GenerateResponse`. The `LLMBackend` interface already exposes
  `Name() string` and the response carries the active `Model`.
- `pkg/extract/anthropic.go` (lines ~202–210) populates `Usage` correctly from
  `usage.input_tokens` and `usage.output_tokens`.
- `pkg/extract/openai_compat.go` populates `Usage` correctly from the OpenAI-format
  `usage` block.
- `pkg/extract/ollama.go` (lines ~62–65, ~131–134) **does not** parse Ollama's
  `prompt_eval_count` / `eval_count` fields — its `ollamaResponse` struct ignores them
  and the returned `GenerateResponse` has `Usage{0, 0, 0}`.
- `pkg/extract/extractor.go` discards `resp.Usage` entirely in both `Classify` (line
  ~78) and `Extract` (line ~111). Nothing else reads it either.

Net effect: token data is parsed by 2 of 3 backends, never read by anyone, and never
exported. Switching from Ollama to Anthropic produces no visible signal in Grafana.

## Detailed Design

### New Prometheus Metrics

Two metrics, both registered in `internal/metrics/metrics.go` next to the existing
`Extraction*` block:

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

Label values:

- `backend` — `ollama` | `anthropic` | `openai_compat` (returned by `LLMBackend.Name()`)
- `model` — the value `GenerateResponse.Model` carries (e.g.,
  `claude-haiku-4-5-20251001`, `mistral:7b-instruct-v0.3-q5_K_M`, etc.)
- `direction` — `input` | `output`

The histogram is included because per-request distribution matters for sizing and
catching prompt regressions. If it turns out unused, dropping a metric later is cheap.

### Cardinality

Worst plausible cardinality:

- 3 backends × ~5 distinct models in any deployment × 2 directions = ~30 counter series
- 3 backends × ~5 models = ~15 histogram series, each with ~10 buckets = ~150 series

Total: well under 200 active series. Negligible.

In practice a single deployment runs exactly one backend at a time and rarely flips
between models, so steady-state is closer to 4–6 active series.

### Wiring Point

Emit from the extractor, not the backends. Two reasons:

- One place to wire instead of three; backends stay focused on the protocol.
- Backend tests already exist with their own response fixtures — adding metric
  assertions to each would be noise.

Concretely, wrap each `b.backend.Generate(ctx, …)` call in `Classify` and `Extract`:

```go
resp, err := e.backend.Generate(ctx, req)
if err == nil {
    backend := e.backend.Name()
    metrics.ExtractionTokensTotal.WithLabelValues(backend, resp.Model, "input").Add(float64(resp.Usage.PromptTokens))
    metrics.ExtractionTokensTotal.WithLabelValues(backend, resp.Model, "output").Add(float64(resp.Usage.CompletionTokens))
    metrics.ExtractionTokensPerRequest.WithLabelValues(backend, resp.Model).Observe(float64(resp.Usage.TotalTokens))
}
```

Errors don't increment token counters — failed calls don't have meaningful usage.
`extraction_failures_total` already covers failures.

### Per-Backend Token Plumbing

| Backend | Status | Action needed |
|---|---|---|
| Anthropic | Already populates `Usage` from `usage.input_tokens` / `usage.output_tokens`. | None. |
| OpenAI-compat | Already populates `Usage` from the OpenAI `usage` block. | None. |
| Ollama | Does not parse `prompt_eval_count` / `eval_count`; returns zero `Usage`. | Add fields to `ollamaResponse`, populate `Usage` on return. |

Ollama fix:

```go
type ollamaResponse struct {
    Model           string `json:"model"`
    Response        string `json:"response"`
    PromptEvalCount int    `json:"prompt_eval_count"`
    EvalCount       int    `json:"eval_count"`
}

return GenerateResponse{
    Content: ollamaResp.Response,
    Model:   ollamaResp.Model,
    Usage: TokenUsage{
        PromptTokens:     ollamaResp.PromptEvalCount,
        CompletionTokens: ollamaResp.EvalCount,
        TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
    },
}, nil
```

These fields are documented in the Ollama API and are present on every non-streaming
response we make today (we already pass `stream: false`).

## API / Interface Changes

### Public Surface

No interface changes. `LLMBackend.Generate` and `Extractor.Classify` /
`Extractor.Extract` keep their current signatures. `TokenUsage` already exists on
`GenerateResponse` and is now actually consumed.

The two new exported variables `metrics.ExtractionTokensTotal` and
`metrics.ExtractionTokensPerRequest` are additive — nothing else changes in the
metrics package.

### Configuration

None. Metric emission is unconditional whenever an LLM call succeeds.

## Data Model

No schema changes. No migrations.

## Testing Strategy

- **Unit test, Ollama backend**: extend the existing `httptest`-based test in
  `pkg/extract/ollama_test.go` with a fixture response that includes
  `prompt_eval_count: 250` and `eval_count: 30`. Assert
  `resp.Usage.PromptTokens == 250` and `resp.Usage.CompletionTokens == 30`.
- **Unit test, extractor metrics**: in `pkg/extract/extractor_test.go`, use a
  `MockLLMBackend` that returns a known `TokenUsage`. After calling `Classify` and
  `Extract`, read the counter values via the `prometheus/testutil.ToFloat64` helper
  to confirm they incremented by the expected amount with the expected labels.
- **No metric assertions in Anthropic / OpenAI-compat tests** — those backends already
  have unit tests that check the `Usage` mapping, which is sufficient since the
  extractor-level test covers the increment path independent of backend.
- **No new integration tests.** Existing extraction integration tests (gated by
  `//go:build integration`) will start emitting metrics naturally; no new behavior
  to verify in integration that isn't already covered by unit-level fixtures.

## Migration / Rollout Plan

1. Land the metric registrations and the wiring change. Additive — no behavior change
   for callers.
2. Land the Ollama parser fix. Additive — Ollama's `Generate` keeps returning the same
   `Content` / `Model`; only `Usage` changes from zero to actual values.
3. Deploy. Verify in `/metrics`:
   - `spt_extraction_tokens_total{backend="ollama",model="…",direction="input"}` is
     non-zero shortly after the next ingestion cycle.
   - `spt_extraction_tokens_per_request_count{backend="ollama"}` increments.
4. Add a Grafana panel: rate of input + output tokens, stacked by `backend`. This is
   the visualization the user wants for the Anthropic swap test.
5. **Anthropic swap, post-rollout**: flip `config.llm.backend: anthropic` in a test
   deployment, kick a re-extraction pass, watch the same panel show Anthropic series
   appear. Multiply by per-model price in PromQL to land at dollar estimates.

Rollback: revert the commits. No external state to clean up. Existing dashboards keep
working.

## Example PromQL

Once shipped, useful queries for the Grafana panel:

```promql
# Tokens per second by backend, stacked
sum by (backend) (rate(spt_extraction_tokens_total[5m]))

# Input vs output split
sum by (backend, direction) (rate(spt_extraction_tokens_total[5m]))

# Estimated $/hour, Anthropic Haiku 4.5 pricing inlined
(
  sum(rate(spt_extraction_tokens_total{backend="anthropic",direction="input"}[5m])) * 1.0  +
  sum(rate(spt_extraction_tokens_total{backend="anthropic",direction="output"}[5m])) * 5.0
) * 3600 / 1e6

# p95 prompt size per backend
histogram_quantile(0.95,
  sum by (le, backend) (rate(spt_extraction_tokens_per_request_bucket[5m]))
)
```

## Open Questions

- **Histogram buckets.** The proposed buckets (50…20000) cover today's prompt sizes
  (~250–2,900 tokens) plus headroom. If we ever inline few-shot examples or large
  reference docs the upper bucket might need to extend. Cheap to revisit; not blocking.
- **Anthropic cache token capture.** When prompt caching becomes applicable
  (preconditions in DESIGN-0006), we'll want to add `cache_read` and `cache_creation`
  values for `direction`, since cached reads are 10× cheaper than fresh input. Out of
  scope until caching is in play.

## References

- DESIGN-0006: Anthropic Batch Extraction (Q3 of its open questions surfaced this work)
- ADR-0001: Anthropic Batch API for Extraction (Deferred — explains why telemetry
  matters before revisiting batch)
- DESIGN-0002: LLM Extraction Pipeline
- `internal/metrics/metrics.go` — existing Prometheus registry
- `pkg/extract/backend.go` — `TokenUsage` struct
- Ollama API generate-endpoint response fields: `prompt_eval_count`, `eval_count`
  (see `github.com/ollama/ollama` API docs)
