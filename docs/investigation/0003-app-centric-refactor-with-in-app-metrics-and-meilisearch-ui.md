---
id: INV-0003
title: "App-centric refactor with in-app metrics and Meilisearch UI"
status: In Progress
author: Donald Gifford
created: 2026-05-15
---
<!-- markdownlint-disable-file MD025 MD041 -->

# INV 0003: App-centric refactor with in-app metrics and Meilisearch UI

**Status:** In Progress
**Author:** Donald Gifford
**Date:** 2026-05-15

<!--toc:start-->
- [Question](#question)
- [Hypothesis](#hypothesis)
- [Context](#context)
- [Approach](#approach)
- [Findings](#findings)
  - [1. Metrics: stop pushing business KPIs to Prometheus](#1-metrics-stop-pushing-business-kpis-to-prometheus)
    - [Current state](#current-state)
    - [Target state](#target-state)
    - [Grafana panels affected](#grafana-panels-affected)
  - [2. Search: Meilisearch as the read-path index](#2-search-meilisearch-as-the-read-path-index)
    - [Why a search index at all](#why-a-search-index-at-all)
    - [Candidate engines](#candidate-engines)
    - [Sync mechanism](#sync-mechanism)
    - [What the index document looks like](#what-the-index-document-looks-like)
  - [3. Web UI: real frontend, not templ+HTMX](#3-web-ui-real-frontend-not-templhtmx)
    - [Why drop the current stack](#why-drop-the-current-stack)
    - [Stack candidates](#stack-candidates)
    - [Repo shape](#repo-shape)
    - [What the SPA does that the current UI doesn't](#what-the-spa-does-that-the-current-ui-doesnt)
  - [4. OTel: tighten to RED-style](#4-otel-tighten-to-red-style)
    - [Current spans (post-IMPL-0019)](#current-spans-post-impl-0019)
    - [RED-style target](#red-style-target)
    - [Why this matters](#why-this-matters)
  - [5. Langfuse: close the prompt-iteration loop](#5-langfuse-close-the-prompt-iteration-loop)
    - [Current state](#current-state-1)
    - [The loop, end-to-end](#the-loop-end-to-end)
    - [What the app needs to support this](#what-the-app-needs-to-support-this)
    - [Better filters at extraction time](#better-filters-at-extraction-time)
  - [6. How this changes INV-0002's scope](#6-how-this-changes-inv-0002s-scope)
- [Conclusion](#conclusion)
- [Recommendation](#recommendation)
- [References](#references)
<!--toc:end-->

## Question

We built the v1 metrics surface as **Prometheus → Grafana** for business
intelligence (best deals, alert volume by component, judge agreement
rate). That was wrong: a metrics pipeline is for system-health observation,
not for queries the *application itself* needs to answer in real time.
And the templ+HTMX alert review UI proved insufficient for operator
workflow ("basically useless" per the user).

**Should we refactor toward an app-centric model where:**

1. **The app owns its own business intelligence** — answers "best deal
   right now for component X" from its own data layer, not from Grafana.
2. **Prometheus is reduced to system metrics** — request latency, queue
   depth, memory, build_info. No more business labels.
3. **A real search index (Meilisearch) powers a real web UI** with
   faceted search, typo tolerance, and live updates.
4. **OTel traces are tightened to RED-style** — Rate, Errors, Duration
   per pipeline stage; no more every-listing fan-out spans.
5. **Langfuse scores close back into a prompt-iteration workflow** —
   judge-vs-operator divergence drives dataset growth and prompt
   versioning.

And: **how much of INV-0002's planned cleanup is invalidated, made
redundant, or made more urgent by this refactor?**

## Hypothesis

The current design conflates three layers:

1. **Observability** — emit signals about system health (correctly
   handled by Prometheus + OTel + Langfuse)
2. **Business intelligence** — answer "what's the best deal right now"
   (incorrectly delegated to Prometheus + Grafana)
3. **User interface** — operator workflow for triage and tuning
   (incorrectly built as a server-rendered debug surface)

Conflating (1) and (2) means business queries depend on a metrics
pipeline that was never designed for them — high cardinality limits, no
joins, no facets. Conflating (2) and (3) means the UI is a thin shell
around whatever Grafana happens to expose, and the operator workflow
is whatever PromQL allows.

**Hypothesis:** Splitting these three concerns will (a) remove a class
of label-cardinality fears, (b) make the operator workflow actually
usable, (c) preserve OTel/Prom for what they're each good at, and
(d) cut roughly **20-30% of the INV-0002 cleanup scope** because the
templ+HTMX layer goes away and several metric concerns become moot.

## Context

**Triggered by:**

- INV-0002 (architectural review) surfaced 24 issues but treated the
  current UI/metrics design as a given.
- Operator feedback: "our alerts UI is basically useless. What's a SQL
  query I can use to get the best r740xds in the last day" — the UI
  doesn't answer the operator's first question.
- IMPL-0019 work shipped judge scores into Langfuse but no in-app
  surface uses them. The data is there; the path to it goes through
  three external tools.
- Recent work on `tools/dashgen` is producing more business panels in
  Grafana when the underlying premise (Grafana as the operator's tool)
  is wrong.

This investigation is the parent for any subsequent DESIGN/IMPL docs
that propose the actual refactor. It is **scoping**, not
**specification**.

## Approach

1. **Inventory the current metrics surface** — separate system metrics
   from business metrics; identify what each is used for; identify
   which Grafana panels lose their data source if business metrics
   move out.
2. **Sketch the target data flow** — engine → DB → search index → UI;
   identify the sync mechanism (outbox vs CDC vs eventual reindex).
3. **Pick the search index** — Meilisearch, Typesense, Postgres
   full-text, OpenSearch — by required capabilities, ops cost,
   familiarity.
4. **Pick a frontend stack** — by team familiarity, deployment
   footprint, server-rendering needs, time-to-first-meaningful-UI.
5. **Map each INV-0002 finding** to: still-applies / moot / urgent /
   deferred.
6. **Propose a phased rollout** with deploy-able checkpoints.

## Findings

### 1. Metrics: stop pushing business KPIs to Prometheus

#### Current state

Two categories of metric currently cohabit `internal/metrics`:

| Category | Examples |
|---|---|
| **System** | `spt_request_duration_seconds`, `spt_request_total{path,status}`, `spt_panic_total`, `spt_build_info` |
| **Business** | `spt_alerts_fired_total{watch_name}`, `spt_alerts_created_total{component_type}`, `spt_extraction_completed_total{component_type}`, `spt_listings_active_total{component_type}`, `spt_judge_evaluations_total{verdict}`, `spt_judge_score_bucket`, `spt_alerts_dismissed_total`, `spt_judge_cost_usd_total{model}` |

The business metrics exist because Grafana is currently the only way to
ask "how many listings did we score yesterday?" or "what's the judge
agreement rate this week?". `tools/dashgen` then builds panels off
those counters.

This is the wrong architecture for three reasons:

1. **Cardinality fragility.** Labels like `watch_name` (operator-set
   free text) explode the series count (INV-0002 §3.8). The fix is not
   to switch to `watch_id` and join in Grafana — the fix is to not put
   the data in Prometheus at all. Postgres can join.
2. **Lossy.** A counter loses the listing-level detail. "We extracted
   42 servers yesterday" is a number; the actual 42 listings carry
   product keys, prices, baselines, judge verdicts, and Langfuse trace
   IDs. The interesting analysis happens at the row level.
3. **Misaligned with the operator workflow.** "Show me R740xd
   listings under $400 in the last 24 hours with judge verdict ≠ noise"
   is a *query*, not a *dashboard*. PromQL can't do it.

#### Target state

**Keep in Prometheus** (system health only):

- HTTP `spt_request_duration_seconds`, `spt_request_total{path,status}`
- Queue depth: `spt_extraction_queue_depth`,
  `spt_alert_notification_queue_depth`
- Build/runtime: `spt_build_info`, `go_goroutines`,
  `process_resident_memory_bytes`
- External-call rate/error: `spt_ebay_calls_total{result}`,
  `spt_llm_backend_calls_total{backend,result}`,
  `spt_db_calls_total{op,result}`
- Buffer health: `spt_langfuse_buffer_depth`,
  `spt_langfuse_buffer_drops_total`
- Panics: `spt_panic_total`

**Remove from Prometheus** (or downgrade to once-per-tick gauges):

- `spt_alerts_fired_total{watch_name}` → drop; query DB
- `spt_alerts_created_total{component_type}` → drop; query DB
- `spt_extraction_completed_total{component_type}` → drop; query DB
- `spt_listings_active_total{component_type}` → optional gauge,
  unlabelled or `{type}` only (small bounded set)
- `spt_judge_evaluations_total{verdict}` → drop; query DB
- `spt_judge_score_bucket` → drop; query DB
- `spt_judge_cost_usd_total{model}` → keep (small bounded labels;
  budget control needs alerting)

**Net effect:** Prometheus stops being the source of truth for business
KPIs. Operator queries hit the app's API which hits Postgres + search.

#### Grafana panels affected

`tools/dashgen/panels/observability.go` currently builds 4 business
panels (JudgeScoreDistribution, JudgeVsOperatorAgreement,
JudgeCostByModel, PipelineStageVolume). Of those:

- **JudgeScoreDistribution, JudgeVsOperatorAgreement** — move into the
  app UI as charts on a "judge health" page (the operator wants these,
  but they belong next to the dismiss button, not in Grafana).
- **JudgeCostByModel** — stays in Grafana (budget alerting is the
  use case).
- **PipelineStageVolume** — convert to RED-style spans-derived
  panel (rate of `extract`, `score`, `notify` spans per minute). Stays
  in Grafana as ops view.

`tools/dashgen` itself shrinks substantially — most of `panels/*.go`
becomes UI-side code. Plan to deprecate per-component-type panels
entirely.

---

### 2. Search: Meilisearch as the read-path index

#### Why a search index at all

The operator's working query — "best deals right now for component
type X" — needs:

- **Full-text search** over title + extracted attributes (typo-tolerant
  matching for "r740xd" vs "R740XD" vs "PowerEdge R740xd").
- **Faceting** by `component_type`, watch, judge verdict, dollar-
  below-P50 bucket, condition, seller country.
- **Range filters** on score, price, `created_at`.
- **Multi-field sort** (score desc, price asc, baseline_pct asc).
- **Sub-100ms latency** on the entire dataset (~50k active listings,
  growing).

Postgres can do all of this — `pg_trgm` + `tsvector` + B-tree indices —
but at noticeably worse ergonomics and latency for the faceted-search
shape. The UI needs `count(*) GROUP BY component_type, judge_verdict`
on every keystroke; a dedicated search engine returns facet counts
in the same response as the result set.

Postgres remains the system of record. The search index is a read-
optimised projection.

#### Candidate engines

| Engine | Pros | Cons |
|---|---|---|
| **Meilisearch** | Drop-in faceting, typo tolerance, simple API, single binary, low ops cost, can sync from Postgres via `meilisync` | Resource-hungry at scale (in-memory by default); single-node by default |
| **Typesense** | Similar API to Meilisearch, slightly more efficient on memory, multi-node clustering built in | Smaller community |
| **Postgres FTS** | Already deployed, no new service to operate | No native faceting; query gymnastics for facet counts; slower under typo tolerance |
| **OpenSearch** | Mature, multi-node, rich query DSL | Heavyweight; JVM ops cost |

**Recommend: Meilisearch.** It matches the operator's expectations
(Google-like fuzzy search), runs in one container, has explicit
faceting, and a `meilisync` container exists for Postgres → Meili sync
if we want CDC instead of app-side dual-write.

Concretely:

- Add a Meilisearch StatefulSet to the Helm chart, off by default
  (`search.enabled=false`).
- Index: `listings` — one document per row, denormalised with watch
  name, judge verdict, baseline metadata, and a `flags` array (e.g.
  `["deal", "fresh-24h", "below-p25"]`) for quick faceting.
- Sync mechanism: see below.

#### Sync mechanism

Three options, ordered by complexity:

1. **App-side dual-write** — engine writes to Postgres AND
   Meilisearch in the same tick. If Meilisearch is down, log and skip;
   reconcile on a periodic full reindex.
2. **Outbox table + worker** — engine writes to Postgres + an
   `outbox_listing_changes` table in the same tx. Background worker
   drains the outbox to Meilisearch. Survives Meilisearch outages.
3. **`meilisync` CDC container** — uses Postgres logical replication.
   Operator runs a separate process. Zero app-side code.

**Recommend: start with (1).** It's the smallest change. Add (2) when
we observe Meilisearch reindex storms or sync drift. (3) only if
Meilisearch's resource footprint becomes problematic enough to want
zero app-side coupling.

The full reindex job (boot or daily) reads from Postgres and rebuilds
the index from scratch — covers all gap scenarios.

#### What the index document looks like

```json
{
  "id": "ebay-v1-145012345678",
  "title": "Dell PowerEdge R740xd 12x3.5\" Server",
  "component_type": "server",
  "product_key": "server:dell:r740xd:lff:configured",
  "price_usd": 389.99,
  "currency": "USD",
  "score": 78,
  "baseline_p50": 850.0,
  "pct_below_p50": 54.1,
  "sample_count": 87,
  "watch_id": "...",
  "watch_name": "Dell R740xd LFF deals",
  "condition": "used",
  "seller_country": "US",
  "judge_verdict": "deal",
  "judge_score": 0.85,
  "operator_dismissed": false,
  "flags": ["deal", "fresh-24h", "below-p25", "judge-deal"],
  "created_at": 1731648000,
  "ebay_url": "...",
  "trace_id": "..."
}
```

Searchable attrs: `title`, `product_key`. Filterable attrs:
`component_type`, `watch_id`, `judge_verdict`, `flags`, `score`,
`price_usd`, `pct_below_p50`. Sortable attrs: `score`, `price_usd`,
`pct_below_p50`, `created_at`.

---

### 3. Web UI: real frontend, not templ+HTMX

#### Why drop the current stack

The templ+HTMX surface was the right MVP choice — server-rendered HTML,
Go-only stack, zero JS toolchain. It's reached its limit:

- **Operator workflow needs multi-pane interaction.** Pinned filters
  + live search + per-row drill-down + bulk dismiss + Langfuse trace
  inline preview. HTMX partial swaps can technically do this, but the
  state coordination becomes complex enough that a real reactive
  framework is the right tool.
- **Search-driven UX needs sub-100ms feedback.** Every keystroke hits
  Meilisearch and re-paints results. HTMX can do this with
  `hx-trigger="input changed delay:200ms"`, but the implementation
  becomes a tangle of small partials.
- **The "useless" feedback** isn't about lack of features — it's
  about the UI shape being wrong. A list with checkboxes is fine for
  notification dismissal but not for the actual operator task of
  "find the deal-grade listings I haven't looked at".

#### Stack candidates

| Stack | Pros | Cons |
|---|---|---|
| **SvelteKit** | Small bundles, simple syntax, SSR built-in, easy to learn, fast | Smaller ecosystem than React |
| **Vue 3 + Vite** | Lowest learning curve, mature, mature search-UI components, good Meilisearch SDK | No first-class SSR by default |
| **Next.js + React** | Largest ecosystem, mature, SSR/RSC | Heaviest dependency footprint, React verbosity |
| **Astro + Svelte/Vue islands** | Static-site-first; only ship JS where needed | Less ideal for live-update-heavy pages |
| **Keep templ+HTMX, layer Alpine reactivity** | No new toolchain | We just established this isn't enough |

**Recommend: SvelteKit.** Smallest mental model, smallest bundle, SSR
where we want it (e.g., shareable URLs to a pre-filtered alert view),
client-side reactivity where the search-as-you-type UX needs it. The
Meilisearch JS SDK works fine there.

Alternative: **Vue 3 + Vite** if SvelteKit's smaller ecosystem is a
concern. Both will work; the choice is taste + familiarity.

**Not Next.js** — overkill, and we'd be importing React's whole
worldview for a single internal tool.

#### Repo shape

Two reasonable layouts:

- **Monorepo with `/web` directory.** Go binary serves the API; web
  is a separate build artifact deployed alongside (or embedded via
  `go:embed` for the dist directory). Simplest deploy.
- **Separate repo `server-price-tracker-ui`.** Deployed as a
  separate container with its own image and Helm subchart. Cleaner
  separation, more CI overhead.

**Recommend: monorepo with `/web` directory + `go:embed` of the
production build.** Keeps the single-deploy model. The CI step
`make web-build` produces `web/dist/` which `go:embed` picks up.

The existing `internal/api/web` (templ) gets retained for `/docs`
(Huma) and `/healthz` plain text, but `/alerts` is replaced by the new
SPA mounted at `/` (or `/app`).

#### What the SPA does that the current UI doesn't

- **Live faceted search.** Type "r740xd 384gb", see results filter
  in <100ms.
- **Saved views.** "Today's deal-grade alerts" / "Last 7 days
  judge-noise to triage" / "Workstation watchlist" — operator-owned
  bookmarks.
- **Per-listing detail panel** with: Langfuse trace, judge verdict
  + reason, baseline curve, raw eBay specifics, dismiss/restore/note.
- **Bulk operations.** Multi-select, dismiss all, mark seen.
- **Prompt-iteration workspace** (small for now; see §5) — view
  judge-operator disagreements, queue for prompt-revision dataset.
- **Watch management UI.** Today's `spt watches` CLI replicated as a
  form-based editor with live validation.

The CLI (`spt`) stays as the API client; it's not deprecated.

---

### 4. OTel: tighten to RED-style

#### Current spans (post-IMPL-0019)

Two histograms exist (`spt.extraction.duration`,
`spt.alert.eval.duration`). The pipeline currently emits spans for:

- Ingestion tick (per watch, per page)
- Each LLM extraction call
- Each listing scoring call
- Each alert evaluation
- Each Discord notification call
- Each judge evaluation

This is fine for *targeted* debugging but produces a lot of low-value
spans during normal operation (per-listing fan-out spans on a 200-
listing batch).

#### RED-style target

**Rate, Errors, Duration** per operation, not per element:

- One span per scheduler tick (ingestion / baseline / re-extract /
  judge), with attributes for batch size, success count, error count.
- One span per HTTP request (Huma already emits this).
- One span per external call (eBay browse, Ollama generate, Anthropic
  generate, Discord webhook) — these are the things that fail and
  matter to debug.
- **Drop** per-element fan-out spans — replace with batch-level span
  attributes (`spt.batch.size`, `spt.batch.success`,
  `spt.batch.errors`) and per-element OTel events (lighter than
  spans) only for failures.

#### Why this matters

- Trace storage cost drops (~10x reduction in span count per tick).
- Trace UIs (Tempo, Jaeger) become readable — one tick is one trace
  with a manageable span tree, not a 200-row mess.
- The signal-to-noise ratio for "this tick had a problem" improves.
- Aligns with the "Prom = system health" framing — both signals are
  now system-level not per-element.

Implementation: a helper `withBatchSpan(ctx, name, fn func(spanCtx))`
replaces the current `withSpan` callers in `internal/engine/scheduler.go`
and removes the per-element span creation in `pkg/extract/extractor.go`,
`pkg/judge/worker.go`, and `internal/engine/alert.go`. Per-element
work emits events (`span.AddEvent("listing.extracted", attrs)`) only
on the failure path.

---

### 5. Langfuse: close the prompt-iteration loop

#### Current state

Langfuse holds:

- Per-extraction generation events (input + output + token usage +
  cost)
- Per-judge generation events
- `extraction_self_confidence` scores (auto-pushed)
- `judge_alert_quality` scores (per IMPL-0019 Phase 5)
- `operator_dismissed` scores (per Phase 4; UI dismiss/restore actions)
- Datasets for regression testing (per Phase 6)

That's all the raw material for a prompt-iteration loop. What's
missing is the *loop* — the operator workflow that turns those scores
into prompt changes.

#### The loop, end-to-end

1. **Capture divergence.** Periodically (daily / weekly), find traces
   where judge and operator disagree:
   - Judge says "deal", operator dismissed → false positive
   - Judge says "noise", operator pinned → false negative
2. **Triage in the app UI.** A "Disagreements" page surfaces these
   listings with full context (title, attrs, baseline, both verdicts,
   judge reason). Operator decides:
   - **Operator was right** → add to regression dataset with correct
     label.
   - **Judge was right** → mark as operator error; revise the dismiss
     threshold or workflow.
   - **Both ambiguous** → label as "edge" and add to dataset.
3. **Iterate prompts.** When the dataset grows enough to show a
   pattern (e.g., judge consistently misses "wholesale lot
   ambiguity"), revise `pkg/extract/prompts.go` or
   `pkg/judge/judge_prompt.tmpl`, run `make test-regression` with
   `--langfuse-dataset-id`, observe accuracy delta, merge.
4. **A/B test prompts.** Two judge prompt variants, traffic-split
   50/50, compare agreement with operator-dismissal labels over a
   week. Promote the winner.

#### What the app needs to support this

- **`/disagreements` UI page** that joins judge scores ↔ operator
  dismissals from Postgres (both already persisted).
- **"Add to dataset" button** that posts a dataset item to Langfuse
  with the chosen label.
- **Per-prompt-version score panel** — show last 30 days of judge
  agreement rate stratified by prompt-template git SHA. (Requires
  threading the prompt template git SHA into the Langfuse generation
  metadata — small addition to the recorder.)
- **No automated prompt rewriting.** This loop is operator-driven; the
  data infrastructure exists to make the operator's job tractable, not
  to replace them.

#### Better filters at extraction time

Langfuse low-confidence scores are also a *pre-validation* signal.
Currently, low `extraction_self_confidence` shows up in dashboards but
doesn't affect ranking. The refactored UI can:

- Filter listings by self-confidence threshold.
- Surface "extraction needs review" alongside "deal alerts" so the
  operator can spot-check the LLM's worst calls.
- Auto-soft-deactivate listings where confidence < 0.3 (configurable),
  with operator override.

This is a downstream consumer of the existing IMPL-0019 work — no new
data needed.

---

### 6. How this changes INV-0002's scope

Mapping each INV-0002 finding to its post-refactor status:

| INV-0002 § | Issue | Post-refactor status |
|---|---|---|
| §1.1 | `pkg/extract` imports `internal/metrics`/`internal/version` | **Still applies** — refactor needs clean SDK boundary (see also INV-0004) |
| §1.2 | `internal/config` imports `pkg/observability/langfuse` | **Still applies** — same |
| §1.3 | `Store` interface 45+ methods | **More urgent** — search-index sync + UI endpoints both want narrow interfaces |
| §1.4 | Engine holds concrete eBay types | **Still applies** |
| §1.5 | Duplicate `RescoreAll` | **Still applies** |
| §1.6 | `judgeWorker` package-level var | **Still applies** |
| §1.7 | `ProcessAlerts` package-level function | **Still applies** |
| §1.8 | `internal/api/web` knows handler shapes | **Moot** — templ pages going away; new frontend has its own DTOs |
| §2.1 | "failed to" sweep | **Still applies** |
| §2.2 | `resp := &OutputType{}` | **Still applies** |
| §2.3 | `Get` prefix | **Still applies** |
| §2.4 | `slog.Default()` | **Still applies** |
| §2.5 | Error type names | **Still applies** |
| §2.6 | Redundant package prefix in errors | **Still applies** |
| §2.7 | Naked bool params | **Still applies** |
| §2.8 | `fmt.Sprintf("%d", ...)` | **Still applies** |
| §2.9 | Error chain broken in handlers | **Still applies** |
| §3.1 | N+1 `ListWatches` | **Still applies** |
| §3.2 | N+1 alert hydration | **Still applies** |
| §3.3 | `RecomputeAllBaselines` sequential | **More urgent** — search index sync needs the same shape |
| §3.4 | Unsized slice append | **Still applies** |
| §3.5 | `[]byte(content)` copy | **Still applies** |
| §3.6 | `bytes.Buffer` not pooled | **Still applies** |
| §3.7 | `strconv.Itoa` per request | **Still applies** |
| §3.8 | `AlertsFiredByWatch` unbounded cardinality | **Moot** — metric goes away when business-Prom is removed |
| §4.1 | PostgresStore test coverage 8.2% | **More urgent** — search-index sync correctness depends on store correctness |
| §4.2 | `serve.go` 677 LOC | **Still applies** |
| §4.3 | Logging inconsistency | **Still applies** |
| §4.4 | Scan-order brittleness | **More urgent** — search-index document construction reads every column |
| §4.5 | 8-touchpoint ComponentType | **More urgent** — search index needs a single source of truth for per-component-type fields |
| §4.6 | Config sprawl | **Still applies** |
| §4.7 | Manual orphan baseline cleanup | **Still applies** |
| §4.8 | Pre-classifier hooks hard-coded order | **Still applies** |
| §4.9 | `condition_norm` from title | **Still applies** |

**Summary of scope change:**

- **2 findings become moot** (§1.8, §3.8) — small saving but they
  remove two PRs from the queue.
- **5 findings become more urgent** (§1.3, §3.3, §4.1, §4.4, §4.5) —
  these now block search-index sync, not just code cleanliness.
- The rest still apply unchanged.

Net: **the refactor doesn't displace INV-0002 work — it raises the
stakes on the structural pieces.** Don't try to do the refactor before
INV-0002 Waves 1-4. The Store split, scan-order safety, and
ComponentType registry are *prerequisites* for a clean search-index
sync.

---

## Conclusion

**Answer:** Yes, the refactor is worth pursuing, and it makes ~5 of
INV-0002's findings more urgent while making 2 moot.

The current design accidentally couples business intelligence to a
metrics pipeline (Prometheus) and operator workflow to a debug surface
(templ+HTMX). Untangling those — business data lives in the app and
its search index; the UI is built for the operator's actual workflow;
Prometheus and OTel return to system-health duties — addresses the
"useless UI" feedback at the root rather than papering over it.

The Langfuse-driven prompt iteration loop is the strategic upside.
We've spent effort on Langfuse integration; right now nothing in the
operator's daily workflow consumes that investment. The new UI is
where that pays back.

**Risks:**

- **Scope creep.** A "real UI" can absorb arbitrary effort. The
  v1 scope should be: faceted search, dismiss/restore, judge-vs-
  operator disagreement page. Everything else is v2.
- **Meilisearch ops cost.** It's a new service to operate. Mitigate
  with Helm chart defaults that match current single-node Postgres
  shape and explicit "off by default" via `search.enabled=false`
  while we shake out.
- **Migration window.** The old `/alerts` HTMX page can stay for
  fallback during the new UI rollout — both can run side-by-side
  until the new UI proves out.
- **Frontend skill gap.** Owner is a Go engineer first. A small,
  conventional SPA in SvelteKit (or Vue 3) avoids React's surface
  area; if even that's a concern, an offshore/contract front-end pair
  for the v1 UI build is a faster path than self-teaching.

## Recommendation

**Phase 0 — Prerequisite cleanup (do first, in this order):**

Execute INV-0002 Waves 1-4 (boundary fixes, performance, naming,
testability + scan-order safety). These are not optional for the
refactor — the search index sync needs the cleaned-up store layer.

**Phase 1 — Search index foundation (parallel-track):**

1. Helm chart: optional Meilisearch StatefulSet (`search.enabled`).
2. New table `listing_search_documents` (denormalised projection) +
   `recompute_listing_search_documents` SQL function.
3. App-side dual-write from engine + on-demand full reindex via
   `POST /api/v1/search/reindex`.
4. Operator-facing `/api/v1/search/listings` endpoint that proxies
   Meilisearch queries (we control the contract; we own the API
   surface; Meilisearch is an implementation detail).

**Phase 2 — Frontend foundation:**

1. `/web` directory in monorepo with SvelteKit (or Vue 3).
2. `make web-build` → `web/dist/` embedded into the Go binary.
3. v1 pages: `/app/search` (faceted listing search), `/app/watches`
   (watch CRUD), `/app/disagreements` (judge-vs-operator).
4. Auth: defer until needed; protect via reverse-proxy auth in front.

**Phase 3 — Prom slim-down + OTel RED-style tightening:**

1. Mark business metrics deprecated (`spt_*_total{component_type, ...}`
   etc.) with a one-release deprecation window.
2. Rewrite OTel spans to batch granularity; replace per-element spans
   with events on failures only.
3. Update `tools/dashgen` to only ship system-health panels; move
   business panels into UI.

**Phase 4 — Langfuse iteration loop:**

1. Thread prompt-template git SHA into Langfuse generation metadata.
2. `/app/disagreements` page in the new UI.
3. "Add to dataset" button on listing detail panel.
4. Per-prompt-version accuracy panel.

**Phase 5 — Deprecate the old surface:**

1. Remove templ+HTMX `/alerts` page (after new UI has been in
   operator hands ≥2 weeks).
2. Remove deprecated Prometheus business metrics.
3. Remove `tools/dashgen` business panels.

**Effort estimate:**

- Phase 0: 6-10 PRs over 2-4 weeks (INV-0002 work)
- Phase 1: 3-4 PRs over 1-2 weeks
- Phase 2: 4-6 PRs over 2-4 weeks (SPA scaffolding + 3 pages)
- Phase 3: 2-3 PRs over 1 week
- Phase 4: 2-3 PRs over 1-2 weeks
- Phase 5: 1-2 PRs over a few days

**Total: 6-9 weeks of focused work**, parallelisable across the
backend (Phases 0/1/3) and frontend (Phase 2) tracks.

## References

- INV-0002 — Architectural review of pre-style-guide codebase (parent;
  this doc references each of its 24 findings)
- INV-0001 — IMPL-0019 post-merge code review findings
- INV-0004 — Evaluate sdk-booty-sh as replacement for in-house
  agentic SDK (sister investigation; the SDK move is orthogonal but
  intersects with Phase 0)
- DESIGN-0010 — Alert review UI (the surface this refactor replaces)
- DESIGN-0016 — Observability / IMPL-0019 (the metrics/Langfuse
  foundation the refactor builds on)
- `tools/dashgen/` — current Grafana panel generator (will shrink)
- Meilisearch — <https://www.meilisearch.com>
- SvelteKit — <https://kit.svelte.dev>
