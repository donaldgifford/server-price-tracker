---
id: DESIGN-0016
title: "OpenTelemetry, Clickhouse, and Langfuse instrumentation for alert quality"
status: Implemented
author: Donald Gifford
created: 2026-05-03
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0016: OpenTelemetry, Clickhouse, and Langfuse instrumentation for alert quality

**Status:** Implemented
**Author:** Donald Gifford
**Date:** 2026-05-03

<!--toc:start-->
- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
  - [What we have today](#what-we-have-today)
  - [What we don't have](#what-we-dont-have)
  - [The recurring pattern this proposal addresses](#the-recurring-pattern-this-proposal-addresses)
- [Detailed Design](#detailed-design)
  - [Three components, each with a distinct job](#three-components-each-with-a-distinct-job)
  - [End-to-end trace path](#end-to-end-trace-path)
  - [Langfuse feature investigation](#langfuse-feature-investigation)
    - [1. Generations (LLM call tracing)](#1-generations-llm-call-tracing)
    - [2. Scores](#2-scores)
    - [3. LLM-as-judge](#3-llm-as-judge)
    - [4. Datasets and runs](#4-datasets-and-runs)
    - [5. Prompts (versioning + templating)](#5-prompts-versioning--templating)
    - [6. Sessions (multi-call linking)](#6-sessions-multi-call-linking)
  - [Use cases mapped to features](#use-cases-mapped-to-features)
  - [Architecture sketch](#architecture-sketch)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Instrument the full ingestion → extraction → scoring → alert
pipeline with OpenTelemetry traces and metrics, ship traces to a
self-hosted Clickhouse, and route every LLM call through a
self-hosted Langfuse. The end goal is **moving alert-quality work
from reactive (operator finds noise → patches code) to proactive
(system flags low-quality alerts before they fire and learns from
operator dismissals)**, so that what reaches Discord is more likely
to be a real deal worth acting on.

The Clickhouse and Langfuse deployments themselves are **out of
scope** for this design — assume they exist as platform
infrastructure (separate Helm charts, separate operational concern).
This design covers only how `server-price-tracker` instruments
itself and integrates against those endpoints. Everything Langfuse-
or Clickhouse-related is **config-gated and defaults off**: the app
must continue to work as pure-OTel (or no telemetry at all) when
those backends are not configured.

## Goals and Non-Goals

### Goals

- **End-to-end trace visibility** for any single listing: ingestion
  span → classify span → extract span → normalise span → score span
  → alert-eval span → notify span. Linkable from the alert review UI
  back to the trace that produced it.
- **Per-LLM-call observability via Langfuse**: every classify and
  extract call records prompt, response, model, latency, token
  usage, parsed result, and any normalisation/validation error.
  Replaces the partial picture from
  `spt_extraction_tokens_total{backend, model, direction}`.
- **LLM-as-judge evaluation pipeline** that scores fired alerts
  retrospectively ("is the price/condition pair below baseline by
  enough margin to plausibly be a deal?"). Score is stored as a
  Langfuse score linked to the originating extraction trace.
- **Operator dismissal as labelled data**: when the operator
  dismisses an alert in the review UI, the dismissal is recorded as
  a Langfuse score on the originating trace. Builds a labelled
  dataset over time without explicit labelling work.
- **Pattern discovery**: query Clickhouse for patterns in dismissed
  alerts (dimension: component_type / vendor / line / score
  bucket / extraction confidence / source backend) so the next
  classifier patch is data-driven instead of "operator noticed".
- **Prompt versioning** in Langfuse so we can A/B classifier prompt
  changes against a curated dataset of known correct/incorrect
  classifications, instead of "deploy and watch the buckets".
- **Cost / value telemetry**: per-trace LLM cost vs alert outcome.
  Answers "what did the $0.02 we spent extracting that listing
  actually buy us?" — sets up future conversations about model
  selection and prompt economy.
- **Config-gated, fully optional**: the entire Langfuse + judge
  stack must be disable-able via config such that the app reverts to
  current behaviour (Prometheus metrics + slog only). OTel exporters
  to Clickhouse are independently disable-able. Three modes:
  Prometheus-only (today), Prometheus + OTel, Prometheus + OTel +
  Langfuse + judge.

### Non-Goals

- **Deploying Clickhouse or Langfuse.** Assumed to exist as
  platform infrastructure managed outside this repo (separate Helm
  charts, separate ownership). This design covers only the
  application-side instrumentation that talks to them.
- Replacing the Prometheus + Grafana metrics stack. Dual-emit
  (Prometheus + OTel) indefinitely — the existing Grafana dashboards
  stay on Prometheus and there's no ROI in rebuilding them. OTel
  adds trace-shaped data and Langfuse adds LLM-shaped data on top
  of, not instead of, the current metrics.
- Real-time alert filtering by judge score in v1. The judge runs
  asynchronously (after Discord is notified) and produces a score
  used for *future* learning; we don't gate the notification on it
  in this iteration. Open Question 4 covers when we might.
- Sampling design beyond "trace 100% of extractions, sample
  ingestion at 10%". Real sampling tuning waits until we see actual
  Clickhouse storage growth.
- Replacing or rebuilding the existing alert review UI. Add Langfuse
  trace deep-links and judge-score columns to it; don't redo it.
- Replacing Discord as the notification channel. Discord summary +
  alert review UI stays as-is; we're improving the *quality* of
  what gets sent, not the transport.

## Background

### What we have today

- **Prometheus metrics**: `spt_extraction_tokens_total{backend,
  model, direction}`,
  `spt_extraction_tokens_per_request{backend, model}`,
  `spt_alerts_created_total{component_type}`,
  `spt_alerts_fired_total`, plus the standard HTTP/queue/rate-limiter
  series. Good for operational health, weak for "what happened on
  *this one* listing."
- **Structured logs** (slog): every `Classify` / `Extract` /
  `ClassifyAndExtract` call logs raw response, parsed result, errors.
  Useful for incident investigation but not queryable across the
  fleet.
- **Alert review UI** (`/alerts`, DESIGN-0010): operators see fired
  alerts, can dismiss them. Dismissal is recorded but only used to
  hide rows; the signal is not fed back into anything that improves
  classification.
- **DB views and SQL helpers** (`docs/SQL_HELPERS.md`): manual
  queries for queue depth, baseline maturity, NULL-component-type
  backfill. The standard "operator runs SQL to figure out what
  happened" workflow.

### What we don't have

- **Pipeline-level trace correlation**: when an alert fires, there's
  no easy way to walk back through "which classify call → which
  extract call → which normaliser pass → which score evaluation"
  produced it. Each stage logs separately.
- **Per-LLM-call detail in queryable storage**: Prometheus aggregates
  by labels, so we lose the "what was the prompt for *this listing*?
  what did the model return? what tokens did it cost?" view.
  Investigating a single weird extraction means grepping logs.
- **Quality feedback loop**: dismissals stop at the UI. We don't
  systematically ask "of the last 100 alerts that fired, how many
  did the operator dismiss within 24 hours? what do those alerts
  have in common?" The answer is locked in operator judgment.
- **Prompt change validation**: when we patch the classifier prompt
  (workstation/desktop rules in IMPL-0018, accessory rules in
  DESIGN-0011, tier rules in IMPL-0016), we ship and watch the
  buckets. There's no curated dataset to regression-test against.
- **LLM-as-judge or any auto-eval**: the only "quality signal" we
  have on an extraction is `extraction_confidence`, which is
  whatever the LLM self-reported (not calibrated against ground
  truth).

### The recurring pattern this proposal addresses

Look at the last six months of work:

- DESIGN-0011 — operator notices ~88 score noise floor; patches
  `priceScore` curve and adds accessory pre-classifier.
- DESIGN-0012 / IMPL-0017 — operator notices GPUs polluting `other`;
  adds `gpu` ComponentType + family-inference rules + product-key
  canonicalisation. Two iterative fixes during dev validation.
- DESIGN-0015 / IMPL-0018 — operator notices workstations polluting
  `server`; adds `workstation` + `desktop` ComponentTypes. Two
  iterative fixes during dev validation, plus a server-line
  hallucination denylist found in Phase 6.
- Three follow-ups still parked: BOXX brand, Dell T-prefix without
  "Precision", Threadripper Pro custom builds.

The shape is identical every time:

1. Operator notices alert noise or a missing classification.
2. Operator runs SQL queries to characterise the pattern.
3. Operator (or AI assistant) patches code: regex + normaliser +
   prompt + product key + migration.
4. Re-extract historical listings + drop orphan baselines.
5. Discover an edge case during dev validation, patch again.
6. Discover another edge case during prod rollout, patch again.

The patches themselves are sound. The *discovery latency* is
expensive — it requires the operator to have already noticed the
problem and have time to investigate. **A trace + LLM-observability
stack moves discovery latency from "operator notices weeks later"
to "dashboard shows score-divergence trend within hours".**

## Detailed Design

### Three components, each with a distinct job

| Component  | Job | Why this and not Prometheus / Postgres |
|------------|-----|----------------------------------------|
| OpenTelemetry SDK + Collector | Emit + route trace and metric data from the Go application | Vendor-neutral; lets us swap the storage backend without re-instrumenting |
| Clickhouse (assumed) | Long-term storage of traces and high-cardinality metrics | Built for trace-shaped data: high write rate, columnar, fast aggregation by trace_id / span attributes; postgres tuned for OLTP, prometheus discards labels with high cardinality |
| Langfuse (assumed) | LLM-specific observability: trace LLM calls, store prompts/responses, run scores and evals | LLM call has structure (prompt → completion → usage → cost) that generic tracing can capture but not query well; Langfuse's data model + UI is built for this and supports LLM-as-judge workflows out of the box |

Clickhouse and Langfuse deployments are out of scope (managed
separately, like CNPG Postgres and Ollama). This design assumes
both endpoints are reachable from the cluster and provided via
config. The application itself only ships an OTel SDK + a thin
Langfuse SDK wrapper — both with no-op fallbacks when their
respective backends are unconfigured.

### End-to-end trace path

```text
Ingestion loop tick
  └── span: ingest_watch (watch_id, query)
       └── span: ebay_browse_call
       └── span: per-listing-upsert
            └── span: enqueue_extraction (listing_id, priority)

Extraction worker pulls from queue
  └── span: extract_listing (listing_id)
       ├── span: preclassify_title       (DetectSystemTypeFromTitle)
       ├── span: preclassify_specifics   (DetectSystemTypeFromSpecifics)
       ├── span: classify
       │    └── langfuse generation: classify-llm
       │         (prompt, completion, model, tokens, latency)
       ├── span: extract
       │    └── langfuse generation: extract-llm
       ├── span: normalize
       └── span: validate

Scheduler scoring tick
  └── span: score_listing (listing_id)
       ├── span: lookup_baseline (product_key)
       └── span: compute_breakdown

Alert evaluation
  └── span: evaluate_alert (listing_id, watch_id, score)
       └── (if alert fires) span: notify_discord

Async judge worker (NEW)
  └── span: judge_alert (alert_id)
       └── langfuse generation: judge-llm
            └── langfuse score: alert-quality (0.0-1.0)
```

Every span carries `listing_id`, `watch_id`, `component_type`,
`product_key` as attributes so Clickhouse queries can slice freely.
Trace IDs propagate through the queue (stored on the
`extraction_queue` row so the worker continues the trace, not starts
a new one).

### Langfuse feature investigation

Langfuse exposes several features beyond raw LLM call tracing.
Investigation here covers what each does and which of our problems
it would address:

#### 1. Generations (LLM call tracing)

Every LLM call becomes a `generation` record with: model, prompt,
completion, input/output tokens, cost, latency, parent trace.
**This replaces our manual log-grep workflow for "what happened on
listing X"** — open the trace, see every prompt+completion in
sequence.

Direct usefulness: high. Foundation for everything else.

#### 2. Scores

Numeric or categorical labels attached to a trace or generation.
Sources: SDK call (auto), human (UI), or another LLM ("LLM-as-judge").

Use cases for us:

- **Auto-score on extraction confidence**: every successful extract
  emits a `extraction_self_confidence` score from the LLM's own
  `confidence` field. Lets us correlate self-reported confidence
  vs operator-dismissal rate (do confident extractions actually
  produce real deals?).
- **Operator dismissal as score**: when an operator dismisses an
  alert, write a `dismissed=1` score on the originating trace.
  Over time this builds a labelled dataset.
- **Judge score on fired alerts** (see below): every fired alert
  gets a `judge_alert_quality` score from a separate LLM call.

Direct usefulness: very high. The dismissal score alone unlocks
labelled data we currently throw away.

#### 3. LLM-as-judge

A managed feature where Langfuse runs an evaluator prompt against
selected traces and stores the result as a score. Configured with:
target trace pattern, evaluator prompt template, eval model, scoring
rubric.

Use cases for us:

- **Alert quality judge**: "Given this listing title, condition,
  price, and the historical baseline (p50, p25), is this plausibly
  a real deal a buyer would act on, or noise? Reply with a score
  0.0-1.0 and a one-sentence reason." Run on every fired alert
  asynchronously; store the score.
- **Classification correctness judge**: on a sample of extractions
  per type, prompt the judge with the title + extracted attributes
  + assigned component_type and ask "does this classification
  match the title?" Catches the LLM-puts-PowerEdge-on-workstation
  failure mode without needing the operator to spot it.
- **Extraction quality judge**: "Given this title and the extracted
  attributes, are all the values plausibly correct?" Catches RAM
  capacity unit confusion, GPU VRAM mistakes, etc.

Direct usefulness: very high. This is the lever that turns "we
react to noise" into "we proactively grade ourselves".

#### 4. Datasets and runs

A "dataset" is a versioned collection of inputs (e.g., real listing
titles + their correct classification). A "run" executes a prompt or
chain over the dataset and records all outputs as a comparable group.

Use cases for us:

- **Curated golden dataset** of ~100 listings spanning all
  ComponentTypes, with operator-validated correct classifications +
  product keys. Live in Langfuse, version-controlled.
- **Pre-deploy regression**: every classifier prompt change runs
  against the golden dataset; CI fails if accuracy drops below a
  threshold. Replaces "deploy and watch buckets".
- **Backend comparison**: same dataset run against Ollama
  qwen2.5:3b, Anthropic Haiku, OpenAI gpt-4o-mini — see which
  classifier produces the best accuracy for our problem at what
  cost.

Direct usefulness: high. Avoids the IMPL-0017/0018 "ship and
discover edge cases in dev validation" cycle.

#### 5. Prompts (versioning + templating)

Store prompt templates in Langfuse with version tags. App fetches
the active version at runtime (cached). Every generation records
which prompt version was used.

Use cases for us:

- **Version tracking**: `classify_prompt:v3` is in production;
  `classify_prompt:v4` is in canary. Compare scores between
  versions across the same time window.
- **Faster iteration**: tweak the classifier prompt in the Langfuse
  UI, deploy a new version, no code change. Roll back by promoting
  the prior version.
- **A/B test infrastructure**: split traffic between two prompt
  versions, compare judge scores.

Direct usefulness: medium. Faster iteration is nice but our
classifier prompts change rarely (3-4 times per IMPL plan).
**Decision: keep prompts in code (git history + code review wins
over UI iteration), but tag every Langfuse generation with the
commit SHA that produced the prompt.** Best of both — code stays
the source of truth, Langfuse can still group/compare results by
prompt version without needing the prompt-management feature.

#### 6. Sessions (multi-call linking)

Group multiple traces under a "session". Useful when one user action
triggers multiple LLM calls.

Direct usefulness: low for us. Our pipeline is one trace per
listing; sessions don't add much.

### Use cases mapped to features

| Goal | Langfuse feature | Other component | Done by |
|------|------------------|-----------------|---------|
| Walk back from a noisy alert to the prompt+response that produced it | Generations + Trace | OTel trace_id propagation | Phase 1-3 |
| Catch LLM hallucinations like PowerEdge-on-workstation without operator noticing | LLM-as-judge: classification correctness | Judge writes score on extract trace; alert review UI shows judge score column | Phase 4 |
| Stop dismissed alerts from being a dead-end signal | Manual scores on operator dismiss | Alert review UI calls Langfuse SDK on dismiss action | Phase 4 |
| Validate prompt changes before deploy | Datasets + runs | CI job runs golden dataset against new prompt | Phase 5 |
| Identify systematic noise patterns | Score aggregation queries | Clickhouse trace data + Langfuse score export | Phase 5 |
| Per-listing cost vs value | Generation cost field | Linked to alert outcome via trace_id | Phase 5 |

### Architecture sketch

```text
┌────────────────────────────────────────────────────────┐
│                Go application (server-price-tracker)   │
│                                                        │
│   ┌──────────────────────────────────────────────────┐ │
│   │ otel-go SDK (traces + metrics)                   │ │
│   │ langfuse-go SDK (LLM generations + scores)       │ │
│   └──────────────────────────────────────────────────┘ │
│                  │                          │          │
└──────────────────┼──────────────────────────┼──────────┘
                   │ OTLP (gRPC)              │ HTTPS
                   ▼                          ▼
           ┌──────────────────┐    ┌──────────────────┐
           │ OTel Collector   │    │ Langfuse server  │
           │ (in-cluster)     │    │ (in-cluster)     │
           └────────┬─────────┘    └────────┬─────────┘
                    │                       │
                    ▼                       ▼
           ┌──────────────────┐    ┌──────────────────┐
           │  Clickhouse      │    │  Postgres        │
           │  (traces +       │    │  (Langfuse meta) │
           │   metrics)       │    │  + Clickhouse    │
           │                  │◀───│  (Langfuse       │
           │                  │    │   events store)  │
           └────────┬─────────┘    └──────────────────┘
                    │
                    ▼
           ┌──────────────────┐
           │  Grafana         │
           │  (existing)      │
           └──────────────────┘
```

Notes:

- Langfuse uses Postgres for metadata + Clickhouse for events.
  Sharing one Clickhouse between OTel and Langfuse is a deployment
  decision left to the platform side; this design doesn't require
  it.
- OTel Collector continues exporting Prometheus metrics so the
  existing Grafana dashboards keep working unchanged. Dual-emit is
  the long-term steady state, not a transition.
- Async judge worker is a Go cron / scheduler job in the same
  binary, not a new service. Reads recent fired alerts from
  Postgres, calls the judge LLM, writes scores back to Langfuse.
  Disabled when `observability.judge.enabled=false` — at which
  point the app behaves identically to today.

## API / Interface Changes

- **Config (additive)**: new `observability:` section with three
  independently disable-able subtrees:
  - `observability.otel` — endpoint, sampling rate, service name.
    Disabled → no traces emitted (current behaviour).
  - `observability.langfuse` — endpoint, public+secret keys.
    Disabled → LLM-call tracing skipped; OTel still active if its
    own subtree is enabled.
  - `observability.judge` — enabled flag, model backend (default
    `anthropic` / `claude-haiku`), schedule, batch size. Disabled
    → judge worker doesn't start.

  All three default off. A deployment with all three off behaves
  identically to today.
- **CLI (additive)**: `spt judge run --since <duration>` — manually
  trigger judge on recent alerts (operator can backfill scores).
- **HTTP API (additive)**: `GET /api/v1/alerts/{id}/trace` returns
  the Langfuse trace URL deep-link for an alert. Powers the alert
  review UI's "View trace" button. Returns 404 when Langfuse is
  disabled.
- **Internal interface**: `LLMBackend.Generate` gains a
  `traceCtx` parameter so the backend wrapper can attach the
  generation to the right Langfuse trace. Backwards compatible via
  context.Context propagation; no-op when Langfuse SDK is the
  no-op variant.
- **No breaking changes**, no DB migrations required for the
  application data model. New tables are Langfuse-internal.

## Data Model

- **No app schema changes.** Trace IDs are kept in
  `extraction_queue.trace_id` (new column, nullable) and
  `alerts.trace_id` (new column, nullable) so the alert review UI
  can build deep-links. Both nullable so historical rows aren't
  affected.
- **Clickhouse schema**: managed by OTel Collector + Langfuse
  installers. We don't write to it directly.
- **Langfuse-side data**: traces, generations, scores, datasets.
  Owned by Langfuse, queried via its API or UI.
- **Retention**: configured platform-side on the Clickhouse
  deployment (target 90 days for full traces). Out of scope for
  this design.

## Testing Strategy

- **Unit**: span emission verified with the OTel `tracetest` SDK;
  Langfuse SDK calls mocked behind an interface (same pattern as
  `LLMBackend`).
- **Integration**: in-process OTel collector + in-memory exporter
  asserts span attributes and parent-child relationships for one
  end-to-end ingestion → extract → score → alert flow.
- **No e2e Clickhouse / Langfuse**: the deployments themselves are
  validated by chart-testing (Helm `make helm-test`), not by Go
  integration tests. Cross-service e2e is operator-driven smoke
  test in dev (mirrors IMPL-0018 Phase 5).
- **Judge prompt regression**: golden dataset of ~30 alerts with
  operator-assigned ground truth. Judge prompt changes must keep
  accuracy above a threshold on this set; runs in CI.

## Migration / Rollout Plan

Sketch — full sequencing belongs in IMPL-0019.

1. **Phase 1** — instrument Go code with OTel SDK; deploy OTel
   Collector locally + Clickhouse Helm chart; verify traces flow.
   No Langfuse yet. Behind config flag, default off.
2. **Phase 2** — wrap LLMBackend implementations with Langfuse
   generation calls. Manual scores still TBD. Auto-score with
   self-reported `confidence`. Verify generations appear in
   Langfuse UI.
3. **Phase 3** — alert review UI gains trace deep-links + judge
   score column (column empty until Phase 4 lights it up).
4. **Phase 4** — async judge worker; operator-dismiss-as-score
   wired in alert review UI. Cold-start golden dataset of ~30
   labelled alerts.
5. **Phase 5** — datasets + CI regression; Grafana dashboards for
   trace-derived metrics; weekly review process documented in
   `docs/OPERATIONS.md`.
6. **Phase 6** — production rollout; gradual sampling rate increase
   from 10% → 100% as Clickhouse capacity confirms.

Backwards compat throughout: every new dependency is feature-flagged
and defaults off. Existing deployments stay on Prometheus-only until
the operator opts in.

## Open Questions

All ten original open questions are resolved; recording the
decisions here for the record. None remain blocking IMPL-0019.

1. **Self-host vs cloud Langfuse + Clickhouse?** **Resolved: out of
   scope.** Both will be self-hosted, but the deployments are owned
   by the platform side and tracked separately. This design assumes
   the endpoints exist and are reachable from the cluster.
2. **Judge LLM model.** **Resolved: Anthropic Claude Haiku for v1.**
   Track per-call costs via Langfuse `generation.cost`; revisit if
   judge spend exceeds an agreed threshold. (Threshold to be set in
   IMPL-0019 alongside the Grafana panel.)
3. **Bootstrapping the judge.** **Resolved.** Operator labels ~30
   alerts manually as `deal` / `noise` / `edge` and we feed them as
   few-shot examples in the judge prompt. Cold-start work happens
   in IMPL-0019 Phase 4.
4. **Should the judge gate notifications?** **Resolved: not in
   v1.** Judge is config-gated (`observability.judge.enabled`) and
   runs async after Discord. The whole judge component must be
   fully disable-able such that the app reverts to current
   behaviour. Gating notifications on judge score is a v2
   conversation after we have data.
5. **Trace retention.** **Resolved: 90 days, configured platform-
   side.** Out of scope for this design — retention lives with the
   Clickhouse deployment.
6. **Sampling strategy.** **Resolved.** OTel head sampling: 10%
   ingestion / 100% extract + score + alert / 100% judge. Revisit
   once cardinality is observable.
7. **Cost ceiling for the observability stack.** **Resolved: not a
   concern for this design** (deployments are out of scope).
8. **Integration with existing Prometheus.** **Resolved: dual-emit
   indefinitely.** App must also work as pure-OTel — i.e., with
   Langfuse + Clickhouse exporters disabled — via config, so we can
   strip either backend without re-instrumenting code.
9. **Privacy / PII.** **Resolved: not a concern.** Listings are
   already public eBay data; seller usernames carry no additional
   sensitivity beyond what's on eBay itself.
10. **Prompt management.** **Resolved: best of both.** Prompts stay
    in code (preserves git history + code review). Every Langfuse
    generation tags the commit SHA that produced the prompt as a
    metadata field, so Langfuse UI can still group/compare runs by
    prompt version.

## References

- DESIGN-0007 — LLM Token Metrics (current Prometheus-side telemetry)
- DESIGN-0010 — Alert review UI (where judge scores will surface)
- DESIGN-0011 — Reduce alert noise via scoring + accessory
  pre-classifier (the recurring problem this design addresses)
- IMPL-0017 / IMPL-0018 — recent ComponentType additions (case
  studies for the iterative-fix pattern)
- OpenTelemetry Go SDK: <https://opentelemetry.io/docs/languages/go/>
- Clickhouse OTel exporter:
  <https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/clickhouseexporter>
- Langfuse self-host: <https://langfuse.com/docs/deployment/self-host>
- Langfuse LLM-as-judge:
  <https://langfuse.com/docs/scores/model-based-evaluations>
- Langfuse datasets + evals:
  <https://langfuse.com/docs/datasets/overview>
