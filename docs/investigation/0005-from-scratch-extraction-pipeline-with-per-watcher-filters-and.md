---
id: INV-0005
title: "From-scratch extraction pipeline with per-watcher filters and sdk-booty-sh"
status: In Progress
author: Donald Gifford
created: 2026-05-17
---
<!-- markdownlint-disable-file MD025 MD041 -->

# INV 0005: From-scratch extraction pipeline with per-watcher filters and sdk-booty-sh

**Status:** In Progress
**Author:** Donald Gifford
**Date:** 2026-05-17

<!--toc:start-->
- [Question](#question)
- [Hypothesis](#hypothesis)
- [Context](#context)
- [Approach](#approach)
- [Findings](#findings)
  - [1. Pipeline shape](#1-pipeline-shape)
  - [2. Pre-extraction stage 1: dedup by eBay item_id](#2-pre-extraction-stage-1-dedup-by-ebay-item_id)
  - [3. Pre-extraction stage 2: per-watcher static rules](#3-pre-extraction-stage-2-per-watcher-static-rules)
  - [4. Extraction stage: sdk-booty-sh as the agent runtime](#4-extraction-stage-sdk-booty-sh-as-the-agent-runtime)
  - [5. Post-extraction stage: optional LLM verifier per watcher](#5-post-extraction-stage-optional-llm-verifier-per-watcher)
  - [6. Schema strategy: coexistence with current data](#6-schema-strategy-coexistence-with-current-data)
  - [7. Validation strategy: re-ingest old listings via URL](#7-validation-strategy-re-ingest-old-listings-via-url)
  - [8. What is explicitly out of scope](#8-what-is-explicitly-out-of-scope)
  - [9. Downstream: rebuild alerting around an active pool](#9-downstream-rebuild-alerting-around-an-active-pool)
- [Open Questions](#open-questions)
- [Conclusion](#conclusion)
- [Recommendation](#recommendation)
- [References](#references)
<!--toc:end-->

## Question

If we rebuilt the extraction pipeline from scratch today — keeping the
existing `listings` table as the historical baseline — what would the
architecture look like?

Concretely:

- How do we stop re-extracting listings that have not materially changed
  since last cycle (price/quantity bumps don't need a fresh LLM pass)?
- How do we move "is this what the watcher asked for?" filtering out of
  the global `preclassify.go` and into per-watcher rules the operator
  can edit?
- Can `sdk-booty-sh` (the in-house agentic Go framework) replace the
  current `LLMBackend` abstraction with a proper agent/tool/skill model
  without losing the things we already rely on (token metrics, Langfuse
  traces, daily budget caps)?
- Where does an optional second-opinion LLM verifier fit, and what
  triggers it on/off?
- How do we validate the new pipeline produces equal-or-better
  extractions than the current one on the same eBay URLs, before
  cutting traffic over?

## Hypothesis

The current `pkg/extract/extractor.go::ClassifyAndExtract` flow conflates
five concerns into one call: classification, accessory rejection,
attribute extraction, normalisation, and validation. A clean rewrite as
a four-stage pipeline — **watcher → pre-extraction → extraction →
post-extraction** — gives us:

1. A deterministic short-circuit for already-known listings, which
   cuts daily LLM call volume noticeably (Open Q1 quantifies how much).
2. Per-watcher control over "is this listing actually what I asked
   for", instead of a global regex that has to work for all watchers
   simultaneously.
3. A real agent/tool surface (via `sdk-booty-sh`) instead of a
   JSON-blob-in-JSON-blob-out call. Budget enforcement, tool routing,
   and listener events become first-class.
4. An optional verifier slot, off by default per watcher, that we can
   evolve later (sample-based audit, low-confidence trigger, baseline
   mismatch trigger) without rewriting the pipeline again.

The cost of getting this wrong is a parallel pipeline that drifts from
the current one and never catches up. The cost of not doing it is more
preclassify hooks every time a new ComponentType lands (DESIGN-0012 GPU,
DESIGN-0015 workstation+desktop both added them), and continued
re-extraction of listings whose only change is price.

## Context

**The current pipeline accumulated, it was not designed.** Reading
`pkg/extract/extractor.go` and `pkg/extract/preclassify.go` together:

- `IsAccessoryOnly` short-circuits bare server-part listings to the
  `other` ComponentType. Global rule, no watcher awareness.
- `DetectSystemTypeFromTitle` overrides the LLM classifier for
  workstation/desktop chassis tokens. Global rule.
- `DetectGPUFamilyFromModel` overrides the LLM's family field for known
  canonical GPU models. Global rule.
- `systemServerLineDenylist` drops PowerEdge/ProLiant/UCS hallucinations
  on workstation `line` field. Global rule.
- Three pre-class hooks run in `ClassifyAndExtract` in a specific order
  (CLAUDE.md documents the ordering as load-bearing).

None of this is bad — it works — but each addition has been a patch on
top of a monolithic extractor that wasn't designed for per-watcher
behaviour. The R740xd-vs-R740xd-part problem is the canonical case: the
existing preclassify catches *some* parts via the accessory list, but a
"PSU **for** R740xd" or a "motherboard **from** R740xd" can slip through
because the listing also mentions the chassis model.

**Two adjacent investigations are already in flight:**

- INV-0002 / IMPL-0020 / IMPL-0021 are tactical cleanups of the current
  architecture (interface segregation, registry pattern for ComponentTypes,
  Uber Go style sweeps). They do not change the pipeline shape.
- INV-0003 is a parallel-layer rethink: app-centric metrics, Meilisearch
  read-path, real frontend. It does not touch extraction either.
- INV-0004 is evaluating `sdk-booty-sh` as a replacement for the in-house
  agentic SDK; that conclusion gates the extraction-stage rewrite here.

**`sdk-booty-sh` v1 just landed** (per its repo README): provider
abstraction (`pkg/llm`), Convo driver with concurrent tool dispatch and
per-conversation `Budget` (`pkg/agent`), `Skill` registry with
`MarkdownSkill` for frontmatter-driven tool resolution (`pkg/skill`),
and an `AsyncRecorder` tracing seam (`pkg/llmhttp`) that mirrors the
buffered Langfuse client pattern from IMPL-0019. This is the right
moment to ask whether our next extraction layer should sit on top of it.

**We have enough listing data to validate.** With the current
`listings` table populated from months of ingestion, we can pick a few
high-volume watchers and replay every listing URL through a new
pipeline. Comparing old vs new extractions on the same input is a
concrete go/no-go signal — much stronger than synthetic test cases or a
hand-picked golden dataset.

## Approach

This investigation produces a pipeline design proposal, not a working
prototype. Steps:

1. Sketch the four-stage pipeline (this doc, Findings §1).
2. Specify each stage's input/output contract and what runs in it
   (Findings §2 through §5).
3. Resolve schema coexistence — how do we write new pipeline output
   without polluting the live `listings` table during validation
   (Findings §6, Open Q5).
4. Specify the validation methodology and exit criteria (Findings §7,
   Open Q7, Q8).
5. Surface the open questions that block a DESIGN doc (Open Questions).
6. Once Open Questions are resolved, draft DESIGN-NNNN with concrete
   schema, interfaces, and migration path.
7. Draft a phased IMPL doc; validation harness ships as Phase 0 so we
   can compare old-vs-new from the very first feature merge.

**Out of scope for this investigation:** writing any code, picking
specific watcher rules, choosing the verifier model, or deciding
cutover dates. Those land in DESIGN / IMPL once the shape is agreed.

## Findings

### 1. Pipeline shape

```
Watcher tick (existing scheduler)
   │
   ▼
[eBay Browse search] → list of items (item_id, title, price, end_time, quantity, specifics)
   │
   ▼
Pre-extraction (deterministic, no LLM)
   ├─ Stage 1: dedup by eBay item_id
   │     ├─ exists in DB  →  UPDATE path (price/quantity/sold/end_time from eBay payload)
   │     └─ new listing   →  continue to Stage 2
   │
   └─ Stage 2: per-watcher static rules (regex + specifics heuristics)
         ├─ rejected   →  log + skip (no extraction, no DB write)
         └─ accepted   →  continue to Extraction
   │
   ▼
Extraction (LLM via sdk-booty-sh Convo + Skill registry)
   │  pulls structured attributes
   ▼
[INSERT new listing row, populate component_type + product_key + attrs]
   │
   ▼
Post-extraction (optional, per-watcher toggle)
   └─ LLM verifier Convo
       "Given the watcher's intent and the extracted attributes,
        is this listing a valid match?"
         ├─ rejected   →  flag (active=false) + record reason
         ├─ uncertain  →  flag for operator review surface
         └─ accepted   →  finalise (active=true)
   │
   ▼
End. Baselines / scoring / alerting unchanged downstream — out of scope.
```

The shape of stages 1 and 2 (cheap, deterministic) before stage 3
(expensive, LLM) is a standard "filter early" pattern. The shape of
stage 4 (optional, expensive verifier) is borrowed from the IMPL-0019
judge — same async / per-tick / budget-bounded shape, just applied at
the extraction layer instead of the alert layer.

### 2. Pre-extraction stage 1: dedup by eBay item_id

**Today.** The ingestion loop upserts listings by `(source, source_item_id)`
and re-enqueues every result of an eBay search into `extraction_queue`.
A listing whose price changed since yesterday goes through the full LLM
extraction again, producing the same `component_type` and `product_key`
99% of the time at the cost of an LLM call.

**Target.** Split the upsert at the top:

```
For each eBay result:
   if listings WHERE source_item_id = result.item_id EXISTS:
      UPDATE listings SET
         price = result.price,
         quantity = result.quantity,
         end_time = result.end_time,
         sold_at = NULL,                 -- eBay marks sold separately
         updated_at = NOW(),
         active = true                   -- reactivate if previously soft-deactivated
      WHERE id = ...
      -- DO NOT enqueue for extraction. Done.
   else:
      INSERT listings (...) ON CONFLICT DO NOTHING
      INSERT INTO extraction_queue (listing_id) ...
```

**Expected impact.** Open Q1 quantifies this from current data. Best
guess: 60-80% of daily ingest is re-occurrence of known listings, so
LLM call volume drops to ~20-40% of current. Counterfactual: if eBay's
search results turn over quickly (most results are new each tick), the
saving is smaller.

**Edge cases.**

- A listing whose title changes mid-cycle (seller edits) currently
  silently keeps the old extraction. New behaviour should detect title
  change and re-enqueue. The current upsert already overwrites title
  but doesn't trigger re-extraction — this is a latent bug.
- A listing that was soft-deactivated under DESIGN-0004 and then
  re-appears in search should reactivate without re-extracting.
- A listing that ended ("ended early" → end_time in past, quantity=0)
  needs a deactivate path even though it still appears in search for a
  short window.

**Alerting implication.** The upsert pattern also unblocks a current
alerting correctness bug. Today, when a listing is re-extracted /
re-classified, the sold and ended state from the eBay payload doesn't
propagate reliably — listings that have actually sold can keep firing
alerts because the re-extraction path doesn't always touch `sold_at`
or `active`. Splitting the write path means the sold/ended state
update is a *pure* upsert operation with no LLM in the critical path,
so a listing that goes alert-worthy → sold within one tick fires the
alert on tick N, then on tick N+1 the upsert marks it sold and the
alert leaves the active pool (per §9). This is the precondition for
the alerting redesign below — without a reliable sold-state update,
an "active pool of alerts" can't be trusted to reflect reality.

The same path also gives us an explicit decision point for
"re-alert on material update vs. silently update the pool entry"
(Open Q14). Today re-extraction can implicitly re-fire because the
alert evaluator runs on whatever it just re-classified; the new path
makes the difference between "this is a new listing, evaluate for
alert" and "this is a price update to a known listing, decide whether
the price change is alert-worthy on its own" structurally visible.

### 3. Pre-extraction stage 2: per-watcher static rules

**Today.** `preclassify.go` runs **once per listing**, with **zero**
awareness of which watcher pulled the listing in. The same accessory
regex evaluates "Dell R740xd Backplane" the same way regardless of
whether the watcher was for `dell r740xd 24 sff server` or
`dell r740xd parts`. The classifier prompt also tries to absorb watcher
intent ("we're looking for X") but only weakly.

**Target.** Per-watcher rule set, stored on the `watches` row, evaluated
deterministically before any LLM call:

```yaml
# example for a watch on "dell r740xd 24 sff server"
preclass:
  title_required:
    - 'r740xd'
  title_forbidden:
    - '\b(backplane|caddy|riser|fan|bezel|heatsink|rail|psu|gpu)\b'
    - '\bfor\s+r740xd\b'         # parts-for-R740xd
    - '\bfrom\s+r740xd\b'        # salvage parts
    - 'parts?\s+(only|kit|lot)'
  specifics_required:
    Form_Factor: ['Rack Server', '2U']
  specifics_forbidden:
    Most_Suitable_For: ['Replacement Part']
```

Three rule kinds in v1:

1. `title_required` — positive regex; listing must match at least one.
2. `title_forbidden` — negative regex; listing fails if any match.
3. `specifics_required` / `specifics_forbidden` — eBay item specifics
   (already pulled in via `DetectSystemTypeFromSpecifics`); listing
   must / must not have these key/value pairs.

**Engine.** Pure Go function `Evaluate(listing eBayResult, rules
WatcherRules) (decision PreClassDecision, reasons []string)`. No DSL,
no template language — just typed lists of regex and string-match
rules. Reasons surface in logs and the operator review UI.

**Operator UX.**

- Watch CRUD endpoints learn a `preclass` field (default: empty =
  permissive).
- `spt watches update <id> --preclass-file rules.yaml` patches the
  rules in place; next scheduler tick picks them up.
- Future web UI surfaces this as a form per watcher.

**Migration path.** The current global `IsAccessoryOnly` and friends
become the **default rule set** for new watches — operators can override
per watcher, but no watcher is unprotected. The existing global hooks
in `preclassify.go` get deleted once every watcher has explicit rules.

### 4. Extraction stage: sdk-booty-sh as the agent runtime

**Today.** `pkg/extract/extractor.go::LLMExtractor` calls
`LLMBackend.Generate` with a hand-built prompt + schema, parses the
JSON, runs normalisation, runs validation. No tool calling, no
multi-turn, no per-call budget. Token metrics emitted via
`recordTokens`; Langfuse trace via `langfuse_backend.go` decorator.

**What `sdk-booty-sh` provides:**

| Package         | Surface                                                                    |
| --------------- | -------------------------------------------------------------------------- |
| `pkg/llm`       | `Service`, `Request`, `Response`, `Message`, `Content`, `Tool`, `Usage`, `Capabilities`. String-backed enums; sentinel errors; `DeriveCost` pricing. |
| `pkg/llm/ant`   | Anthropic provider impl: SSE streaming, retry/backoff, prompt caching, vision. |
| `pkg/llmhttp`   | `Recorder` interface + `AsyncRecorder` (bounded buffer + drop counter) + `Transport` (HTTP round-trip observer). |
| `pkg/agent`     | `Convo` driver: concurrent tool dispatch, response-level `Budget` (`MaxTokens`, `MaxToolCalls`), `PermissionFn` policy seam, `Listener` events, `SubConvo` sharing usage. |
| `pkg/skill`     | `Skill` interface + `MarkdownSkill` (frontmatter-driven tool resolution) + `ProgrammaticSkill` + `Registry`. |
| `pkg/llm/llmtest` | `MockService`, `FakeRecorder`, `NewEchoTool`, `NewConvo` fixture. |

**Mapping onto our extraction:**

- Each ComponentType becomes a `Skill`. The `MarkdownSkill` frontmatter
  declares the available tools and embeds the extraction prompt — this
  lines up neatly with the IMPL-0021 registry pattern (each Spec already
  bundles prompt + validation + normalisation).
- Extraction is a `Convo` with the appropriate Skill loaded. Watcher
  metadata + listing data + item specifics go in as the initial user
  message.
- Tools we'd expose to the model:
  - `extract_attributes(json)` — the structured-output sink; the
    Convo terminates when this is called successfully.
  - `lookup_known_canonical(family, model)` — resolves canonical GPU
    families or server lines from a tiny lookup table; replaces the
    current `CanonicalizeGPUModel` / `DetectGPUFamilyFromModel` logic.
  - `mark_uncertain(reason)` — explicit "this isn't matching cleanly"
    signal; flags for operator review without producing a row.
- `Budget` is set per-call (`MaxTokens` from config, `MaxToolCalls` =
  small integer like 5). Replaces the worker-level cap with a
  conversation-level cap.
- `Listener` events feed our existing OTel spans + Langfuse traces.
  `AsyncRecorder` replaces the buffered Langfuse client from IMPL-0019
  (or, more likely, wraps it — same shape).

**Net wins vs current `LLMBackend`:**

- Tool calling is first-class. Today we stuff "structured output" into
  the JSON schema and parse it back out. With tools, the LLM can
  legitimately say "I don't have enough info" via `mark_uncertain`
  instead of producing best-guess junk.
- Per-conversation budget. Today's daily cap is a worker-level
  guardrail; sdk-booty-sh enforces per-Convo, so a runaway listing
  can't burn the day's budget.
- Skill registry is a clean per-ComponentType surface — `RAMSkill`,
  `ServerSkill`, etc. — that maps onto the IMPL-0021 registry pattern.

**Net costs:**

- Another dependency. We need to track sdk-booty-sh versions and roll
  forward with its API (still v1 — INV-0004 evaluating this).
- Re-wires the Langfuse plumbing. Today's `langfuse_backend.go`
  decorator wraps `LLMBackend.Generate`. sdk-booty-sh has its own
  `Recorder` seam, so we either wrap sdk-booty-sh under our existing
  Langfuse path, or adapt our Langfuse client to be a `Recorder`
  implementation. Open Q3.

### 5. Post-extraction stage: optional LLM verifier per watcher

**Today.** No verifier. The extracting LLM emits its own
`extraction_confidence`; we trust it.

**v1 target.** Per-watcher boolean toggle (default: off). When enabled,
a second Convo runs after extraction succeeds:

```text
System: You are reviewing a server-hardware listing extraction.
        The operator's watcher is looking for: <watcher.search_query>
        with component_type=<watcher.component_type>.
        Your job: decide pass / fail / uncertain.

User:   Listing title: <listing.title>
        Extracted component_type: <extracted.component_type>
        Extracted product_key: <extracted.product_key>
        Extracted attributes: <extracted.attrs JSON>
        eBay specifics: <listing.specifics JSON>

Tools:
  verify_pass()                       -- looks correct
  verify_fail(reason)                 -- definitely wrong
  verify_uncertain(reason)            -- can't tell from this data
```

Outcomes:

- `verify_pass` → listing inserted with `active=true`. Done.
- `verify_fail` → listing inserted with `active=false`, reason recorded
  on the row (new column or sidecar table — Open Q5 implication).
- `verify_uncertain` → listing inserted with `active=true` but flagged
  in a new `verifier_review` queue. Surfaces in the operator UI for
  manual decision.

**Why per-watcher toggle is enough for v1.** Per the user's reasoning:
we don't have data to set conditional triggers (low-confidence
threshold, baseline mismatch threshold) until the new pipeline has run
for a while. Starting with a flat on/off per watcher lets us turn it
on for the noisiest watchers first, gather data, then move to
conditional triggering in v2.

**v2+ (deferred, mentioned for shape).**

- Trigger conditions: extraction confidence below threshold, attribute
  values outside baseline range, sampled audit (e.g., 1 in 50).
- Multi-judge ensembling (same shape as the IMPL-0019 alert judge —
  multiple verdicts, agreement signal, Langfuse score for verifier
  accuracy).
- Operator labelling of verifier disagreements feeds the few-shot
  example set, same loop as `tools/judge-bootstrap` for the alert
  judge.

### 6. Schema strategy: coexistence with current data

The hardest non-pipeline question: where does the new pipeline's
output live during validation, and how does it relate to the existing
`listings` table?

**Constraint.** Dev and prod share the same Postgres (per CLAUDE.md and
the auto-memory note). We cannot do "test in dev, validate, then
deploy to prod" — both deployments hit the same data. Anything we
write to `listings` is visible to the prod app immediately.

**Three options:**

| Option | Pros | Cons |
| --- | --- | --- |
| Parallel `listings_v2` table | Clean separation; schema can evolve freely; diffing is a join | Doubles storage for the validation window; new pipeline can't reuse the dedup short-circuit (it'd dedup against the wrong table) |
| Version column on `listings` (`pipeline_version SMALLINT`) | One table; new pipeline can dedup against the same `source_item_id` set | Every existing query needs `WHERE pipeline_version = 1`; risk of forgetting and contaminating baselines / alerts |
| Shadow schema (`spt_v2.listings`) | Namespace isolation; explicit | Doubles the ops surface; harder to JOIN cross-schema for diffing |

**Tentative recommendation: parallel `listings_v2` table during
validation; rename + drop the old one at cutover.**

Trade-off accepted: the new pipeline's dedup step queries
`listings_v2.source_item_id` only — so during the validation window the
new pipeline never sees the existing data as "already known". For the
validation methodology this is correct: we **want** the new pipeline to
re-extract every URL we feed it so we can diff.

Open Q5 asks whether this is the right call, and whether parallel-table
is safe under the shared-DB constraint.

### 7. Validation strategy: re-ingest old listings via URL

**Methodology.**

1. Pick 2-3 high-volume, high-diversity watchers (Open Q7 — likely
   `dell r740xd 24 sff`, `dell precision t5810`, and one GPU watcher).
2. Query the current `listings` table for all rows matched to those
   watchers in the last 90 days.
3. For each listing: extract `source_item_id`, construct the canonical
   eBay item URL `https://www.ebay.com/itm/<item_id>`.
4. Feed those URLs into the new pipeline via a one-off ingest endpoint
   (new code — `POST /api/v2/ingest/by-url` or a CLI subcommand).
5. New pipeline runs all four stages and writes to `listings_v2`.
6. Diff query joining `listings` ↔ `listings_v2` on `source_item_id`,
   bucketing into:
   - `same` — both pipelines agree on `component_type` + `product_key`
   - `attrs_diverge` — same classification, different attributes
   - `class_diverge` — different `component_type`
   - `key_diverge` — same `component_type`, different `product_key`
   - `only_old` — old extracted, new rejected at preclass / verifier
   - `only_new` — new extracted, old had no row (shouldn't happen
     within the validation set but might surface bugs)
7. Spot-check the divergences manually; classify each as
   `new_better` / `old_better` / `equal_different`.

**Replay constraints.**

- Option A: re-fetch from live eBay. Costs Browse API quota
  (~5k/day cap, each `getItem` is one call). For 1000-listing
  validation that's two days of budget burn. Doable but uncomfortable.
- Option B: store raw eBay payload per listing going forward (new
  column `raw_payload JSONB`), then replay from storage. No quota
  cost, but only covers listings ingested **after** the raw-payload
  column lands.
- Option C: hybrid — start storing raw payloads now, run validation
  on the listings we've captured in the meantime.

Open Q6 picks one.

**Cutover criteria (Open Q8 refines).** Tentative bar:

- New pipeline matches old on `component_type` + `product_key` for at
  least 95% of listings in the validation set.
- For the 5% divergences, manual review shows new pipeline is
  at-least-as-good in at least 80% of cases.
- Pre-extraction Stage 2 (per-watcher rules) catches at least 90% of
  the parts-vs-system listings the old pipeline let through, measured
  against a small hand-labelled set.

### 8. What is explicitly out of scope

This investigation is about the **extraction pipeline first**. The
downstream alerting redesign (§9) is described here because it's
motivated by the dedup change in §2 and the design choices in §2 have
to leave room for it, but it does **not** ship in the
extraction-pipeline v1 — extraction must be validated against current
data before alerting changes layer on top.

Until the new pipeline is validated and cut over, the following are
preserved exactly as-is:

- Baselines (`price_baselines` table, `recompute_baseline` SQL function)
- Scoring (`pkg/scorer` weights and curves)
- Alerting engine, Discord notifier, summary mode, alert review UI
- Judge (`pkg/judge` for alert quality scoring)
- Quota management, scheduler timing, watch CRUD shape, eBay client

The premise: get the **input data** right first. Everything downstream
already works once it receives clean, correctly-classified listings —
and the alerting redesign in §9 becomes structurally simpler once the
upsert pattern in §2 makes sold-state propagation reliable.

### 9. Downstream: rebuild alerting around an active pool

This section sketches a downstream consequence of the extraction
pipeline rewrite, not v1 work. It's captured here because (a) the
upsert pattern in §2 is the precondition for it, and (b) the
extraction-pipeline design choices need to leave room for it without
having to rewrite them later.

**The current model: alerts as log entries.**

- An alert fires when an active listing's score crosses the watcher
  threshold → row inserted into `alerts` table with `notified=false`.
- The Discord notifier picks it up, sends, sets `notified=true`.
- The operator can dismiss via the review UI → `dismissed_at`
  populated.
- The row sits in the DB forever; it is a historical event log.

This model has two visible problems:

- **Stale alerts.** When a listing actually sells or ends, the alert
  row stays `notified=true, dismissed_at=NULL`. The review UI keeps
  showing it as an active alert even though the underlying listing is
  gone. The §2 bug makes this worse: re-extraction sometimes fails to
  mark the listing sold, so even queries that filter on
  `listings.active` don't catch it.
- **No "best deals right now" view.** The natural operator question
  ("what's the best server deal available right now?") becomes a join
  through alerts → listings filtered by sold-state guesses, which —
  per the bug above — don't always reflect reality.

**The proposed model: alerts as an active pool of listings.**

- The pool is conceptually a query: "listings currently active *and*
  above some watcher's alert threshold *and* not dismissed".
- When a listing's state changes (sold, ended, dropped below threshold
  on re-score, dismissed by operator), it leaves the pool with no extra
  bookkeeping — the query just stops returning it.
- The pool *is* the operator workflow surface. The UI default view
  (when INV-0003's frontend lands) shows pool contents sorted by score;
  the operator triages from there.

**Notifications layer on top of pool changes.**

- "Pool added" events can fire Discord notifications, but
  per-channel-filtered: rules say "Discord channel `#servers` wants
  pool-adds with `component_type=server`, `listing_age<2h`,
  `score>=80`". The current `summary_only` mode is a degenerate case
  of this (one channel, "all pool changes aggregated").
- "Pool removed" events can also notify (or not, per operator
  preference) to confirm a deal is gone — useful for "I was about to
  buy this" cases.
- This decouples *what's interesting right now* (the pool, the read
  model) from *what gets pushed to where* (per-channel filters, the
  notification policy).

**Storage shape (Open Q12).** The pool can be either:

- **Derived view** over current `listings` ⨯ `watches` ⨯ `scores` —
  recomputed on every read. Simplest; no consistency problems by
  construction; score recompute churns the pool but reads stay correct.
- **Persisted snapshot table** with explicit add / remove logic on
  state change — closer to the current `alerts` shape, but with
  proper lifecycle. More wiring; needs careful invalidation; risks
  divergence from live state.

Recommendation leans derived-view. The pool's content is *derived*
from current state by definition — persisting it risks the exact
divergence the redesign is meant to fix. The cost is read-time
recomputation; at our current scale (low thousands of active listings)
that's a cheap query. Open Q12 confirms under load.

**Why this works in the new pipeline but not the old.**

- Pre-extraction Stage 2 (per-watcher rules) means listings that
  shouldn't have been considered for a given watcher's pool never
  enter it — fewer false-positive entries to begin with.
- Post-extraction verifier (when enabled per watcher) catches
  "extraction was technically right but it's not what the operator
  wanted" before the listing reaches the pool.
- Dedup-upsert (§2) means the sold-state update happens reliably and
  doesn't depend on re-extraction succeeding, so pool exit on
  sold/ended is correct.
- The pool is a read-side concept; alerts are the read of "interesting
  active listings". This matches the operator's mental model and
  removes the historical-log confusion entirely.

**v1 scope: zero code in this area.** The full alerting redesign
lands in its own DESIGN doc after the extraction pipeline has been
validated. This investigation only commits to ensuring the extraction
pipeline doesn't make the redesign harder — specifically:

1. The §2 upsert pattern lands with reliable sold-state propagation.
2. The §3 per-watcher rules don't bake in assumptions about how
   alerts get fired (they decide whether to *extract*, not whether
   to *alert*).
3. The §5 verifier outcome model leaves room for "this listing should
   not be in any pool" as a possible verdict (current sketch already
   does — `verify_fail` → `active=false`).

## Open Questions

- **Q1 — Dedup hit rate.** What percent of daily ingestion volume is
  already known? Need to query current data
  (`SELECT count(*) FILTER (WHERE updated_at > created_at) / count(*)
   FROM listings WHERE updated_at > NOW() - INTERVAL '7 days';`) to
  size the LLM cost reduction claim. Without this number the
  "noticeable savings" claim is hand-waving.

- **Q2 — Per-watcher rule storage shape.** YAML in a `JSONB` column on
  `watches`? A typed Go struct with explicit fields? A small DSL?
  Recommendation leans typed-Go-struct serialised to JSONB — explicit,
  no parser to build, easy to validate at write time.

- **Q3 — sdk-booty-sh adoption shape.** Two paths:
  - **(a) Wholesale replace** `LLMBackend` with sdk-booty-sh primitives;
    the existing `OllamaBackend` / `AnthropicBackend` either gain
    sdk-booty-sh `Service` adapters or get deleted.
  - **(b) Wrap** sdk-booty-sh under the existing `LLMBackend`
    interface during cutover; collapse the interface later.
  - INV-0004's conclusion gates this. The risk with (a) is a bigger
    bang; the risk with (b) is the wrapper smooths out the things
    that make sdk-booty-sh worth adopting (tools, budget, listeners).

- **Q4 — Post-extraction verifier model + cost.** Decided per the
  conversation: v1 ships per-watcher on/off only. Open sub-question:
  do we use the same model for verification as for extraction, or a
  smaller/cheaper model for the verifier? Cheaper model risks
  rubber-stamp passes; same model risks shared blind spots.

- **Q5 — Schema coexistence.** Recommendation is parallel `listings_v2`
  table. Confirm under the shared-DB constraint that this is safe (no
  cron / view / report accidentally reads `listings_v2` thinking it's
  the production table). Alternatives: version column, shadow schema.

- **Q6 — Raw payload storage.** Do we start storing eBay's raw response
  payload per listing now (`raw_payload JSONB` column on `listings`)
  to enable cheap replay, or do we accept the Browse API quota burn for
  the validation window? Storing payloads has downstream value
  beyond this investigation (debugging extraction failures, rebuilding
  baselines from history).

- **Q7 — Validation watcher choice.** Need 2-3 watchers with: enough
  historical data (≥500 listings each over 90 days), diverse
  ComponentTypes (at least one server, one GPU or workstation, one
  RAM/drive/other), and a known parts-vs-system noise problem the
  current pipeline gets wrong. Tentative picks above; needs
  confirmation against current data.

- **Q8 — Cutover criteria.** Tentative bar above (95% classification
  agreement, 80% of divergences in new-pipeline's favour, 90% of
  parts-vs-system catches). Need to confirm these numbers are
  defensible — too low and we ship a regression; too high and we
  never cut over.

- **Q9 — Per-watcher rule editing UX.** API only for v1 (matches
  current watch CRUD pattern), CLI subcommand
  (`spt watches preclass <id> --file rules.yaml`), or wait for the
  INV-0003 web UI? API + CLI is the natural path; web UI lands when
  INV-0003 conclusions allow.

- **Q10 — Relationship to IMPL-0020 / IMPL-0021.** IMPL-0021's
  ComponentType registry pattern is the right substrate for the new
  pipeline's Skills. Does the registry land first (Phase 1+2 of
  IMPL-0021) and then the new pipeline builds on it, or does the
  new pipeline subsume IMPL-0021's Phase 3+ work because those
  per-ComponentType `Spec`s become sdk-booty-sh `Skill`s anyway?

- **Q11 — Title-change re-extraction trigger.** Edge case from §2: a
  listing whose title changes mid-cycle (seller edits) currently keeps
  the old extraction. New pipeline should detect title diff and
  re-enqueue. How big is this in practice — do sellers edit titles
  often enough to matter, or is this a "nice to have" we can defer?

- **Q12 — Active alert pool storage shape.** Derived view over
  `listings` ⨯ `watches` ⨯ `scores`, recomputed on every read? Or
  persisted snapshot table with explicit add / remove on state change?
  Recommendation leans derived-view (consistency by construction);
  needs validation that read-time recomputation cost is acceptable at
  current and projected scale.

- **Q13 — Notification filter shape.** Per-channel YAML rules
  (`#servers` Discord channel wants pool-adds with
  `component_type=server`, `listing_age<2h`, `score>=80`)? Or
  simpler one-webhook-receives-everything with summary-mode
  aggregation? The current `summary_only` mode already approximates
  this — do we generalise it into a proper per-channel filter engine
  or keep it as one toggle?

- **Q14 — Alert re-fire policy on material update.** When a listing's
  price drops materially after the first alert, do we re-alert (the
  price improved, operator may want to know) or just update the pool
  entry (it's the same listing)? Per the user's framing, this becomes
  an explicit decision point the new pipeline enables. Likely answer:
  per-watcher or per-channel policy ("re-alert if new score is X
  points above previous high"), but needs concrete rules.

## Conclusion

Pending. The pipeline shape itself is well-formed and the trade-offs are
visible. The unknowns are mostly operational details — rule storage
format, sdk-booty-sh wrapping shape, schema coexistence under the
shared-DB constraint, and the validation methodology's exit criteria —
not pipeline architecture questions.

This investigation is **In Progress** until the 14 Open Questions are
resolved with the user.

## Recommendation

1. Resolve Open Questions Q1, Q5, Q6 first — they affect the very next
   commits (Q1 needs a SQL query against current data, Q5 + Q6 add
   columns / tables that have to land early).
2. Resolve Q2, Q3, Q4, Q7, Q8 next — these shape the DESIGN doc but
   don't change the pipeline scaffold.
3. Resolve Q9, Q10, Q11 — scope edges for the extraction pipeline.
4. Resolve Q12, Q13, Q14 last (or defer to a follow-up INV) — they
   relate to the §9 alerting redesign, which is downstream of v1 and
   only needs answers when that DESIGN doc is drafted.
5. Draft `DESIGN-NNNN — Four-stage extraction pipeline` once
   extraction-related Open Questions are resolved.
6. Draft `IMPL-NNNN` with the validation harness as Phase 0, the
   pipeline as Phase 1-4 (one per stage), and the cutover + old
   pipeline retirement as Phase 5.
7. Once the extraction pipeline is validated and cut over, draft a
   follow-up `DESIGN-NNNN — Active alert pool` covering §9 in detail.
8. Revisit IMPL-0020 / IMPL-0021 scope: any of their Important
   findings that touch `pkg/extract/extractor.go` or
   `pkg/extract/preclassify.go` should be marked "deferred —
   superseded by INV-0005" rather than done twice.

## References

- DESIGN-0002 — current LLM extraction pipeline (the thing being
  rewritten)
- DESIGN-0004 — inactive listings lifecycle (the `active` soft-deactivation
  pattern the new pipeline preserves)
- DESIGN-0008 / 0009 / 0010 — Discord notifier, summary mode, alert
  review UI (the surface the §9 redesign replaces)
- DESIGN-0011 — score curve recalibration (P25/P50/P75 → 70/30/10; the
  threshold reference the §9 pool inclusion query uses)
- DESIGN-0012 — GPU support (added preclassify hooks)
- DESIGN-0015 — workstation/desktop support (added more preclassify
  hooks; canonical example of the per-ComponentType complexity creep
  this investigation is trying to bound)
- IMPL-0015 — notification + alert review (the current alerting
  pipeline the §9 redesign restructures)
- IMPL-0019 — observability + judge (the post-extraction verifier
  borrows its shape from the alert judge)
- INV-0001 — IMPL-0019 post-merge findings (BufferMetrics adapter
  pattern, lifecycle gotchas — same lessons apply to wiring
  sdk-booty-sh's `Recorder` to our metrics)
- INV-0002 — architectural review (overlap with IMPL-0020 cleanup
  scope; this investigation may invalidate some of it)
- INV-0003 — app-centric refactor (parallel-layer rethink; the web UI
  rules-editing work depends on it)
- INV-0004 — sdk-booty-sh evaluation (gates extraction stage choice)
- IMPL-0020 — INV-0002 remediation (tactical cleanup; may be partially
  subsumed)
- IMPL-0021 — ComponentType registry pattern (the Skills surface for
  sdk-booty-sh maps onto this)
- <https://github.com/donaldgifford/sdk-booty-sh> — Agentic Go SDK
  framework (v1 MVP landed)
