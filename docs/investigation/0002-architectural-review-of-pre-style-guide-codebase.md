---
id: INV-0002
title: "Architectural review of pre-style-guide codebase"
status: In Progress
author: Donald Gifford
created: 2026-05-15
---
<!-- markdownlint-disable-file MD025 MD041 -->

# INV 0002: Architectural review of pre-style-guide codebase

**Status:** In Progress
**Author:** Donald Gifford
**Date:** 2026-05-15

<!--toc:start-->
- [Question](#question)
- [Hypothesis](#hypothesis)
- [Context](#context)
- [Approach](#approach)
- [Environment](#environment)
- [Findings](#findings)
  - [1. Architecture (boundaries, interfaces, package layout)](#1-architecture-boundaries-interfaces-package-layout)
    - [1.1 pkg/extract imports from internal/* — Critical](#11-pkgextract-imports-from-internal--critical)
    - [1.2 internal/config imports pkg/observability/langfuse — Important](#12-internalconfig-imports-pkgobservabilitylangfuse--important)
    - [1.3 Store interface is 45+ methods wide — Critical](#13-store-interface-is-45-methods-wide--critical)
    - [1.4 Engine struct holds concrete eBay types — Important](#14-engine-struct-holds-concrete-ebay-types--important)
    - [1.5 Duplicate RescoreAll with different semantics — Critical](#15-duplicate-rescoreall-with-different-semantics--critical)
    - [1.6 Package-level mutable state in serve.go — Important](#16-package-level-mutable-state-in-servego--important)
    - [1.7 ProcessAlerts is a package-level function — Nice-to-have](#17-processalerts-is-a-package-level-function--nice-to-have)
    - [1.8 Inverted dependency: internal/api/web knows handler shapes — Nice-to-have](#18-inverted-dependency-internalapiweb-knows-handler-shapes--nice-to-have)
  - [2. Style (Uber Go Style Guide conformance)](#2-style-uber-go-style-guide-conformance)
    - [2.1 "failed to" error prefix — Important (style guide §Error Strings)](#21-failed-to-error-prefix--important-style-guide-error-strings)
    - [2.2 resp := &OutputType{} pattern — Nice-to-have (Uber §var-decl)](#22-resp--outputtype-pattern--nice-to-have-uber-var-decl)
    - [2.3 Get prefix on methods — Important (Uber §function-name)](#23-get-prefix-on-methods--important-uber-function-name)
    - [2.4 slog.Default() instead of injected logger — Critical](#24-slogdefault-instead-of-injected-logger--critical)
    - [2.5 Error type naming missing Error suffix — Important](#25-error-type-naming-missing-error-suffix--important)
    - [2.6 Redundant package prefix in error strings — Nice-to-have](#26-redundant-package-prefix-in-error-strings--nice-to-have)
    - [2.7 Naked bool parameters — Important](#27-naked-bool-parameters--important)
    - [2.8 fmt.Sprintf("%d", ...) for int → string — Nice-to-have](#28-fmtsprintfd--for-int--string--nice-to-have)
    - [2.9 Error chain broken in API handlers — Important](#29-error-chain-broken-in-api-handlers--important)
  - [3. Performance (hot paths, allocations, query shape)](#3-performance-hot-paths-allocations-query-shape)
    - [3.1 N+1: ListWatches per scored listing — Critical](#31-n1-listwatches-per-scored-listing--critical)
    - [3.2 N+1: per-alert HasSuccessfulNotification + GetListingByID — Critical](#32-n1-per-alert-hassuccessfulnotification--getlistingbyid--critical)
    - [3.3 RecomputeAllBaselines sequential loop — Important](#33-recomputeallbaselines-sequential-loop--important)
    - [3.4 Unsized slice append in formatItemSpecifics — Nice-to-have](#34-unsized-slice-append-in-formatitemspecifics--nice-to-have)
    - [3.5 []byte(content) copy in JSON parse — Nice-to-have](#35-bytecontent-copy-in-json-parse--nice-to-have)
    - [3.6 bytes.Buffer not pooled in prompt builder — Nice-to-have](#36-bytesbuffer-not-pooled-in-prompt-builder--nice-to-have)
    - [3.7 strconv.Itoa per request in middleware — Nice-to-have](#37-strconvitoa-per-request-in-middleware--nice-to-have)
    - [3.8 AlertsFiredByWatch unbounded label cardinality — Important](#38-alertsfiredbywatch-unbounded-label-cardinality--important)
  - [4. Tech debt (testing, complexity, duplication)](#4-tech-debt-testing-complexity-duplication)
    - [4.1 PostgresStore test coverage 8.2% — Important](#41-postgresstore-test-coverage-82--important)
    - [4.2 serve.go is 677 LOC — Important](#42-servego-is-677-loc--important)
    - [4.3 Logging inconsistency (slog DI vs slog default vs fmt.Print) — Important](#43-logging-inconsistency-slog-di-vs-slog-default-vs-fmtprint--important)
    - [4.4 Scan-order brittleness — Critical](#44-scan-order-brittleness--critical)
    - [4.5 Eight-touchpoint ComponentType duplication — Important](#45-eight-touchpoint-componenttype-duplication--important)
    - [4.6 Config sprawl: 22 structs in 461 LOC — Nice-to-have](#46-config-sprawl-22-structs-in-461-loc--nice-to-have)
    - [4.7 Manual orphan baseline cleanup — Important](#47-manual-orphan-baseline-cleanup--important)
    - [4.8 Pre-classification hooks hard-coded ordering — Nice-to-have](#48-pre-classification-hooks-hard-coded-ordering--nice-to-have)
    - [4.9 condition_norm not derived from title signals — Important](#49-condition_norm-not-derived-from-title-signals--important)
- [Conclusion](#conclusion)
- [Recommendation](#recommendation)
  - [Wave 1 — boundary + correctness fixes (parallel, safe)](#wave-1--boundary--correctness-fixes-parallel-safe)
  - [Wave 2 — performance (parallel, low risk)](#wave-2--performance-parallel-low-risk)
  - [Wave 3 — naming + ergonomics (sequence with §1.3)](#wave-3--naming--ergonomics-sequence-with-13)
  - [Wave 4 — testability + scan-order safety](#wave-4--testability--scan-order-safety)
  - [Wave 5 — structural cleanup](#wave-5--structural-cleanup)
  - [Wave 6 — the big lift](#wave-6--the-big-lift)
  - [Wave 7 — polish](#wave-7--polish)
- [References](#references)
<!--toc:end-->

## Question

The codebase grew organically from MVP through IMPL-0019 before the Uber Go
style skill, go-architect, and go-performance review agents existed. **What
specific architectural, stylistic, performance, and tech-debt gaps exist in
the current code, and what is the right sequencing to fix them?**

The goal is not to rewrite — the goal is to surface every concrete issue
with a file:line citation so each can be triaged into a follow-up PR or
deliberately deferred with a rationale.

## Hypothesis

Because the code predates the style guides, we expect to find:

- **Boundary violations** between `pkg/` (public) and `internal/`.
- **Fat interfaces** (especially the `Store`) that have accreted methods.
- **N+1 queries** on alert evaluation, baseline recompute, and batch sends.
- **Duplicate logic** around ComponentType (the eight-touchpoint problem
  documented in CLAUDE.md is a known smell).
- **Style drift** — "failed to" error prefixes, `Get`-prefixed methods,
  `slog.Default()` instead of dependency injection.
- **Test coverage gaps** in the data layer (raw SQL is hard to TDD).

If any of these are absent we should be pleasantly surprised. If they
compound (e.g., a hot path also has an N+1 also has unbounded metric
cardinality) we want them surfaced together so a single PR can fix the
cluster instead of three separate ones.

## Context

**Triggered by:** Operator review post-IMPL-0019 deploy.

INV-0001 already documented 10 post-merge issues from a focused review of
the IMPL-0019 branch. That review surfaced load-bearing precedents (drop-
newest buffer semantics, stopCh lifecycle, untrusted-prompt sanitisation)
but was deliberately scoped to the new code. The user's request here is
broader: take the same lens to the entire codebase, including code that
predates the style skill.

The alert review UI proved insufficient for operator workflow ("basically
useless") which is a downstream symptom — the engine and store layers
weren't designed for the query patterns the UI now needs. This is the
right moment to audit before adding more.

**Codebase scale at time of review:**

- 20,029 LOC, 227 Go files
- Largest files: `internal/store/postgres.go` (1,279 LOC),
  `cmd/server-price-tracker/cmd/serve.go` (677), `internal/engine/engine.go`
  (616), `pkg/extract/extractor.go` (478)

## Approach

Two passes of four parallel review agents, each scoped to a single
lens:

1. **go-architect** — package boundaries, dependency direction, interface
   shape, dependency injection patterns, separation of concerns.
2. **go-style** — Uber Go Style Guide conformance: naming, error handling,
   struct initialisation, control flow.
3. **go-performance** — allocations, N+1 queries, hot-path benchmarks,
   metric cardinality, goroutine lifecycle.
4. **Explore (general tech debt)** — test coverage, complexity, hard-coded
   sequences, duplication, scan-order brittleness.

**First pass** (initial review): each agent ran cold without prior
context. Findings landed as §1-§4 below.

**Second pass** (follow-up review on the same commit): each agent was
briefed on the first pass's findings and asked to surface what was
missed. The instruction was "look for what the first pass didn't
find"; agents that surfaced duplicates of §1-§4 had those merged
inline (expanding §2.4, §3.1, §3.2). The novel findings landed as §5.
Three second-pass claims didn't survive verification against the
actual code and are recorded in §5.18 as rejected so they don't get
re-investigated.

Findings are grouped by lens but cross-referenced where the same issue
appears in multiple reviews (the `Store` interface, for instance, shows up
in both architect and explore findings — those are merged).

Severity scale used throughout:

| Tag | Meaning |
|---|---|
| **Critical** | Correctness, security, or boundary violation; fix before extending the area |
| **Important** | Maintainability or hot-path cost; fix before the next feature |
| **Nice-to-have** | Polish; fix opportunistically during nearby work |

## Environment

| Component | Version / Value |
|-----------|----------------|
| Go | 1.25.10 |
| golangci-lint | 2.8.0 |
| Reviewed branch | `main` @ commit `e5bb4e1` |
| Review date | 2026-05-15 |
| Lens count | 4 parallel agents |

## Findings

### 1. Architecture (boundaries, interfaces, package layout)

#### 1.1 `pkg/extract` imports from `internal/*` — **Critical**

`pkg/` is contractually public-importable; it must not depend on
`internal/`.

- `pkg/extract/extractor.go:16` imports `internal/metrics`
- `pkg/extract/langfuse_backend.go:10` imports `internal/version`

The metrics dependency should follow the same pattern already established
for the Langfuse buffer (INV-0001 §"BufferMetrics adapter pattern"): inject
an interface from `pkg/extract`, implement the adapter in `internal/metrics`,
wire in `cmd/.../serve.go`. The version dependency is gratuitous — the
caller (serve.go) knows the version and can pass it in via a functional
option.

#### 1.2 `internal/config` imports `pkg/observability/langfuse` — **Important**

`internal/config/config.go:13` imports the Langfuse client just to reuse
its `ModelCost` struct. This inverts dependency direction (config is the
foundation; observability layers sit above it).

Define a local `ModelCost` struct in `internal/config` (it's three fields:
input rate, output rate, currency) and convert at the boundary in serve.go.

#### 1.3 `Store` interface is 45+ methods wide — **Critical**

`internal/store/store.go:74-182` exposes every operation on every entity
through one interface. This breaks the Interface Segregation Principle and
makes mocking expensive (45 methods to stub even when a test only needs
one).

The CLAUDE.md guidance is *"interfaces belong in consumer packages"* —
right now every consumer takes the whole `Store`. Split by entity:

- `WatchStore` (CRUD + filter helpers)
- `ListingStore` (CRUD + query + active-flag toggles)
- `BaselineStore` (recompute, lookup, refresh)
- `AlertStore` (create, mark notified, dismiss, restore, review query)
- `QueueStore` (extraction queue: enqueue, claim, complete)
- `JobStore` (scheduler state, job runs)
- `RateLimitStore` (token bucket + daily quota)
- `JudgeStore` (judge scores, recent un-judged alerts)

Each consumer takes the narrow interface it actually uses. The concrete
`PostgresStore` keeps every method (it satisfies all the interfaces); only
the *consumer-facing* contracts shrink. This is a refactor done in-place;
no migration required.

#### 1.4 `Engine` struct holds concrete eBay types — **Important**

`internal/engine/engine.go:31-32` has `ebay.Paginator` and
`ebay.AnalyticsClient` as concrete fields. The MVP-era `EbayClient`
interface (per CLAUDE.md "Interface-First Design") was meant to cover the
whole external boundary; these were added later without an interface
update.

Define `EbayPaginator` and `EbayAnalytics` (or merge into `EbayClient` if
the surface stays small) so the engine's tests can mock both. Right now
engine tests can't exercise paginated ingestion or analytics fallbacks.

#### 1.5 Duplicate `RescoreAll` with different semantics — **Critical**

There are two methods named `RescoreAll`: one on the engine, one on the
store, with subtly different behaviour (the store's variant skips listings
where `component_type IS NULL`; the engine's does not, and re-invokes the
scorer). The `/api/v1/rescore` handler calls the engine variant. Operators
running ad-hoc backfills sometimes call the store variant via psql/RPC
helpers and get a different count.

Rename: `Store.RescoreAll` → `Store.RecomputeListingScores` (it's a SQL-
side recompute against current baselines); keep `Engine.RescoreAll` (it's
the user-facing operation that re-runs the scorer over hydrated data).

#### 1.6 Package-level mutable state in `serve.go` — **Important**

- `cmd/server-price-tracker/cmd/serve.go:578` — `judgeWorker` is a
  package-level `var`. It's set in `runServe` and read by signal-handler
  goroutines. Should be a field on a `serveState` struct (or local
  variable closed over by the signal handler).

#### 1.7 `ProcessAlerts` is a package-level function — **Nice-to-have**

`internal/engine/alert.go:38` exports `ProcessAlerts` as a package-level
function that takes the engine plus several dependencies as arguments.
Convention everywhere else in the engine is method-on-Engine. Move it onto
`*Engine` so it can use injected fields (logger, store, notifier) directly.

#### 1.8 Inverted dependency: `internal/api/web` knows handler shapes — **Nice-to-have**

`internal/api/web/components` (templ) imports DTOs straight from the
handler package. The split between rendering and data shaping is good in
principle, but the import direction means the renderer breaks every time a
DTO field is renamed for an API contract reason. Mirror the relevant DTOs
in `web/viewmodels` and convert at the handler edge.

---

### 2. Style (Uber Go Style Guide conformance)

#### 2.1 "failed to" error prefix — **Important** (style guide §Error Strings)

Style guide is explicit: lowercase, no "failed to". Found in:

- 12+ `slog.Error("failed to ...")` log calls across `internal/engine/*`,
  `internal/api/*`, `cmd/spt/*`
- 3 returned errors:
  - `internal/engine/scheduler.go:147` — `fmt.Errorf("failed to start scheduler: %w", err)`
  - `pkg/extract/extractor.go:198` — `fmt.Errorf("failed to call LLM backend: %w", err)`
  - `internal/store/postgres.go:892` — `fmt.Errorf("failed to scan alert: %w", err)`

Replace with the operation verb: `"start scheduler"`, `"call LLM backend"`,
`"scan alert"`. Fix as a single sweep PR — `rg "failed to" --type go`.

#### 2.2 `resp := &OutputType{}` pattern — **Nice-to-have** (Uber §var-decl)

13 Huma handlers initialise their output struct as `resp := &OutputType{}`
then assign fields. Idiomatic Go is `var resp OutputType` (zero struct,
no allocation, no pointer) — Huma takes the value, not the pointer.

Affected files: `internal/api/handlers/{watches,listings,alerts,extract,
ingest,baseline,rescore,reextract,extraction,system,jobs,quota,judge}.go`.

#### 2.3 `Get` prefix on methods — **Important** (Uber §function-name)

14 methods follow Java-style `GetX` naming. Idiomatic Go drops the prefix
(`user.Name()` not `user.GetName()`). Found across `internal/store`,
`pkg/scorer`, and `pkg/extract`. Examples:

- `Store.GetWatchByID` → `Store.WatchByID`
- `Store.GetListingByID` → `Store.ListingByID`
- `Store.GetBaseline` → `Store.Baseline`

Bundle this rename with the Store interface split (§1.3) — both touch the
same method signatures.

#### 2.4 `slog.Default()` instead of injected logger — **Critical**

**Expanded in second-pass review (see §5.10):** the pattern is wider
than `alert.go` alone — library packages also fall back to
`slog.Default()` instead of requiring an injected logger.

The engine has a `logger *slog.Logger` field but ignores it in four spots:

- `internal/engine/alert.go:118` — `slog.Default().Info(...)`
- `internal/engine/alert.go:238` — `slog.Default().Warn(...)`
- `internal/engine/alert.go:311` — `slog.Default().Error(...)`
- `internal/engine/alert.go:329` — `slog.Default().Debug(...)`

Additional library-package sites (second-pass):

- `pkg/judge/worker.go:114` — `NewWorker` falls back to `slog.Default()`
- `pkg/observability/langfuse/buffered_client.go:111` —
  `NewBufferedClient` falls back to `slog.Default()`
- `pkg/extract/extractor.go:97` — `NewLLMExtractor` falls back to
  `slog.Default()`

This breaks the structured-logging contract — operator log filters
matching `service=spt` work against the injected logger but the default
logger has no such attribute. Use the injected logger everywhere; the
library constructors should require non-nil and let the caller decide
whether to pass `slog.Default()` explicitly.

#### 2.5 Error type naming missing `Error` suffix — **Important**

`pkg/extract/types.go` has `ValidationFailure` (should be
`ValidationError`), `ParseFailure` (should be `ParseError`). The style
guide convention: types representing errors end in `Error`; sentinel values
start with `Err`.

#### 2.6 Redundant package prefix in error strings — **Nice-to-have**

`pkg/judge/worker.go:92,95`:

```go
errors.New("judge.NewWorker: judge dependency is required")
errors.New("judge.NewWorker: store dependency is required")
```

Style guide: error strings don't include the function name (caller adds
context via `%w` wrap). Drop the `judge.NewWorker:` prefix.

#### 2.7 Naked bool parameters — **Important**

- `internal/engine/engine.go:282` — `evaluateAlertsForListing(ctx, listing, force bool)`
- `internal/engine/engine.go:314` — `notifyBatch(ctx, alerts, dryRun bool)`
- `internal/engine/engine.go:394` — `markListingsActive(ctx, ids, active bool)`

Callers read as `notifyBatch(ctx, alerts, true)` — unreadable at the call
site. Replace with named typed enums (`DryRunMode` / `LiveMode`) or split
into two methods.

#### 2.8 `fmt.Sprintf("%d", ...)` for int → string — **Nice-to-have**

`cmd/spt/output.go:71` — use `strconv.Itoa` (faster + clearer intent).

#### 2.9 Error chain broken in API handlers — **Important**

18 handlers do `huma.Error500("description: " + err.Error())` instead of
`huma.Error500("description", err)`. The string-concat path loses the
error chain — `errors.Is/As` no longer works against the original.

Huma's `Error500InternalServerError(msg, errs ...error)` accepts an error
chain; use it.

---

### 3. Performance (hot paths, allocations, query shape)

#### 3.1 N+1: `ListWatches` per scored listing — **Critical**

`internal/engine/engine.go:282` — `evaluateAlertsForListing` calls
`store.ListWatches(ctx)` once per listing being evaluated. With ~200
listings/cycle and ~30 watches each, this is 6,000 DB roundtrips per cycle
where 1 would suffice.

**Expanded in second-pass review:** the same root cause fires from
`(*Engine).RescoreAll` (engine.go:246), which cursor-iterates every
active listing in batches of 200 and calls `evaluateAlertsForListing`
once per row. A full rescore over 5,000 listings is 5,000 round-trips
to fetch the watch list that never changes.

Fix: cache watches at the start of the engine tick (they change at human
pace, not at listing-arrival pace) and pass the slice down. Bonus: ranged
loop over a slice is allocation-free; per-call `ListWatches` allocates a
fresh slice every time. The fix is one cache + a parameter on
`evaluateAlertsForListing`; covers both call sites.

#### 3.2 N+1: per-alert `HasSuccessfulNotification` + `GetListingByID` — **Critical**

`internal/engine/alert.go:264-271` — for each alert in the batch:

```go
ok, err := store.HasSuccessfulNotification(ctx, alertID)
listing, err := store.GetListingByID(ctx, listing_id)
```

Two queries × batch size. With Discord summary mode collapsing one tick's
alerts into a single embed (DESIGN-0010), batch sizes can be 20-50.

**Expanded in second-pass review:** the same shape appears in
`processSummary` (alert.go:88), which loops over the pending-alerts
list and calls `store.GetListingByID(ctx, pending[i].ListingID)` for
each one. The summary-mode path has *no upper bound* on batch size,
unlike `sendBatch` which is at least capped by `batchThreshold`. A
single-tick summary over 200 pending alerts is 200 round-trips.

Fix: add `Store.ListingsByIDs(ctx, ids []string)` (single query with
`WHERE id = ANY($1)`) and `Store.AlertsWithNotificationStatus(ctx, ids)` —
both single-query joins. Engine consumes the maps directly. Apply to
both `sendBatch` and `processSummary`.

#### 3.3 `RecomputeAllBaselines` sequential loop — **Important**

`internal/store/postgres.go:384-409` iterates product keys and runs one
INSERT-or-UPDATE per key. Empirically this is the slowest endpoint on the
deployed system (15+ seconds for ~3,000 product keys).

Fix: single SQL statement using `INSERT ... SELECT ... ON CONFLICT DO
UPDATE` with a window function or a CTE that computes percentiles per
group. Postgres can do this in one query in under a second.

#### 3.4 Unsized slice append in `formatItemSpecifics` — **Nice-to-have**

`pkg/extract/prompts.go:417` — `var lines []string` then appends in a loop
with known max size. Pre-size: `lines := make([]string, 0, len(specifics))`.

#### 3.5 `[]byte(content)` copy in JSON parse — **Nice-to-have**

`pkg/extract/extractor.go:289` — `json.Unmarshal([]byte(content), &out)`
where `content` is a `string`. Allocates a copy of every response.

Use `json.NewDecoder(strings.NewReader(content)).Decode(&out)` — same
behaviour, no copy. With Ollama returning multi-KB responses, the saving
is real.

#### 3.6 `bytes.Buffer` not pooled in prompt builder — **Nice-to-have**

`pkg/extract/prompts.go:360,378,397` — three prompt builders allocate a
fresh `bytes.Buffer` per call. Use `sync.Pool[*bytes.Buffer]` (or, if
prompt build dominates extraction time at high throughput, replace with
`strings.Builder` and an exhaust-channel reuse pattern).

Defer until profiling shows prompt construction in the top 5 of CPU
samples — current throughput (30-50 extractions/day) makes this academic.

#### 3.7 `strconv.Itoa` per request in middleware — **Nice-to-have**

`internal/metrics/metrics.go:52` — formats status code per request. Pre-
compute a `[600]string` array indexed by status code and avoid the call
entirely. Marginal but free.

#### 3.8 `AlertsFiredByWatch` unbounded label cardinality — **Important**

`internal/engine/alert.go:249,309` — metric is labelled by `watch_name`
which is operator-set free text. If an operator creates 1,000 watches
each cycle the Prometheus series count explodes.

Fix: label by `watch_id` (bounded UUID); join to name in Grafana via a
recording rule. Or cap label cardinality at the metric layer (drop labels
when watch count > 100).

---

### 4. Tech debt (testing, complexity, duplication)

#### 4.1 `PostgresStore` test coverage 8.2% — **Important**

53 methods, ~4 tested. The data layer is the highest-blast-radius part of
the system — a wrong SELECT column order silently corrupts every scanned
listing — and we have almost no tests of it.

Options:

- **Testcontainers Postgres** in unit tests (slowest, most reliable, no
  CI changes). Pattern is well-established; the runtime cost is ~5s
  startup amortised across all tests.
- **dockertest** with a session-scoped Postgres container.
- **sqlmock** — fast but tests the wrong thing (mock-vs-prod divergence is
  why migration 013's FK type bug shipped).

Recommend testcontainers; gate behind a build tag (`//go:build dbtest`)
so the fast `make test` path stays mock-only.

#### 4.2 `serve.go` is 677 LOC — **Important**

The boot path is unscoped: flag parsing, config loading, store init,
backend selection, scheduler wiring, signal handling, graceful shutdown,
metric registration, observability bootstrap — all in one file.

Split:

- `cmd/server-price-tracker/cmd/serve.go` — humacli registration only
- `internal/bootstrap/store.go` — DB connect + migrate
- `internal/bootstrap/backend.go` — LLM backend selection
- `internal/bootstrap/observability.go` — OTel + Langfuse init
- `internal/bootstrap/scheduler.go` — scheduler + worker wiring
- `internal/bootstrap/lifecycle.go` — signal handling + shutdown

Each piece becomes testable. The serve command is then a 50-line
composition.

#### 4.3 Logging inconsistency (slog DI vs slog default vs fmt.Print) — **Important**

Three logging conventions in one binary:

- Server code: structured `slog` via injected logger (good)
- `internal/engine/alert.go`: `slog.Default()` (§2.4, bug)
- `cmd/spt/*`: `fmt.Println`/`fmt.Fprintf` (CLI, intentional but
  inconsistent with the rest)

Recommend: keep CLI on fmt (user-facing TTY output), fix all server-side
`slog.Default()` to use injected loggers, and add a lint rule to forbid
`slog.Default()` outside `cmd/spt`.

#### 4.4 Scan-order brittleness — **Critical**

`scanListing` / `scanListingRow` / etc. depend on column order matching
SELECT order exactly. Every column addition requires updating: domain
struct, every SELECT in `queries.go`, both scan functions. The CLAUDE.md
warning ("when adding a column to listings, update: domain struct, all
SELECT queries, both scan functions") is itself the smoking gun — the
codebase shouldn't need to warn about this.

Options:

- **sqlx** — `db.GetContext(ctx, &out, query)` uses struct tags, scan order
  derived from query column list automatically. Mature, no ORM.
- **sqlc** — compile-time-checked queries from SQL files. Big rewrite.
- **pgx scany** (`github.com/georgysavva/scany/v2/pgxscan`) — drop-in
  scanner for pgx that uses struct tags. Smallest delta.

Recommend pgx scany — single import, struct-tag-driven, keeps all current
SQL in `queries.go`.

#### 4.5 Eight-touchpoint ComponentType duplication — **Important**

The CLAUDE.md note ("Adding a new ComponentType — eight touchpoints to
keep in sync") is a documentation workaround for a structural problem.
Each ComponentType should be a single object that registers itself:

```go
// pkg/extract/component/server.go
var Server = component.New("server", component.Spec{
    Patterns:        regexp.MustCompile(`...`),  // pre-classifier
    PromptTemplate:  serverPromptTmpl,           // LLM template
    Validate:        validateServer,             // validator
    Normalize:       normalizeServer,            // normaliser
    ProductKey:      serverProductKey,           // key derivation
    DBConstraint:    "server",                   // CHECK enum value
})
```

A `component.Registry.Register(Server)` call wires everything. Adding a
ComponentType becomes one new file + one CHECK migration. The current
eight-step ritual is the largest source of bugs in the system (the
IMPL-0018 migration 011 incident, the IMPL-0017 baseline orphan incident).

This is the largest refactor in this report. Defer if scope is a concern
— but the bug economics already justify it.

#### 4.6 Config sprawl: 22 structs in 461 LOC — **Nice-to-have**

`internal/config/config.go` has 22 nested config structs. Several are
single-field wrappers around a `bool`/`string` that could collapse into
the parent. The Web/Observability/Notifications sections are particularly
flat.

Low priority — config is read once at boot, complexity tax is small.

#### 4.7 Manual orphan baseline cleanup — **Important**

CLAUDE.md documents: *"After any normaliser change, manually clean up:
DELETE FROM price_baselines WHERE product_key NOT IN ..."*

This should be automatic. `recompute_baseline` (migration 008) only
INSERT-UPDATEs. Add a final DELETE step that prunes baselines whose
product key no longer matches any active listing.

The CLAUDE.md note already flags this as a "real fix parked as follow-up"
— bump it.

#### 4.8 Pre-classification hooks hard-coded ordering — **Nice-to-have**

`(*LLMExtractor).ClassifyAndExtract` runs three pre-classifier hooks
(DetectSystemTypeFromTitle, IsAccessoryOnly, DetectSystemTypeFromSpecifics)
in a hard-coded order. Adding a fourth hook means editing the orchestrator
in a specific position.

A typed list (`[]PreClassifier{TitleHook{}, AccessoryHook{},
SpecificsHook{}}`) ranged in order would be no worse, and `Insert(idx,
hook)` becomes trivial. Tiny refactor, paid back the second time it's
extended.

#### 4.9 `condition_norm` not derived from title signals — **Important**

Already documented as a follow-up (CLAUDE.md "condition_from_title_followup")
— "FOR PARTS" in title does not propagate to `condition_norm`, polluting
baselines. Surface it here so it's tracked alongside the rest.

---

### 5. Second-pass findings (2026-05-15 follow-up)

A second round of the same four lenses was run against the same
commit. The agents were briefed on the §1-§4 findings to focus on
incremental issues. **20 additional findings** surfaced — three are
expansions of §2.4/§3.1/§3.2 already merged inline above; the other
17 are listed here. Numbering continues per-lens (`A` for architecture,
`S` for style, `P` for performance) to avoid renumbering §1-§4.

The second pass also surfaced **three false-positive claims** worth
recording so they're not re-investigated. See §5.18.

#### Architecture (second-pass)

#### 5.A1 `internal/store/postgres.go` imports `internal/metrics` — **Important**

`internal/store/postgres.go:15` — the store layer directly increments
Prometheus counters (`AlertsQueryDuration`, `NotificationAttemptsInsertedTotal`,
`AlertsTableRows`) inside method bodies. This is *not* a `pkg/`
boundary violation (both packages are under `internal/`) but it's the
same anti-pattern at the wrong layer: the persistence layer should
not own metric emission. Engine and scheduler emit their own metrics
after calling store methods, which is the correct layering.

Fix: define `StoreMetricsRecorder` interface in `internal/store/store.go`,
wire a `metrics.StoreMetricsAdapter{}` in `serve.go`, inject via
`NewPostgresStore`. Mirrors the `BufferMetrics` pattern from INV-0001.

#### 5.A2 `internal/engine/engine.go` holds concrete `config.AlertsConfig` — **Critical**

`internal/engine/engine.go:37,119` — `Engine` holds a
`config.AlertsConfig` field and `WithAlertsConfig` accepts that
concrete struct. The only field consumed is `ReAlertsCooldown`
(engine.go:463,465). Any rename or restructuring of `AlertsConfig`
forces an engine.go touch.

Fix: drop the config import. Replace with `reAlertsCooldown time.Duration`
and a `WithReAlertsCooldown(d time.Duration) EngineOption`. Caller in
`serve.go` passes `cfg.Alerts.ReAlertsCooldown` directly.

#### 5.A3 `NewPostgresStore` silently ignores `DatabaseConfig.PoolSize` — **Critical**

`internal/store/postgres.go:42` unconditionally sets
`cfg.MaxConns = defaultPoolSize` (10), ignoring the `PoolSize` field
read from YAML and available in `config.DatabaseConfig`. Operators who
configure `database.pool_size` get no effect — the parameter is
silently discarded. Correctness gap under load.

Fix: change `NewPostgresStore(ctx, connString string)` to accept
`maxConns int` (or a functional option). Wire from `cfg.Database.PoolSize`
in `serve.go`.

#### 5.A4 Duplicate `stripJSONFences` in `pkg/extract` and `pkg/judge` — **Important**

`pkg/extract/extractor.go:113` and `pkg/judge/llm_judge.go:151` are
byte-for-byte identical functions. The comment at `llm_judge.go:147`
acknowledges this explicitly ("duplicated rather than exported
because the extract one is a private helper").

Fix: extract to a new `pkg/llmutil` package (or `pkg/extract/response`)
and export `StripJSONFences`. Both callers import it. No `internal/`
boundary crossing.

#### 5.A5 `internal/engine/score.go` exports package-level functions that are unused — **Important**

`internal/engine/score.go:77,87,104` — `RescoreListings`,
`RescoreByProductKey`, and a package-level `RescoreAll` are exported
but have zero callers outside the package. `(*Engine).RescoreAll`
(engine.go:246) is the actual public surface — it inlines the cursor
logic rather than delegating.

Fix: rename to `rescoreListings`, `rescoreByProductKey`, `rescoreAll`
(unexported). They're implementation details of the engine package.

#### 5.A6 `internal/engine/scheduler.go` imports `pkg/observability/langfuse` only for `WithSessionID` — **Important**

`internal/engine/scheduler.go:18,169` — scheduler imports the entire
Langfuse package solely to call `langfuse.WithSessionID(ctx, sessionID)`.
The function is a no-op when Langfuse is disabled, but the import is
permanent — soft coupling to an optional subsystem.

Fix: move `WithSessionID` / `SessionIDFromContext` into a tiny
`pkg/session` or `internal/sessionctx` package with no Langfuse
dependency. The Langfuse client *reads* from the context key but
doesn't *own* setting it.

#### 5.A7 `QuotaHandler` accepts concrete `*ebay.RateLimiter` — **Nice-to-have**

`internal/api/handlers/quota.go:14-19` — `QuotaHandler` holds
`*ebay.RateLimiter` directly. Four methods consumed
(`MaxDaily/DailyCount/Remaining/ResetAt`); every other handler in the
package defines its own narrow interface.

Fix: define `QuotaProvider` interface; accept it in `NewQuotaHandler`.
Consistent with `Rescorer`, `Ingester`, `ExtractionStatsStore`,
`JobsProvider`, `SystemStateProvider` already in the package.

#### 5.A8 Four handlers still accept the full `store.Store` interface — **Nice-to-have**

`internal/api/handlers/listings.go:15`, `watches.go:15`,
`baselines.go:15`, `health.go:15` — these four handlers hold
`store.Store` (the 45-method interface, see §1.3) while their peers
(`extraction_stats.go`, `jobs.go`, `system_state.go`, `rescore.go`,
`trigger.go`) define narrow per-handler interfaces.

Fix: define `ListingsStore`, `WatchStore`, `BaselinesStore`,
`HealthStore` interfaces in each handler file, scoped to actually-
called methods. The existing `MockStore` satisfies them automatically.
Bundle with §1.3 work.

#### Style (second-pass)

#### 5.S1 Magic verdict thresholds in judge worker — **Important**

`pkg/judge/worker.go:288,290` — `verdictBucket` uses bare `0.7` and
`0.3` literals with no named constants. The cutoffs also appear in
`examples.json` and operator docs; a tuning change misses one.

Fix: `const verdictDealThreshold = 0.7` and
`const verdictNoiseThreshold = 0.3`.

#### 5.S2 Magic `MaxTokens: 256` in judge generate request — **Important**

`pkg/judge/llm_judge.go:87` — literal `256` is the judge's token
budget; controls truncation. Should be a named constant near the
other judge defaults.

Fix: `const judgeMaxTokens = 256`.

#### 5.S3 Duplicate `const batchSize = 200` — **Important**

`internal/engine/score.go:109` and `internal/engine/engine.go:247` —
same constant declared twice in the same package. Both are valid;
the duplication is a `decl-group` style violation.

Fix: hoist to a single package-level `const` in `engine.go`; delete
the local declaration in `score.go`.

#### 5.S4 Missing `var _ LLMBackend = (*X)(nil)` compliance assertions — **Important**

`pkg/extract/anthropic.go`, `ollama.go`, `openai_compat.go`,
`langfuse_backend.go` — none have the interface-compliance assertion.
`pkg/observability/langfuse` has them on all three Client impls (a
good precedent). A future method on `LLMBackend` would silently
break all four implementations.

Fix: add `var _ LLMBackend = (*AnthropicBackend)(nil)` etc. to each
implementation file.

#### 5.S5 Unreachable `panic` in OTel noop meter registration — **Nice-to-have**

`internal/engine/meter.go:31` and `pkg/extract/meter.go:41` —
`panic(fmt.Sprintf("noop histogram registration failed: %v", nerr))`
inside a `sync.OnceValue` closure. The comment acknowledges the noop
constructor "cannot fail". Unreachable code; Uber rule still says no
panic outside `main`/`init`.

Fix: drop the inner `if nerr != nil` block; return the noop histogram
directly.

#### 5.S6 Hard-coded HTTP timeout in `NewAnthropicBackend` — **Nice-to-have**

`pkg/extract/anthropic.go:69` — `60 * time.Second` literal with no
named constant. Ollama and OpenAI-compat backends also set timeouts;
all three are bare literals.

Fix: `const defaultAnthropicTimeout = 60 * time.Second` (and same for
the other two).

#### 5.S7 Test helpers missing `t.Helper()` — **Nice-to-have**

`internal/engine/engine_test.go:39,44` — `expectCountMethods` and
`newTestEngine` are called from test bodies but don't register
`t.Helper()`. Stack traces on mock-expectation failures point at the
wrong line.

Fix: take `t *testing.T` as first param; call `t.Helper()`.

#### 5.S8 Scheduler lock TTLs are bare literals — **Nice-to-have**

`internal/engine/scheduler.go` — `30*time.Minute` appears three times
(ingestion, re-extraction, judge) and `60*time.Minute` once (baseline
refresh) as inline literals in `runJob` calls. Tuning knobs for
operators.

Fix: named constants or config-driven values.

#### Performance (second-pass)

#### 5.P1 `GetAlertDetail` makes two round-trips when one would suffice — **Critical**

`internal/store/postgres.go:685-728` — runs the full
`alerts/listings/watches` JOIN query (line 687) which already has
all `watches` columns available via `alertReviewSelectColumns`, then
calls `s.GetWatch(ctx, row.Alert.WatchID)` (line 713) to re-fetch the
watch row. The second query is redundant.

`scanAlertWithListing` (line 674) currently scans only `WatchName`
from the watch, which is why `GetWatch` is "needed". Fix: expand
`scanAlertWithListing` to populate the full `domain.Watch` from the
JOIN result. Drop the `GetWatch` call.

Bonus: `rows.Close()` is called explicitly at line 711 *after*
`defer rows.Close()` at line 698 — double-close. pgx handles it
idempotently but it's noise.

#### 5.P2 Missing covering index for `queryHasRecentAlert` — **Critical**

`internal/store/queries.go:369` filters
`watch_id = $1 AND listing_id = $2 AND notified = true AND notified_at > $3`.
Existing alerts indices: `idx_alerts_watch` (watch_id only),
`idx_alerts_pending` (partial `WHERE notified = false`). Neither
covers this access pattern — the partial pending index *excludes*
notified=true rows. With any volume this is a sequential scan over
notified-alerts history.

Fix: add migration
`CREATE INDEX idx_alerts_cooldown ON alerts (watch_id, listing_id, notified_at DESC) WHERE notified = true;`.
Hot path: every alert evaluation runs this check.

#### 5.P3 Missing covering index for `queryHasSuccessfulNotification` — **Important**

`internal/store/queries.go:382` filters
`alert_id = $1 AND succeeded = true`. `notification_attempts_alert`
covers `(alert_id, attempted_at DESC)` but doesn't restrict by
`succeeded`. Postgres fetches all attempts for the alert and filters
in memory. For high-retry-rate alerts (Discord 429s) the row count
grows over time.

Fix: add migration
`CREATE INDEX notification_attempts_success ON notification_attempts (alert_id) WHERE succeeded = true;`.

#### 5.P4 `DiscordNotifier` uses `http.DefaultClient` — **Important**

`internal/notify/discord.go:48` — `client: http.DefaultClient`.
`http.DefaultClient` has no timeout and shares the process-wide
transport with all other HTTP users. Discord webhook calls can block
indefinitely on network stalls.

Fix: construct a dedicated `*http.Client` with a 15-second timeout
in `NewDiscordNotifier` as the default, matching the pattern in
`internal/ebay.BrowseClient` and `internal/ebay.OAuthTokenProvider`.
`WithHTTPClient(c *http.Client)` option already exists for overrides.

#### 5.P5 `todayUTCMidnight` recomputed per iteration in judge budget check — **Important**

`pkg/judge/worker.go:199` — `checkBudget` is called once per alert
in the `Run` loop, and each call invokes `todayUTCMidnight()` (line
199) which calls `time.Now().UTC()` and constructs a `time.Time`.
The midnight value doesn't change within a tick.

Fix: compute once at the top of `Run`; pass into `checkBudget`. Or
cache via `sync.OnceValue` if `Run` is short-lived (it is, but the
parameter pattern is clearer).

#### 5.P6 Missing composite partial indices for unextracted / unscored listings — **Nice-to-have**

`internal/store/queries.go:84` (`queryListUnextractedListings`,
filter `active = true AND component_type IS NULL`) and `queries.go:96`
(`queryListUnscoredListings`, filter `active = true AND component_type
IS NOT NULL AND score IS NULL`). Existing indices are single-column;
Postgres can bitmap-AND but a composite partial is cheaper.

Fix:
`CREATE INDEX idx_listings_unextracted ON listings (first_seen_at DESC) WHERE active = true AND component_type IS NULL;`
and the symmetric one for unscored. Both are hot paths on every
ingestion tick.

#### 5.P7 Synchronous Langfuse `Score` loop on alert-dismiss HTTP handler — **Nice-to-have**

`internal/api/handlers/alerts_ui.go:128` —
`scoreOperatorDismissValue` iterates `traceIDs` and calls
`h.deps.Langfuse.Score(ctx, traceID, ...)` synchronously per trace.
With the current `BufferedClient` this is a non-blocking enqueue, so
it's fine in practice. The risk is a future client swap that's
synchronous would block the HTTP request handler indefinitely.

Fix: detach the loop onto a background goroutine with its own
context derived from the request context. Buffered client guarantees
non-blocking but the call-site shape shouldn't depend on the client's
implementation detail.

#### 5.18 Findings investigated and rejected

Three claims from the second-pass review didn't survive verification.
Recording them here so they're not rediscovered:

**Rejected #1 — "SQL LIMIT/OFFSET pagination uses unsafe `strconv.Itoa`
concatenation".** `internal/store/postgres.go:629` uses
`strconv.Itoa(len(listArgs)-1)` to build placeholder *indices*
(`$N OFFSET $N+1`), not values. The actual values are passed as
positional pgx params at line 631 (`listArgs` built at line 620-621).
The placeholder indices are derived correctly from the same slice's
length. Not injection-prone, not off-by-one prone. The code is fine.

**Rejected #2 — "Langfuse buffered client hot path has no timeout and
can block".** `pkg/observability/langfuse/buffered_client.go:230-237`
implements drop-newest semantics:
```go
select {
case b.jobs <- job:
    b.metrics.SetDepth(len(b.jobs))
default:
    b.metrics.RecordDrop()
}
```
Non-blocking by construction. INV-0001 §"Buffer overflow is
drop-newest" already established and documented this contract.

**Rejected #3 — "No metrics for observability backend failures".**
`spt_langfuse_buffer_drops_total` exists and is documented in
CLAUDE.md ("Operators read it as 'records lost'"). Per INV-0001 the
counter is the canonical observability for buffer overflow events.
The agent didn't survey existing metrics before flagging the gap.

---

## Conclusion

**Answer:** Confirmed. The hypothesis underestimated the surface area —
the first-pass review surfaced 34 distinct issues, and a second pass
against the same commit surfaced 23 additional findings (§5) plus 3
expansions of existing §2.4/§3.1/§3.2 entries. **Net: 57 issues across
both passes** (count verified by direct enumeration of every `#### N.M`
heading; corrects an earlier draft of this Conclusion that reported
the totals as 41).

First-pass totals (§1-§4):

| Severity | Count | Lens distribution |
|---|---|---|
| Critical | 7 | architect=3, style=1, perf=2, debt=1 |
| Important | 16 | architect=3, style=5, perf=2, debt=6 |
| Nice-to-have | 11 | architect=2, style=3, perf=4, debt=2 |

Second-pass additions (§5):

| Severity | Count | Lens distribution |
|---|---|---|
| Critical | 4 | architect=2, perf=2 |
| Important | 11 | architect=4, style=4, perf=3 |
| Nice-to-have | 8 | architect=2, style=4, perf=2 |

Plus 3 false-positive claims investigated and rejected (§5.18).

**Combined totals: 11 Critical, 27 Important, 19 Nice-to-have = 57.**

The boundary violations (§1.1, §1.2) and `slog.Default()` (§2.4,
expanded in §5) are unambiguous bugs masquerading as style issues —
they should be fixed immediately. The Store interface split (§1.3)
and ComponentType registry (§4.5) are the largest refactors but
unlock the most downstream wins. The N+1 fixes (§3.1, §3.2, now
expanded to cover `RescoreAll` and `processSummary`) are the highest-
leverage perf changes — single-PR, measurable wallclock impact on
`/api/v1/ingest`.

Second-pass surfaces several issues the first pass missed because they
require deeper reading than a structural scan:

- **§5.A3 PoolSize ignored** — a *correctness* bug, not a style
  issue. Operators can't actually tune the pool. Should jump the queue.
- **§5.P1 GetAlertDetail double-fetch** — the JOIN already has the
  watch data; the second query is pure waste.
- **§5.P2 missing index for `queryHasRecentAlert`** — the partial
  index on alerts deliberately excludes notified=true, but the cooldown
  check needs exactly that subset. Sequential scan grows with history.
- **§5.A4 duplicate `stripJSONFences`** — the comment in the second
  copy acknowledges the duplication. Easy fix; high readability win.

Nothing surfaced suggests a rewrite; the architecture is sound (interface-
first design held up, the `pkg/`/`internal/` split is mostly clean, the
engine pipeline is composable). The findings are localised — every one of
them is a contained PR.

## Recommendation

**Proposed PR sequence** (top-to-bottom = ascending blast radius;
parenthesised tag is the section above):

### Wave 1 — boundary + correctness fixes (parallel, safe)

1. `pkg/extract` → `internal/metrics` adapter pattern (§1.1)
2. `pkg/extract` → drop `internal/version` import via functional option (§1.1)
3. `internal/config` → drop `pkg/observability/langfuse` import (§1.2)
4. `slog.Default()` → injected logger across all five sites (§2.4 +
   §5.S library-package additions: `pkg/judge`, `pkg/observability/langfuse`,
   `pkg/extract`)
5. Error-chain fix in 18 Huma handlers (§2.9)
6. `slog` "failed to" sweep — single mechanical PR (§2.1)
7. **NEW**: Fix `PoolSize` ignored bug in `NewPostgresStore` — silent
   correctness gap, ~10 LOC change (§5.A3)
8. **NEW**: Drop `config.AlertsConfig` from `Engine`; pass
   `time.Duration` directly (§5.A2)
9. **NEW**: Move `WithSessionID` out of `pkg/observability/langfuse`
   into a separate `pkg/session` package (§5.A6)
10. **NEW**: `internal/store/postgres.go` → drop `internal/metrics`
    import via `StoreMetricsRecorder` adapter pattern (§5.A1)

### Wave 2 — performance (parallel, low risk)

1. Cache `ListWatches` per engine tick — covers ingestion AND
   `RescoreAll` call sites (§3.1, expanded)
2. Batch alert hydration via `ListingsByIDs` + `AlertsWithNotificationStatus`
   — covers `sendBatch` AND `processSummary` (§3.2, expanded)
3. `RecomputeAllBaselines` single-SQL rewrite (§3.3)
4. `AlertsFiredByWatch` label switch to `watch_id` (§3.8)
5. **NEW**: Collapse `GetAlertDetail` to single query (expand
    `scanAlertWithListing` to populate full watch; drop the
    `GetWatch` re-fetch and the double `rows.Close()`) (§5.P1)
6. **NEW**: Add `idx_alerts_cooldown` covering index for
    `queryHasRecentAlert` — DB migration (§5.P2)
7. **NEW**: Add `notification_attempts_success` partial index for
    `queryHasSuccessfulNotification` — DB migration (§5.P3)
8. **NEW**: `DiscordNotifier` — replace `http.DefaultClient` with
    a dedicated client + 15s timeout (§5.P4)
9. **NEW**: Compute `todayUTCMidnight` once per judge tick, pass
    into `checkBudget` (§5.P5)

### Wave 3 — naming + ergonomics (sequence with §1.3)

1. Store interface split into per-entity interfaces (§1.3)
2. Drop `Get` prefix sweep, bundled with §1.3 rename (§2.3)
3. Disambiguate `RescoreAll` (§1.5)
4. Naked bool params → named modes (§2.7)

### Wave 4 — testability + scan-order safety

1. Adopt pgx scany; remove inline scan functions (§4.4)
2. Add testcontainers-backed `dbtest` build tag with PostgresStore tests (§4.1)

### Wave 5 — structural cleanup

1. Split `serve.go` into `internal/bootstrap/*` packages (§4.2)
2. Move `ProcessAlerts` and friends onto `*Engine` (§1.7)
3. Move `judgeWorker` package-level var into struct field (§1.6)

### Wave 6 — the big lift

1. ComponentType registry pattern (§4.5) — single largest refactor;
    do after waves 1-5 so the registry can take advantage of the cleaner
    Store interfaces and bootstrap separation.

### Wave 7 — polish

1. Templ viewmodel layer (§1.8) — note: moot if INV-0003 SPA
    refactor lands (see §6 of INV-0003)
2. Pre-classifier hook list (§4.8)
3. Orphan baseline auto-cleanup (§4.7)
4. Condition derivation from title (§4.9)
5. `resp := &OutputType{}` → `var resp OutputType` sweep (§2.2)
6. Misc style fixes (§2.5, §2.6, §2.8, §3.4, §3.5, §3.6, §3.7)
7. **NEW**: Magic-number constants in judge: verdict thresholds and
    MaxTokens (§5.S1, §5.S2)
8. **NEW**: Hoist duplicate `const batchSize = 200` (§5.S3)
9. **NEW**: Add `var _ LLMBackend = (*X)(nil)` compliance assertions
    on all four implementations (§5.S4)
10. **NEW**: Hoist HTTP timeout to named constant in
    `NewAnthropicBackend` (and symmetric for Ollama/OpenAI-compat)
    (§5.S6)
11. **NEW**: Drop unreachable `panic` in OTel noop meter registration
    (§5.S5)
12. **NEW**: Migrate four remaining handlers off `store.Store` to
    narrow interfaces — bundle with §1.3 work (§5.A8)
13. **NEW**: `QuotaHandler` → `QuotaProvider` interface (§5.A7)
14. **NEW**: Rename unused exports in `internal/engine/score.go`
    (`RescoreListings`, etc.) (§5.A5)
15. **NEW**: Add composite partial indices for unextracted/unscored
    listings — DB migration (§5.P6)
16. **NEW**: Detach `scoreOperatorDismissValue` loop onto background
    goroutine (§5.P7)
17. **NEW**: Move `stripJSONFences` to shared `pkg/llmutil` package
    (§5.A4)
18. **NEW**: Scheduler lock TTLs → named constants or config (§5.S8)
19. **NEW**: Add `t.Helper()` to engine test helpers (§5.S7)

Each wave is independently shippable; deploys can land between waves.
Total effort estimate (after second-pass additions): **12-16 PRs of
substance + a long tail of style sweeps**. None of the substantial
PRs should exceed ~600 LOC; most will be smaller. Two NEW critical
fixes from the second pass (PoolSize §5.A3, AlertsConfig coupling
§5.A2) should land in Wave 1 alongside the original boundary work.

**See IMPL-0020** for the concrete per-PR execution plan and
**IMPL-0021** for the dedicated ComponentType registry sub-plan
(carved out of Wave 6 because it's a 1,000+ LOC piece of work
deserving its own doc).

## References

- INV-0001 — IMPL-0019 post-merge code review findings (load-bearing
  precedents already established for `pkg/` boundary patterns)
- CLAUDE.md — self-documented warnings that this review formalises
  (eight-touchpoint ComponentType, scan-order brittleness, orphan
  baseline cleanup, condition-from-title follow-up)
- Uber Go Style Guide — primary reference for §2 findings
- `go-development:go` skill — applied to every §2 and §3 finding
- DESIGN-0001 — original architecture (interface-first design held up)
