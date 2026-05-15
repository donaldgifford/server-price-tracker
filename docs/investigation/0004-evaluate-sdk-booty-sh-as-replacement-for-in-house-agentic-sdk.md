---
id: INV-0004
title: "Evaluate sdk-booty-sh as replacement for in-house agentic SDK"
status: In Progress
author: Donald Gifford
created: 2026-05-15
---
<!-- markdownlint-disable-file MD025 MD041 -->

# INV 0004: Evaluate sdk-booty-sh as replacement for in-house agentic SDK

**Status:** In Progress
**Author:** Donald Gifford
**Date:** 2026-05-15

<!--toc:start-->
- [Question](#question)
- [Hypothesis](#hypothesis)
- [Context](#context)
- [Approach](#approach)
- [Findings](#findings)
  - [1. What sdk-booty-sh ships today](#1-what-sdk-booty-sh-ships-today)
  - [2. What our codebase has (the "in-house agentic SDK")](#2-what-our-codebase-has-the-in-house-agentic-sdk)
    - [pkg/extract](#pkgextract)
    - [pkg/judge](#pkgjudge)
    - [pkg/observability/langfuse](#pkgobservabilitylangfuse)
    - [tools/judge-bootstrap, tools/dataset-bootstrap, tools/dataset-upload, tools/regression-runner](#toolsjudge-bootstrap-toolsdataset-bootstrap-toolsdataset-upload-toolsregression-runner)
  - [3. Feature parity gap](#3-feature-parity-gap)
  - [4. Concerns and suggestions for sdk-booty-sh](#4-concerns-and-suggestions-for-sdk-booty-sh)
    - [Concerns](#concerns)
    - [Suggestions (would file as upstream issues / PRs)](#suggestions-would-file-as-upstream-issues--prs)
  - [5. What the migration looks like](#5-what-the-migration-looks-like)
    - [Step 1 — Upstream contributions (sdk-booty-sh PRs)](#step-1--upstream-contributions-sdk-booty-sh-prs)
    - [Step 2 — Tracker-side migration (server-price-tracker PRs)](#step-2--tracker-side-migration-server-price-tracker-prs)
    - [Total LOC delta (estimated)](#total-loc-delta-estimated)
    - [Risk](#risk)
  - [6. Pros and cons](#6-pros-and-cons)
    - [Pros](#pros)
    - [Cons](#cons)
    - [Overall gain](#overall-gain)
- [Conclusion](#conclusion)
- [Recommendation](#recommendation)
- [References](#references)
<!--toc:end-->

## Question

We currently maintain an in-house agentic SDK inside this repo —
`pkg/extract` (LLMBackend interface + Anthropic/Ollama/OpenAICompat
providers + Extractor orchestrator), `pkg/judge` (LLM-as-judge worker),
and `pkg/observability/langfuse` (in-house Langfuse client). All three
were built MVP-style and have accreted lessons that INV-0001 hardened.

`github.com/donaldgifford/sdk-booty-sh` is an Agentic Go SDK framework
by the same owner. v1 MVP has landed (Phases 0/A/B/C of its IMPL-0001).

**Should server-price-tracker migrate from its in-house SDK to
sdk-booty-sh?** Specifically:

1. What features does sdk-booty-sh provide today vs. what we use?
2. What gaps must be closed (in the SDK or in our code) before
   migration is feasible?
3. What does the migration look like (PRs, LOC delta, risk)?
4. What are the pros, cons, and overall gains?

## Hypothesis

Both codebases were built by the same owner with overlapping intent.
INV-0003 (provider-abstraction-prototype) in sdk-booty-sh explicitly
audited server-price-tracker as input. The natural endpoint is
consolidation: the SDK becomes the canonical provider abstraction;
the tracker becomes its first real consumer; both stop maintaining
parallel implementations.

The friction is **timing** — sdk-booty-sh's deferred Phases D (Ollama,
OpenAI-compat providers) and E (eval/judge) cover exactly what we
*use* most heavily (Ollama is our default backend; judge is IMPL-0019's
centrepiece). Migration is gated on closing those gaps, either by
upstreaming the in-house code or by waiting.

**Hypothesis:** The migration is worth doing, but it should be
sequenced as **upstream contributions first, consumer migration
second**. The cost is contributing Ollama + OpenAI-compat providers
to the SDK (~1,500 LOC of provider code we already have, plus tests
restructured to the SDK's `pkg/llm/llmtest` patterns). The gain is
that those same providers, plus the SDK's better Recorder/Listener
separation and Capabilities-driven gating, become available to every
future agent we build (the ADK product layer mentioned in
sdk-booty-sh's RFC-0001, the AWS triage bot in RFC-0002, anything
else that wants an LLM-with-tools loop).

## Context

**Triggered by:**

- INV-0003 (app-centric refactor) is going to touch `pkg/extract` and
  `pkg/judge` regardless; this is a natural moment to ask whether
  those packages should be ours at all.
- INV-0001 (post-merge code review) established load-bearing
  precedents for our in-house Langfuse client. Migrating after those
  lessons are baked in is safer than migrating before.
- sdk-booty-sh's INV-0003 ("provider-abstraction-prototype") cites
  server-price-tracker as a reference codebase — the relationship
  goes both ways and consolidation is the explicit endgame.

This investigation is **deciding-whether-to**, not **planning-how**.
A concrete migration plan would be a follow-up PLAN doc.

## Approach

1. **Read sdk-booty-sh's surface area** — README, CLAUDE.md, package
   docs, the `pkg/llm`, `pkg/agent`, `pkg/llmhttp`, `pkg/skill`
   public APIs.
2. **Inventory what we use** in `pkg/extract`, `pkg/judge`, and
   `pkg/observability/langfuse` — interfaces, types, hidden contracts.
3. **Map each thing we use → what the SDK provides** (or what gap
   exists).
4. **Estimate migration cost** by counting LOC affected on both sides.
5. **Stress-test the decision** by listing what could go wrong.

The sdk-booty-sh review used `gh api repos/donaldgifford/sdk-booty-sh/
contents/...` and direct fetches of `pkg/llm/llm.go`, `pkg/agent/
convo.go`, plus the README and CLAUDE.md.

## Findings

### 1. What sdk-booty-sh ships today

**v1 MVP, Phases 0/A/B/C complete. Phases D (providers), E (eval/
judge), F (MCP) deferred.**

| Package | Surface |
|---|---|
| `pkg/llm` | `Service` interface (`Do(ctx, *Request) (*Response, error)`, `TokenContextWindow()`, `MaxImageDimension()`, `Capabilities()`). Types: `Request`, `Response`, `Message`, `Content`, `Tool`, `ToolChoice`, `Usage`, `SystemContent`, `Capabilities`. String-backed enums for `MessageRole`, `ContentType`, `StopReason`, `ErrorType`, `Tool.Kind`. Sentinel errors: `ErrToolsUnsupported`, `ErrVisionUnsupported`, `ErrStreamingUnsupported`. `DeriveCost` pricing helper. |
| `pkg/llmhttp` | Tracing seam: `Recorder` interface (`OnRequest/OnResponse/OnError`), `RequestInfo/ResponseInfo/ErrorInfo` heavy-pointer payloads, `NoopRecorder`, `AsyncRecorder` (bounded buffer + drop counter), `Transport` (`http.RoundTripper` observer), context helpers (`WithConversationID`, `WithProvider`). `ResolveRecorder(nil) → NoopRecorder{}` so dispatch sites are nil-check-free. |
| `pkg/llm/ant` | Anthropic provider. Public `Service` struct, `internal/` impl split for request building, response parsing, SSE handling, retry/backoff with `errors.Join`. Supports SSE streaming, prompt caching, vision. Integration test gated behind `//go:build integration`. |
| `pkg/llm/llmtest` | Public test API: `MockService` (scripted responses), `FakeRecorder` (buffered events + assertions), `NewEchoTool`, `NewConvo` fixture with option helpers (`WithTools`, `WithBudget`, `WithListener`, `WithPermissionFn`, `WithCapabilities`, `WithSystemPrompt`). |
| `pkg/agent` | Conversation driver. `Convo` (sequential-use, internally concurrent tool dispatch), `SendMessage`, `SendUserTextMessage`, `Budget` / `BudgetExceededError` / `CumulativeUsage` (response-granularity enforcement), `Listener` (loop-level events: `OnRequest`/`OnResponse`/`OnToolCall`/`OnToolResult`), `PermissionFn` (policy seam for tool calls), `SubConvo` / `SubConvoWithHistory` (sharing parent usage), `CancelToolUse`. Capability checks before request dispatch. |
| `pkg/skill` | `Skill` interface, `MarkdownSkill` (with YAML frontmatter via `gopkg.in/yaml.v3`, loaded via `LoadMarkdown(fs.FS, path, opts...)`), `ProgrammaticSkill`, `Registry` for frontmatter tool resolution. |

**Conventions enforced:**

- Heavy `Info` structs as pointer params (gocritic `hugeParam`).
- `Recorder` (HTTP/provider seam) ≠ `Listener` (loop seam) — two
  layers, don't collapse.
- `AsyncRecorder` snapshots on enqueue (caller-mutates-after-call
  safe).
- Provider packages: public surface + `internal/` impl split.
- String-backed enums everywhere.
- Coverage ≥ 80% on `pkg/llm`, `pkg/agent`, `pkg/skill`, `pkg/llmhttp`,
  `pkg/llm/ant`.
- `.golangci.yml` enforces Uber Go Style Guide (the same standard we
  recently formalised in INV-0002).

**Deferred / not yet shipping:**

- Phase D providers: **Copilot SDK, OpenAI-compat, Ollama**.
- Phase E: **eval/judge** subsystem.
- Phase F: **MCP** client.
- Streaming iterator (`Service.Stream(ctx, req) iter.Seq2[StreamDelta, error]`):
  scoped in INV-0005 there; must land before Copilot impl.

### 2. What our codebase has (the "in-house agentic SDK")

Three packages are in scope for the comparison:

#### `pkg/extract`

| Surface | What it does |
|---|---|
| `LLMBackend` interface | `Generate(ctx, prompt) (Response, error)` — single round-trip, no tool loop |
| `OllamaBackend` | HTTP client for local Ollama; structured-JSON grammar mode; token usage parsed from `/api/generate` response |
| `AnthropicBackend` | HTTP client for Claude API; strips markdown code fences; token usage from `usage` block |
| `OpenAICompatBackend` | HTTP client for OpenAI-compat servers (OpenRouter, vLLM, etc.) |
| `Extractor` | Two-pass orchestrator: pre-classifier hooks → classify → extract → normalize → validate |
| Pre-classification | `IsAccessoryOnly` regex, `DetectSystemTypeFromTitle`, `DetectSystemTypeFromSpecifics` |
| Normalisation | `NormalizeExtraction`, `NormalizeRAMSpeed`, `CanonicalizeGPUModel`, `NormalizeSystemExtraction`, etc. — repair common LLM mistakes before validation |
| Validation | `ValidateExtraction` — enum/range checks per ComponentType |
| Product key generation | Per-ComponentType key derivation for baseline grouping |
| Langfuse decorator | Wraps any `LLMBackend` to push generation events + cost via `Recorder`-equivalent pattern |
| Cost telemetry | Per-model rate table → `extraction_cost_usd_total` Prom counter |

LOC: ~3,500 in `pkg/extract` (including tests, normalisers, prompt
templates).

#### `pkg/judge`

| Surface | What it does |
|---|---|
| `Judge` interface | `Evaluate(ctx, AlertContext) (Score, error)` |
| `LLMJudge` | Prompt template + few-shot `examples.json` cold-start |
| `Worker` | Polls unjudged alerts; daily-budget enforcement with mid-batch rechecks; persists to Postgres + Langfuse |
| `Score` ↔ `Verdict` | Domain types: deal/edge/noise verdict with [0,1] score |
| Untrusted-prompt sanitisation | `sanitizeUntrusted` (strip control chars + truncate + delimiters) |
| `JudgeStore` | Postgres CRUD for `judge_scores` table (migration 013) |

LOC: ~1,200 in `pkg/judge`.

#### `pkg/observability/langfuse`

| Surface | What it does |
|---|---|
| `HTTPClient` | In-house Langfuse HTTP client; goes through `/api/public/ingestion` (not deprecated endpoints) |
| `BufferedClient` | Bounded async wrapper; drop-newest semantics; `BufferMetrics` interface for `internal/metrics` adapter pattern |
| Generation / Trace / Score event types | `generation-create`, `trace-create`, `score-create` event bodies |
| Session context | `WithSessionID` / `SessionIDFromContext` — thread-safe session propagation |
| `MockClient` | Test double |
| Datasets + Dataset Run Items | `CreateDatasetItem` (idempotent on ID), `CreateDatasetRunItem` |
| Cost derivation | `ModelCost` map → `CostUSD` per generation |

LOC: ~1,800 in `pkg/observability/langfuse`.

**Total in-house SDK surface: ~6,500 LOC.**

#### `tools/judge-bootstrap`, `tools/dataset-bootstrap`, `tools/dataset-upload`, `tools/regression-runner`

LLM-eval-adjacent CLIs. ~1,500 LOC total. These would map to a Phase
E (eval/judge) in sdk-booty-sh if such a phase existed.

### 3. Feature parity gap

Map of what we use today vs. what the SDK provides:

| Capability | Our SDK | sdk-booty-sh | Gap |
|---|---|---|---|
| LLM provider abstraction | `LLMBackend` interface | `llm.Service` interface | Equivalent; SDK's shape is richer (Capabilities, MaxImageDimension, TokenContextWindow) |
| Anthropic provider | ✓ | ✓ (`pkg/llm/ant`) | **SDK is better** — SSE streaming, prompt caching, vision, integration test gating, internal split |
| **Ollama provider** | ✓ (we use as default) | ✗ deferred Phase D | **MUST CLOSE** — upstream contribution |
| **OpenAI-compat provider** | ✓ | ✗ deferred Phase D | **MUST CLOSE** — upstream contribution |
| Tool-use loop | ✗ (not used) | ✓ (`pkg/agent.Convo`) | Unused for our extraction work; nice-to-have for future "research a listing" mode |
| Permission seam | ✗ | ✓ (`PermissionFn`) | Unused for our extraction work |
| Capability-mismatch gate | ✗ | ✓ (`ErrToolsUnsupported`, etc.) | **SDK is better** — we've been bitten by OpenAI silently returning wrong shape |
| Vision | ✗ | ✓ | Not used for listings today; eBay images would unlock this someday |
| Streaming callback | ✗ | ✓ (callback now, iterator scoped) | Not used today |
| Recorder/tracing seam | ad-hoc decorator on `LLMBackend` | ✓ (`Recorder` interface; `Noop`/`Async`/`Transport`) | **SDK is better** — explicit two-layer split |
| Listener seam (loop events) | N/A | ✓ (`Listener`) | Unused for our extraction work |
| Budget enforcement | ✓ (`Worker` daily $) | ✓ (response-token `Budget`) | **Different scopes** — both should coexist; per-call budgets live in SDK, per-day budgets live in consumer (`Worker`) |
| Cost derivation | per-model rate table | `DeriveCost` | Equivalent; both pluggable |
| Prompt caching (Anthropic) | ✗ | ✓ | Nice-to-have; would reduce extraction cost when prompts get large |
| Langfuse adapter | in-house client | ✗ | **MUST CLOSE** — write a `Recorder` adapter (could land upstream as `pkg/llmhttp/langfuse` or stay in tracker) |
| Untrusted-prompt sanitisation | ✓ (judge prompts) | ✗ | Consumer concern; stays in `pkg/judge` |
| LLM judge | ✓ (`pkg/judge`) | ✗ deferred Phase E | Stays in tracker; consumes SDK's `llm.Service` |
| Dataset / regression runner | ✓ (3 CLIs) | ✗ deferred Phase E | Stays in tracker; could become a future SDK phase E contribution |
| Test helpers | ad-hoc mocks per package | ✓ (`pkg/llm/llmtest`) | **SDK is better** — `MockService`, `FakeRecorder`, `NewConvo` fixture, `NewEchoTool` |
| Skill loader (MarkdownSkill) | ✗ | ✓ (`pkg/skill`) | Interesting: could replace our per-Go-file prompt templates with markdown files |
| MCP client | ✗ | ✗ deferred Phase F | Both blank — not a current need |

**Bottom line on parity:**

- **3 hard blockers** — Ollama provider, OpenAI-compat provider,
  Langfuse adapter. Each is an upstream contribution we'd write
  (Ollama and OpenAI-compat fit Phase D; Langfuse is a Recorder
  adapter sized at ~400 LOC).
- **0 soft blockers** — everything else is either present in the SDK
  in better shape, or genuinely not needed for this consumer.
- **Our judge worker, prompt orchestration, normalisers, validation,
  and ComponentType-specific logic all stay in the tracker.** The
  SDK provides the *provider* layer; we keep the *domain* layer.

### 4. Concerns and suggestions for sdk-booty-sh

Observations that would inform an upstream contribution:

#### Concerns

1. **License: "All rights reserved" / private.** Fine for same-owner
   private use; blocker if server-price-tracker ever goes
   open-source. Worth pinning down the licensing intent before we
   commit infrastructure to the dependency.
2. **Phase D timing risk.** The SDK's roadmap defers Ollama and
   OpenAI-compat indefinitely. We'd need to either accept ownership
   of those provider phases or land them as paid-down debt.
3. **Streaming iterator surface (INV-0005) hasn't landed.** Not a
   blocker for us — we don't stream extraction responses. But if we
   ever add streaming, the iterator-vs-callback decision is theirs to
   make and we'd ride along.
4. **Convo / tool-loop surface is unused for our extraction path.**
   We'd import `pkg/agent` but never call `SendMessage`. That's fine —
   `pkg/llm.Service` can be used directly without `pkg/agent.Convo`.
   Worth confirming the README example doesn't imply otherwise (it
   doesn't — the SDK lets you call `Service.Do` without ever
   constructing a Convo).
5. **Capabilities surface is statically declared by provider.** What
   happens when an OpenAI-compat endpoint claims tool support but the
   underlying model (e.g., Llama 3 via OpenRouter) doesn't actually
   support tools? Our `OpenAICompatBackend` has been bitten by this.
   Probably needs a per-model override mechanism in the provider
   config.

#### Suggestions (would file as upstream issues / PRs)

1. **`pkg/llm/ollama` should be Phase D's first contribution** if we
   migrate — that's the unblocker for us *and* for any other
   consumer running local models.
2. **`pkg/llmhttp/langfuse` recorder adapter.** Either upstream as a
   first-party adapter, or document the consumer-side pattern. Our
   in-house Langfuse client has hard-won precedents (drop-newest
   buffer, stopCh-driven lifecycle, `/api/public/ingestion` not
   deprecated endpoints, session context propagation, untrusted-
   prompt sanitisation in prompt templates) — all worth preserving in
   the adapter.
3. **Per-model `Capabilities` override.** A `WithCapabilityOverrides`
   functional option on provider constructors would let us turn off
   `Tools=true` when an OpenAI-compat model doesn't actually support
   them. Today this kind of mismatch fails late, after the model
   returns garbage.
4. **Untrusted-content delimiter convention.** Document the
   `<<<UNTRUSTED>>>...<<<END_UNTRUSTED>>>` pattern (INV-0001 §4) as a
   recommended helper, possibly `llm.SanitizeUntrusted(text, maxRunes
   int)`. Stays in `pkg/llm` because it's an LLM concern, not a
   tracker concern.
5. **Cost telemetry surface.** `DeriveCost` is good; consider also
   exposing a `Generation.CostBreakdown()` method that returns
   per-input/output/cache-write/cache-read line items. Helps
   downstream cost-attribution dashboards.
6. **A formal Phase E (eval/judge)** would absorb our
   `tools/regression-runner`, `tools/dataset-bootstrap`,
   `tools/dataset-upload`, and `pkg/judge`. Worth co-designing if we
   ever want other agents to use the same eval shape.

### 5. What the migration looks like

Assuming Phases D (Ollama, OpenAI-compat) and a Langfuse Recorder
adapter are in place:

#### Step 1 — Upstream contributions (sdk-booty-sh PRs)

- **PR-A**: `pkg/llm/ollama` — port our `OllamaBackend` to the SDK's
  shape (public surface + internal split + integration test gating).
  ~500-800 LOC + tests.
- **PR-B**: `pkg/llm/openai` — port our `OpenAICompatBackend`. Similar
  shape and effort.
- **PR-C**: `pkg/llmhttp/langfuse` (or in-tracker) — Recorder adapter
  preserving the lessons in INV-0001 §"Langfuse writes go through
  /api/public/ingestion" etc. ~400 LOC + tests.

#### Step 2 — Tracker-side migration (server-price-tracker PRs)

- **PR-1**: Replace `pkg/extract.LLMBackend` with `llm.Service`. Adapt
  `Extractor` to call `Service.Do(ctx, *Request)` instead of
  `LLMBackend.Generate(ctx, prompt)`. Drop our three backend impls.
  Wire `llmhttp.Recorder` via the Langfuse adapter. ~800 LOC delete
  + ~200 LOC adapter.
- **PR-2**: Replace mock backend pattern with `llmtest.MockService`.
  Test files only. ~500 LOC churn, net delete.
- **PR-3**: Migrate `pkg/judge` to consume `llm.Service` instead of
  `LLMBackend`. ~150 LOC.
- **PR-4**: Move `pkg/observability/langfuse` non-recorder bits
  (datasets, dataset run items) — those are eval-loop infrastructure
  that doesn't belong on a recorder. Either keep in tracker or push
  upstream as Phase E groundwork.
- **PR-5**: Remove `pkg/extract` provider files
  (`ollama_backend.go`, `anthropic_backend.go`,
  `openai_compat_backend.go`, etc.) — they're now in the SDK.
- **PR-6**: Delete `pkg/observability/langfuse` (except dataset
  helpers if Step 4 kept them).

#### Total LOC delta (estimated)

- **Delete** from server-price-tracker: ~4,500 LOC (three provider
  impls + Langfuse client + their tests).
- **Add** to server-price-tracker: ~500 LOC (adapters + wiring).
- **Contribute** to sdk-booty-sh: ~2,500 LOC (provider impls + tests +
  Recorder adapter).
- **Net repo size change**: tracker shrinks by ~4,000 LOC; SDK grows by
  ~2,500. The asymmetry is because the SDK has shared infrastructure
  (request building, response parsing) that we currently re-derive per
  provider.

#### Risk

- **Migration regression risk.** We're moving load-bearing,
  battle-tested code. Token counting edge cases, fence-stripping for
  Anthropic, structured-JSON grammar mode for Ollama — these have
  been hardened through real bugs. Mitigate with: integration tests
  hitting real Ollama + Anthropic before and after; backend-by-backend
  rollout (Anthropic first since it's already in the SDK, then
  Ollama, then OpenAI-compat).
- **Coordinated release.** Every server-price-tracker PR depends on a
  specific sdk-booty-sh tag. We'd pin via go.mod and bump as upstream
  releases land. Standard semver discipline.
- **Test-helper compatibility.** `llmtest.MockService` has a
  scripted-response API; our current tests use ad-hoc per-backend
  mocks. Some test files need rewriting (not just import swaps).

### 6. Pros and cons

#### Pros

- **One canonical provider abstraction** used by tracker + any future
  consumer (the ADK product layer, the AWS triage bot, etc.). No
  per-product reimplementation drift.
- **Capabilities-driven gating** prevents a class of "OpenAI-compat
  model claims tool support but doesn't" silent failures.
- **Recorder/Listener split** is structurally cleaner than our
  ad-hoc decorator pattern. We've already started moving toward this
  shape (Langfuse buffer adapter in INV-0001) — the SDK has the
  general form.
- **Public test helpers** (`pkg/llm/llmtest`) replace per-package
  mock-rolling. Smaller test maintenance surface.
- **Skill loader (`pkg/skill`)** could replace our per-ComponentType
  Go template files with markdown files — addresses the INV-0002 §4.5
  "eight-touchpoint duplication" problem at a different angle than a
  Go registry would.
- **Versioned external dependency**: SDK improvements (better
  retries, new providers, streaming iterator) come for free with a
  `go get -u`. Today every improvement requires our own PR.
- **Provider ecosystem leverage**: someday Copilot SDK provider,
  someday Bedrock, someday Vertex — the SDK absorbs that work.
- **Smaller tracker.** Removing 4,500 LOC of provider plumbing makes
  the tracker code more about the *domain* (eBay → extract → score →
  alert) and less about *infrastructure* (HTTP retry / SSE parsing /
  token counting per provider).

#### Cons

- **Upstream Phase D contribution required.** We have to write Ollama
  + OpenAI-compat providers in the SDK before we can migrate. That's
  blocking effort that doesn't ship tracker-visible value until the
  migration completes.
- **Convo / tool-loop / Listener surface is unused for our extraction
  path.** We import `pkg/agent` (probably not — we only need
  `pkg/llm`) but the package's stated focus on tool-loop ergonomics is
  partly orthogonal to our needs. Worth confirming `pkg/llm` is
  usable standalone (it is, per the README's quick example).
- **License is "All rights reserved".** Tracker open-source is
  blocked unless that changes. Same owner so this is fixable, but
  needs to be addressed explicitly.
- **Migration cost.** ~6 PRs on the tracker side + ~3 PRs on the SDK
  side. Not free.
- **Token-counting and response-parsing edge cases** could regress.
  Our current backends have specific fixes — Anthropic markdown
  fence stripping, Ollama structured-JSON grammar, OpenAI-compat
  fallback shapes. The SDK's Anthropic provider already handles fences
  in its `internal/` impl; Ollama and OpenAI-compat would inherit our
  current handling because *we* write those providers.
- **Two-codebase coordination overhead.** Cross-repo PRs, version
  bumps, integration testing on a moving target.

#### Overall gain

**Net positive over a 6-12 month horizon.** The migration cost is
front-loaded; the maintenance dividend is long-tailed. If
server-price-tracker were the only consumer of agentic infrastructure
ever, the calculus would tip the other way (don't extract a library
for one consumer). But sdk-booty-sh's stated direction (ADK product
layer, triage bot, future agents) means the SDK is *going* to exist
either way. The choice is whether tracker uses it or maintains a
parallel implementation.

## Conclusion

**Answer:** Yes, the migration is worth doing — but **not yet**.

The migration unlocks long-term consolidation (one provider
abstraction, one Langfuse pattern, one test-helper surface), but it
depends on three concrete prerequisites:

1. **sdk-booty-sh Phase D** — Ollama and OpenAI-compat providers must
   exist upstream before tracker can switch. This is paid-down debt
   we'd write ourselves.
2. **Langfuse Recorder adapter** — either upstream as
   `pkg/llmhttp/langfuse` or in tracker as a single file.
3. **INV-0002 boundary fixes** (especially §1.1 `pkg/extract` →
   `internal/metrics`) — required so the consumer-side migration is
   to a clean target, not a tangled one.

Without those three, we're trading a working-but-imperfect in-house
SDK for an unfinished external one. With them, we're trading a
maintenance-tax abstraction for a published, versioned library that
serves more consumers than just this tracker.

The decision is essentially **"do we own the agentic SDK, or does
sdk-booty-sh"**, and the right answer is the latter — the SDK is
designed for multi-consumer reuse, and tracker is one of multiple
intended consumers. But the migration must wait until the SDK's
deferred phases close the providers gap.

## Recommendation

**Sequence:**

1. **Block on INV-0002 Wave 1 boundary fixes** (§1.1, §1.2) — clean
   target before migration.
2. **Contribute `pkg/llm/ollama` to sdk-booty-sh** (Phase D, sub-task
   1). Estimated effort: 2-3 PRs over 1-2 weeks. Port our existing
   Ollama backend; add SSE-equivalent if Ollama supports it (it
   doesn't by default; structured-JSON grammar is the closest
   feature).
3. **Contribute `pkg/llm/openai` to sdk-booty-sh** (Phase D, sub-task
   2). Similar effort. OpenAI-compat is broad — recommend documenting
   per-endpoint quirks (OpenRouter vs vLLM vs LM Studio) so the
   provider doesn't accumulate per-host special cases.
4. **Contribute or in-tracker-build a Langfuse Recorder adapter.**
   Prefer upstream as `pkg/llmhttp/langfuse` so the next consumer
   doesn't reinvent it.
5. **Tracker migration PR sequence** (PR-1 through PR-6 above).
   Backend-by-backend rollout: Anthropic first (already in SDK),
   then Ollama, then OpenAI-compat. Each PR has integration tests
   hitting the real provider before and after to catch regressions.
6. **Keep `pkg/judge` in tracker** for now. Phase E (eval/judge) in
   the SDK is deferred and the judge logic is fairly tracker-specific
   (deal/edge/noise verdicts, alert-context shape, daily-budget
   semantics).
7. **Co-design Phase E** with the SDK if a second consumer ever
   wants a judge — at that point, `pkg/judge` becomes a candidate for
   the same upstream-and-migrate pattern.

**Parking decisions:**

- **Defer the license question** until just before any consumer of
  this codebase ships publicly. Same-owner private use is fine.
- **Defer the streaming iterator (INV-0005)** until we want streaming
  extraction. We don't today.
- **Defer Skill-loader replacement** of our prompt templates. The
  in-Go templates work; moving to markdown is taste, not a forcing
  function.

**Open question:**

- **Is the SDK ready to accept Phase D contributions from this
  consumer, or is it currently solo-developed?** Same owner so
  presumably yes, but the contribution workflow (PRs, review
  expectations, release cadence) should be confirmed before
  committing the time.

## References

- INV-0001 — IMPL-0019 post-merge code review findings (load-bearing
  precedents the Langfuse adapter must preserve)
- INV-0002 — Architectural review of pre-style-guide codebase
  (boundary-fix prerequisites)
- INV-0003 — App-centric refactor (the parallel investigation; SDK
  migration is orthogonal but intersects in Phase 0 cleanup)
- `github.com/donaldgifford/sdk-booty-sh` — the SDK under evaluation
  - `pkg/llm` — provider abstraction
  - `pkg/llmhttp` — tracing seam
  - `pkg/llm/ant` — Anthropic provider
  - `pkg/agent` — conversation driver (unused for our extraction
    path; relevant if we ever add tool-using flows)
  - `pkg/skill` — skill loader
  - DESIGN-0001 — v1 core design
  - IMPL-0001 — phased implementation plan
  - INV-0003 in that repo — provider-abstraction-prototype (cites
    server-price-tracker as input)
- `pkg/extract/`, `pkg/judge/`, `pkg/observability/langfuse/` — the
  in-house SDK surface under consideration for replacement
