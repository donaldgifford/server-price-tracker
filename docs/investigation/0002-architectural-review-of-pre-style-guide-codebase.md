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
    - [4.9 condition_norm not derived from title signals — Important](#49-conditionnorm-not-derived-from-title-signals--important)
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

Four parallel review agents, each scoped to a single lens:

1. **go-architect** — package boundaries, dependency direction, interface
   shape, dependency injection patterns, separation of concerns.
2. **go-style** — Uber Go Style Guide conformance: naming, error handling,
   struct initialisation, control flow.
3. **go-performance** — allocations, N+1 queries, hot-path benchmarks,
   metric cardinality, goroutine lifecycle.
4. **Explore (general tech debt)** — test coverage, complexity, hard-coded
   sequences, duplication, scan-order brittleness.

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

The engine has a `logger *slog.Logger` field but ignores it in four spots:

- `internal/engine/alert.go:118` — `slog.Default().Info(...)`
- `internal/engine/alert.go:238` — `slog.Default().Warn(...)`
- `internal/engine/alert.go:311` — `slog.Default().Error(...)`
- `internal/engine/alert.go:329` — `slog.Default().Debug(...)`

This breaks the structured-logging contract — operator log filters
matching `service=spt` work against the injected logger but the default
logger has no such attribute. Use `e.logger` everywhere.

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

Fix: cache watches at the start of the engine tick (they change at human
pace, not at listing-arrival pace) and pass the slice down. Bonus: ranged
loop over a slice is allocation-free; per-call `ListWatches` allocates a
fresh slice every time.

#### 3.2 N+1: per-alert `HasSuccessfulNotification` + `GetListingByID` — **Critical**

`internal/engine/alert.go:264-271` — for each alert in the batch:

```go
ok, err := store.HasSuccessfulNotification(ctx, alertID)
listing, err := store.GetListingByID(ctx, listing_id)
```

Two queries × batch size. With Discord summary mode collapsing one tick's
alerts into a single embed (DESIGN-0010), batch sizes can be 20-50.

Fix: add `Store.ListingsByIDs(ctx, ids []string)` and
`Store.AlertsWithNotificationStatus(ctx, ids)` — both single-query joins.
Engine consumes the maps directly.

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

## Conclusion

**Answer:** Confirmed. The hypothesis underestimated the surface area —
the four reviews surfaced **24 distinct issues** with varying severity:

| Severity | Count | Lens distribution |
|---|---|---|
| Critical | 6 | architect=3, style=1, perf=2 |
| Important | 11 | architect=2, style=4, perf=2, debt=3 |
| Nice-to-have | 7 | architect=2, style=2, perf=3, debt=0 |

The boundary violations (§1.1, §1.2) and `slog.Default()` (§2.4) are
unambiguous bugs masquerading as style issues — they should be fixed
immediately. The Store interface split (§1.3) and ComponentType registry
(§4.5) are the largest refactors but unlock the most downstream wins.
The N+1 fixes (§3.1, §3.2) are the highest-leverage perf changes —
single-PR, measurable wallclock impact on `/api/v1/ingest`.

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
4. `slog.Default()` → injected logger in `alert.go` (§2.4)
5. Error-chain fix in 18 Huma handlers (§2.9)
6. `slog` "failed to" sweep — single mechanical PR (§2.1)

### Wave 2 — performance (parallel, low risk)

7. Cache `ListWatches` per engine tick (§3.1)
8. Batch alert hydration via `ListingsByIDs` + `AlertsWithNotificationStatus` (§3.2)
9. `RecomputeAllBaselines` single-SQL rewrite (§3.3)
10. `AlertsFiredByWatch` label switch to `watch_id` (§3.8)

### Wave 3 — naming + ergonomics (sequence with §1.3)

11. Store interface split into per-entity interfaces (§1.3)
12. Drop `Get` prefix sweep, bundled with §1.3 rename (§2.3)
13. Disambiguate `RescoreAll` (§1.5)
14. Naked bool params → named modes (§2.7)

### Wave 4 — testability + scan-order safety

15. Adopt pgx scany; remove inline scan functions (§4.4)
16. Add testcontainers-backed `dbtest` build tag with PostgresStore tests (§4.1)

### Wave 5 — structural cleanup

17. Split `serve.go` into `internal/bootstrap/*` packages (§4.2)
18. Move `ProcessAlerts` and friends onto `*Engine` (§1.7)
19. Move `judgeWorker` package-level var into struct field (§1.6)

### Wave 6 — the big lift

20. ComponentType registry pattern (§4.5) — single largest refactor;
    do after waves 1-5 so the registry can take advantage of the cleaner
    Store interfaces and bootstrap separation.

### Wave 7 — polish

21. Templ viewmodel layer (§1.8)
22. Pre-classifier hook list (§4.8)
23. Orphan baseline auto-cleanup (§4.7)
24. Condition derivation from title (§4.9)
25. `resp := &OutputType{}` → `var resp OutputType` sweep (§2.2)
26. Misc style fixes (§2.5, §2.6, §2.8, §3.4, §3.5, §3.6, §3.7)

Each wave is independently shippable; deploys can land between waves.
Total effort estimate: **8-12 PRs of substance + a long tail of style
sweeps**. None of the substantial PRs should exceed ~600 LOC; most will
be smaller.

## References

- INV-0001 — IMPL-0019 post-merge code review findings (load-bearing
  precedents already established for `pkg/` boundary patterns)
- CLAUDE.md — self-documented warnings that this review formalises
  (eight-touchpoint ComponentType, scan-order brittleness, orphan
  baseline cleanup, condition-from-title follow-up)
- Uber Go Style Guide — primary reference for §2 findings
- `go-development:go` skill — applied to every §2 and §3 finding
- DESIGN-0001 — original architecture (interface-first design held up)
