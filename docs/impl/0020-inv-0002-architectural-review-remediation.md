---
id: IMPL-0020
title: "INV-0002 architectural review remediation"
status: Draft
author: Donald Gifford
created: 2026-05-15
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0020: INV-0002 architectural review remediation

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-05-15

<!--toc:start-->
- [Objective](#objective)
  - [Phase / PR map](#phase--pr-map)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Critical findings](#phase-1-critical-findings)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: Important findings](#phase-2-important-findings)
    - [Phase 2A: Architecture](#phase-2a-architecture)
      - [Tasks](#tasks-1)
      - [Success Criteria](#success-criteria-1)
    - [Phase 2B: Performance](#phase-2b-performance)
      - [Tasks](#tasks-2)
      - [Success Criteria](#success-criteria-2)
    - [Phase 2C: Style sweeps](#phase-2c-style-sweeps)
      - [Tasks](#tasks-3)
      - [Success Criteria](#success-criteria-3)
    - [Phase 2D: Tech debt](#phase-2d-tech-debt)
      - [Tasks](#tasks-4)
      - [Success Criteria](#success-criteria-4)
  - [Phase 3: Nice-to-have findings](#phase-3-nice-to-have-findings)
    - [Tasks](#tasks-5)
    - [Success Criteria](#success-criteria-5)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
  - [Resolved](#resolved)
  - [Still open](#still-open)
- [References](#references)
<!--toc:end-->

## Objective

Remediate the 57 findings catalogued in INV-0002 (architectural review
of the pre-style-guide codebase). Phases are tiered by severity:
every Critical fix lands before any Important fix; every Important fix
lands before any Nice-to-have. Phase 2 (Important) is split into four
sub-phases by domain so each ships as a coherent, reviewable PR.

**Implements:** INV-0002 (Architectural review of pre-style-guide
codebase).

**Sister doc:** **IMPL-0021** (ComponentType registry pattern) —
carved out of this IMPL because the §4.5 work is large enough
(~1,000-1,500 LOC) to warrant its own phased plan.

This doc is the execution plan. INV-0002 is the source of truth for
*what* each finding is and *why* it matters — IMPL-0020 catalogues
*when* each is fixed and *how* the work is sequenced.

### Phase / PR map

| Phase | Title | Findings | PR # |
|---|---|---|---|
| 1 | Critical findings | 11 | 1 |
| 2A | Important — Architecture | 7 | 2 |
| 2B | Important — Performance | 5 | 3 |
| 2C | Important — Style sweeps | 9 | 4 |
| 2D | Important — Tech debt | 5 | 5 |
| 3 | Nice-to-have findings | 19 | 6 |

6 PRs from IMPL-0020 + however many IMPL-0021 produces for the
ComponentType registry.

## Scope

### In Scope

- All 11 Critical findings from INV-0002 (§1-§5)
- 26 of the 27 Important findings (the ComponentType registry §4.5
  moves to IMPL-0021)
- All 19 Nice-to-have findings from INV-0002
- DB migrations required by §5.P2, §5.P3, §5.P6
- Test additions required by changes in scope
- CLAUDE.md updates that follow from the architectural changes

### Out of Scope

- **§4.5 ComponentType registry** — moved to IMPL-0021 (own phased
  plan; tracked there with phases for foundation, per-type migration,
  and cleanup)
- INV-0003 work (app-centric refactor, Meilisearch UI, OTel RED-style,
  Langfuse iteration loop) — separate IMPL doc
- INV-0004 work (sdk-booty-sh migration) — separate IMPL doc
- Any new features beyond the remediation surface
- Performance benchmarking beyond what's needed to validate the
  Critical N+1 / query-collapse fixes

## Implementation Phases

Each phase ships as one PR. Within a phase, tasks are ordered so
earlier tasks set the foundation for later tasks in the same phase.

The phase ordering — Critical → Important → Nice-to-have — is
deliberately conservative: even if Phase 2 or Phase 3 slip, the
production-impact bugs (correctness, boundary violations, hot-path
N+1) are already deployed.

---

### Phase 1: Critical findings

**Goal:** fix every Critical finding from INV-0002 (boundary
violations, correctness gaps, hot-path N+1s, missing indices,
scan-order brittleness, fat-interface foundation work).

Tasks are ordered so the foundation pieces (boundary cleanup, logger
injection, Store split, scan-order safety) land before the consumers
of those changes (query optimisation, N+1 batching).

#### Tasks

**1.1 Boundary + correctness cleanup (foundation):**

- [ ] **(INV §1.1)** Remove `internal/metrics` import from
      `pkg/extract/extractor.go:16` — define a `TokenMetricsRecorder`
      interface in `pkg/extract`, implement adapter in
      `internal/metrics`, wire in `serve.go`. Mirror the
      `BufferMetrics` adapter pattern (INV-0001).
- [ ] **(INV §1.1)** Remove `internal/version` import from
      `pkg/extract/langfuse_backend.go:10` — pass the version string
      via a functional option (`WithVersion(string)`).
- [ ] **(INV §5.A3)** Fix `NewPostgresStore` silently ignoring
      `DatabaseConfig.PoolSize` — accept `maxConns int` (or functional
      option); wire `cfg.Database.PoolSize` in `serve.go`.
- [ ] **(INV §5.A2)** Drop `config.AlertsConfig` field from `Engine`;
      replace with `reAlertsCooldown time.Duration` + a
      `WithReAlertsCooldown(d time.Duration)` option. Caller passes
      `cfg.Alerts.ReAlertsCooldown` directly.

**1.2 Logger injection sweep:**

- [ ] **(INV §2.4)** Replace `slog.Default()` with the injected
      logger at all five sites:
  - `internal/engine/alert.go:118,238,311,329` (use `e.logger`; will
    require §1.7 ProcessAlerts → method conversion to access; do
    that conversion as part of this task, even though §1.7 itself
    is a Nice-to-have)
  - `pkg/judge/worker.go:114` (require non-nil; let caller pass
    `slog.Default()` explicitly if desired)
  - `pkg/observability/langfuse/buffered_client.go:111` (same)
  - `pkg/extract/extractor.go:97` (same)

**1.3 Store interface split (foundation for Phase 2C naming sweep):**

- [ ] **(INV §1.3)** Split `internal/store/store.go` 45-method
      `Store` interface into per-entity contracts:
  - `WatchStore` (CRUD + filter helpers)
  - `ListingStore` (CRUD + query + active-flag toggles)
  - `BaselineStore` (recompute, lookup, refresh)
  - `AlertStore` (create, mark notified, dismiss, restore, review query)
  - `QueueStore` (extraction queue: enqueue, claim, complete)
  - `JobStore` (scheduler state, job runs)
  - `RateLimitStore` (token bucket + daily quota)
  - `JudgeStore` (judge scores, recent un-judged alerts)
- [ ] Keep `Store` as a *union* of the eight new interfaces with an
      explicit deprecation doc-comment so `staticcheck SA1019` flags
      any new code that takes it:

      ```go
      // Store is the legacy union of every per-entity store.
      //
      // Deprecated: new consumers must depend on the narrow per-entity
      // interface they actually need (WatchStore, ListingStore, ...).
      // The union is retained for backwards compatibility while
      // remaining handlers and engine call-sites migrate. Tracked
      // for removal in IMPL-0020 Phase 3 §5.A8.
      type Store interface {
          WatchStore
          ListingStore
          BaselineStore
          AlertStore
          QueueStore
          JobStore
          RateLimitStore
          JudgeStore
      }
      ```
- [ ] Update each consumer in scope for Phase 1 to depend on the
      narrow interface. `PostgresStore` keeps every method (satisfies
      all interfaces). Handlers still on the wide `Store` (listings,
      watches, baselines, health — §5.A8 in Phase 3) inherit the
      deprecation warning but don't block this phase.
- [ ] Update `MockStore` generation: `make mocks` regenerates per-
      interface mocks.
- [ ] Add `staticcheck` exemption (or no-op) for `internal/store`
      itself — the union's own declaration is allowed to reference it.

**1.4 Disambiguation:**

- [ ] **(INV §1.5)** Rename `Store.RescoreAll` →
      `Store.RecomputeListingScores` (SQL-side recompute); keep
      `Engine.RescoreAll` (user-facing operation). Update
      `/api/v1/rescore` handler routing if needed (verify it points
      at the engine method, not the store method).

**1.5 Scan-order safety (foundation for §5.P1 expanded scan):**

- [ ] **(INV §4.4)** Adopt `github.com/georgysavva/scany/v2/pgxscan`
      as the scanner library. Add struct tags to `domain.Listing`,
      `domain.Watch`, `domain.Alert`, `domain.AlertWithListing`,
      `domain.AlertDetail`, etc.
- [ ] Replace inline `scanListing` / `scanListingRow` /
      `scanAlertWithListing` and friends with `pgxscan.ScanOne(...)`
      / `pgxscan.ScanAll(...)`.
- [ ] Remove the CLAUDE.md "when adding a column to listings" warning
      (now structurally enforced by struct tags).

**1.6 Query collapse (depends on 1.5 for safe scan expansion):**

- [ ] **(INV §5.P1)** Collapse `GetAlertDetail`
      (`internal/store/postgres.go:685-728`) to a single query:
  - Expand `alertReviewSelectColumns` (or its scan) to populate the
    full `domain.Watch` from the JOIN result.
  - Drop the second `s.GetWatch(ctx, row.Alert.WatchID)` call at
    line 713.
  - Drop the explicit `rows.Close()` at line 711 (`defer` already
    handles it).

**1.7 DB index migration:**

- [ ] **(INV §5.P2)** Add migration
      `015_add_alert_cooldown_index.sql`:

      ```sql
      CREATE INDEX idx_alerts_cooldown ON alerts (watch_id, listing_id, notified_at DESC)
          WHERE notified = true;
      ```
- [ ] Mirror to embedded copy `internal/store/migrations/`.
- [ ] Run `make migrate` against local Postgres; verify index appears
      via `\d alerts`.

**1.8 N+1 batching:**

- [ ] **(INV §3.1)** Cache `ListWatches` at the start of the engine
      tick instead of calling per-listing. Pass the slice into
      `evaluateAlertsForListing`. Covers both call sites
      (`RunIngestion` and `RescoreAll`).
- [ ] **(INV §3.2)** Add `Store.ListingsByIDs(ctx, ids []string)` (uses
      `WHERE id = ANY($1)`) and
      `Store.AlertsWithNotificationStatus(ctx, ids []string)` (single
      JOIN).
- [ ] Refactor `sendBatch` and `processSummary` to hydrate via the new
      batch methods.

**1.9 Tests + telemetry verification:**

- [ ] All new interfaces have generated mocks (`make mocks`).
- [ ] Add table-driven tests for the new `ListingsByIDs` /
      `AlertsWithNotificationStatus` methods (testcontainers-backed,
      see Q4 below — falls back to mock-based until Phase 2 lands
      `dbtest` build tag).
- [ ] Add a regression test confirming `evaluateAlertsForListing`
      doesn't call `ListWatches` once the cache is in place (mock
      assertion: `ListWatches` called exactly once per tick).
- [ ] Add a benchmark or table-driven duration assertion confirming
      `GetAlertDetail` issues exactly one DB query (count via
      pgx-level tracing or a `QueryRecorder` test double).
- [ ] CLAUDE.md updates: remove scan-order warning; note the new
      per-entity Store interfaces.

#### Success Criteria

- `make lint` passes (Uber Go Style Guide via golangci-lint).
- `make test` passes.
- `make build` produces both binaries (`server-price-tracker` and
  `spt`).
- `pkg/extract` has zero `internal/` imports (grep proof:
  `! rg "internal/" pkg/extract/`).
- `internal/store/store.go` defines ≥8 per-entity interfaces; the
  monolithic `Store` interface either survives as an alias union or
  is removed entirely (see Q3).
- All 5 `slog.Default()` sites use injected loggers; the regression
  is caught by a linter rule (forbid `slog.Default` outside
  `cmd/spt`) committed as part of this phase.
- Migration `015_add_alert_cooldown_index.sql` applied and indexed
  in `schema_migrations`.
- `GetAlertDetail` makes exactly 2 DB queries (the main JOIN +
  `listNotificationAttempts`), down from 3.
- `RescoreAll` makes 1 `ListWatches` call per invocation, not 1 per
  listing.
- `Store.RecomputeListingScores` is callable; `Engine.RescoreAll`
  unchanged.
- Operators tuning `database.pool_size` see actual effect (manual
  verify via `pgxpool.Stats()`).

---

### Phase 2: Important findings

**Goal:** address 26 of the 27 Important findings (the 27th, §4.5
ComponentType registry, lives in IMPL-0021).

Split into four sub-phases (2A-2D) by domain. Each sub-phase ships
as a separate PR — keeps reviewability manageable. Sub-phases are
independent of each other once Phase 1 has landed; they can run in
parallel if the operator wants to land them out of order.

---

#### Phase 2A: Architecture

7 findings. Boundary cleanup, dependency direction, function-vs-
method placement.

##### Tasks

- [ ] **(INV §1.2)** Drop `internal/config` import on
      `pkg/observability/langfuse`. Define `ModelCostConfig` locally
      in `internal/config`; convert at the boundary in `serve.go`.
- [ ] **(INV §1.4)** Define `EbayPaginator` and `EbayAnalytics`
      interfaces in `internal/engine`; replace concrete fields on
      `Engine` struct (`engine.go:31-32`).
- [ ] **(INV §1.6)** Move `judgeWorker` package-level var from
      `serve.go:578` into a `serveState` struct (or local variable
      closed over by the signal handler). Change `buildEngine` to
      return `(*engine.Engine, *engine.Scheduler, *judge.Worker)`.
- [ ] **(INV §5.A1)** Drop `internal/metrics` import from
      `internal/store/postgres.go`. Define `StoreMetricsRecorder`
      interface; adapter in `internal/metrics`; inject via
      `NewPostgresStore`.
- [ ] **(INV §5.A4)** Extract duplicate `stripJSONFences` to a new
      shared package (`pkg/llmutil/json.go` or
      `pkg/extract/response`); delete the duplicate from
      `pkg/judge/llm_judge.go`.
- [ ] **(INV §5.A5)** Rename `internal/engine/score.go` package-
      level exports (`RescoreListings`, `RescoreByProductKey`,
      package-level `RescoreAll`) to unexported names.
- [ ] **(INV §5.A6)** Create `pkg/session` (or `internal/sessionctx`)
      package for `WithSessionID` / `SessionIDFromContext`. Move the
      symbols; update `pkg/observability/langfuse` to *read* the
      context key but no longer *own* setting it. Remove
      `pkg/observability/langfuse` import from
      `internal/engine/scheduler.go`.
- [ ] CLAUDE.md updates: note the new `pkg/session` and
      `pkg/llmutil` packages; note that `internal/store` no longer
      pulls in Prometheus.

##### Success Criteria

- `make lint` and `make test` pass.
- Zero `internal/` imports in any `pkg/` package (`! rg "internal/" pkg/`).
- `pkg/observability/langfuse` has no `internal/config` dependency.
- `internal/store/postgres.go` has no `internal/metrics` import.
- `internal/engine/scheduler.go` does not import
  `pkg/observability/langfuse`.
- `stripJSONFences` exists in exactly one location.

---

#### Phase 2B: Performance

5 findings. SQL rewrites, missing indices, HTTP client hardening,
hot-path allocation cleanup.

##### Tasks

- [ ] **(INV §3.3)** Rewrite `RecomputeAllBaselines`
      (`postgres.go:384-409`) as a single SQL statement using
      `INSERT ... SELECT ... ON CONFLICT DO UPDATE` with a window
      function or CTE that computes percentiles per group.
- [ ] **(INV §3.8)** Switch `AlertsFiredByWatch` label from
      `watch_name` to `watch_id` (bounded UUID). Recreate the join
      in Grafana via a recording rule, or accept that ops dashboards
      will need to query the DB for the human-readable name.
- [ ] **(INV §5.P3)** Add migration
      `016_add_notification_success_index.sql`:

      ```sql
      CREATE INDEX notification_attempts_success ON notification_attempts (alert_id)
          WHERE succeeded = true;
      ```
- [ ] **(INV §5.P4)** Replace `http.DefaultClient` in
      `internal/notify/discord.go:48` with a dedicated `*http.Client`
      having a 15s timeout. Existing `WithHTTPClient` option still
      lets callers override.
- [ ] **(INV §5.P5)** Compute `todayUTCMidnight` once at the top of
      the judge `Run` loop; pass into `checkBudget`. Eliminates
      per-iteration allocation.

##### Success Criteria

- `make lint` and `make test` pass.
- `RecomputeAllBaselines` completes in <2s on the production dataset
  (down from ~15s).
- `spt_alerts_fired_total` is labelled by `watch_id`, not
  `watch_name`. Cardinality on `/metrics` measured before/after to
  prove the bound.
- Migration 016 applied; `notification_attempts_success` visible
  via `\d notification_attempts`.
- Discord webhook calls observably bounded by 15s (manual test:
  block port; confirm the call returns within ~15s with an error
  instead of hanging).
- `todayUTCMidnight` called exactly once per judge tick (mock or
  trace-level assertion).

---

#### Phase 2C: Style sweeps

9 findings. Mechanical refactors — the kind of PR where the diff is
large but the change per file is small.

##### Tasks

- [ ] **(INV §2.1)** "failed to" sweep — mechanical PR-task:
      `rg "failed to" --type go` and rewrite each call site as a
      verb-phrase ("start scheduler", "scan alert", "call LLM
      backend", etc.).
- [ ] **(INV §2.3)** Drop `Get` prefix on the per-entity store
      methods (`GetWatchByID` → `WatchByID`, etc.). The interfaces
      were defined in Phase 1 §1.3; the rename happens here so the
      Phase 1 PR didn't have to absorb the churn.
- [ ] **(INV §2.5)** Rename error types: `ValidationFailure` →
      `ValidationError`, `ParseFailure` → `ParseError` in
      `pkg/extract/types.go`.
- [ ] **(INV §2.7)** Replace naked bool parameters at
      `internal/engine/engine.go:282,314,394` with named typed enums
      (`DryRunMode` / `LiveMode`) or split into separate methods.
- [ ] **(INV §2.9)** Fix error-chain loss in 18 Huma handlers:
      replace `huma.Error500("desc: " + err.Error())` with
      `huma.Error500InternalServerError("desc", err)`.
- [ ] **(INV §5.S1)** Define `verdictDealThreshold = 0.7` and
      `verdictNoiseThreshold = 0.3` constants near `verdictBucket`
      in `pkg/judge/worker.go`.
- [ ] **(INV §5.S2)** Define `judgeMaxTokens = 256` constant in
      `pkg/judge/llm_judge.go`.
- [ ] **(INV §5.S3)** Hoist duplicate `const batchSize = 200` to a
      single location in `internal/engine` (delete from
      `score.go:109`; keep the one in `engine.go:247`).
- [ ] **(INV §5.S4)** Add `var _ LLMBackend = (*X)(nil)` compliance
      assertions on `AnthropicBackend`, `OllamaBackend`,
      `OpenAICompatBackend`, `LangfuseBackend`.

##### Success Criteria

- `make lint` and `make test` pass.
- `rg "failed to" --type go` returns zero results outside CLI
  output and `_test.go` strings.
- No `GetX` methods on `internal/store` types.
- All Huma handlers return error chains intact — `errors.Is/As`
  works against the original error from any returned 500.
- Magic numbers `0.7`, `0.3`, `256`, `200` no longer appear as bare
  literals in `pkg/judge` or `internal/engine`.
- `go vet ./...` confirms the four new interface-compliance
  assertions hold.

---

#### Phase 2D: Tech debt

5 findings (the ComponentType registry §4.5 moves to IMPL-0021).

##### Tasks

- [ ] **(INV §4.1)** Add testcontainers-backed Postgres tests gated
      behind `//go:build dbtest`. Target ≥80% coverage on
      `PostgresStore` methods (initially the most critical: alert
      review query, baseline recompute, listing CRUD, queue
      lifecycle). The fast `make test` path stays mock-only;
      `make test-db` runs the new tag.
- [ ] **(INV §4.2)** Split `cmd/server-price-tracker/cmd/serve.go`
      (677 LOC) into `internal/bootstrap/*` packages:
  - `internal/bootstrap/store.go` (DB connect + migrate)
  - `internal/bootstrap/backend.go` (LLM backend selection)
  - `internal/bootstrap/observability.go` (OTel + Langfuse init)
  - `internal/bootstrap/scheduler.go` (scheduler + worker wiring)
  - `internal/bootstrap/lifecycle.go` (signal handling + shutdown)
- [ ] **(INV §4.3)** Add a lint rule forbidding `slog.Default()` /
      `fmt.Println` outside `cmd/spt` and `*_test.go` files (Phase 1
      §2.4 already removed the existing violations; this finalises
      enforcement via `forbidigo`).
- [ ] **(INV §4.7)** Auto-cleanup orphan baselines: extend
      `recompute_baseline` (or the SQL function it calls) to
      `DELETE FROM price_baselines WHERE product_key NOT IN (SELECT
      DISTINCT product_key FROM listings WHERE active = true AND
      product_key IS NOT NULL)` as a final step. Remove the manual-
      cleanup warning from CLAUDE.md.
- [ ] **(INV §4.9)** Derive `condition_norm` from title signals
      ("FOR PARTS", "for parts", "as-is", "untested") in the
      normaliser. Re-run the normaliser over historical listings or
      schedule a one-shot backfill.
- [ ] CLAUDE.md updates: remove the orphan-baseline cleanup
      warning; remove the condition-from-title follow-up note; note
      the new `internal/bootstrap/*` structure.

##### Success Criteria

- `make lint` and `make test` pass.
- `make test-db` (new build-tag target) passes against a local
  testcontainers Postgres.
- `cmd/server-price-tracker/cmd/serve.go` is ≤200 LOC (down from
  677); the boot path is composed from `internal/bootstrap/*`.
- Forbidigo lint rule rejects new `slog.Default()` outside
  `cmd/spt/` and `*_test.go`.
- `recompute_baseline` deletes orphan rows automatically; no
  manual cleanup needed after a normaliser update.
- Listings with "FOR PARTS"-style title signals have
  `condition_norm` correctly populated (verify via `SELECT
  condition_norm, count(*) ... GROUP BY 1`).

---

### Phase 3: Nice-to-have findings

**Goal:** the 19 Nice-to-have polish items. None block anything; this
phase is a "cleanup PR" that benefits from being batched so the
review and merge cost amortises.

#### Tasks

**3.1 Architecture polish:**

- [ ] **(INV §1.7)** Move `ProcessAlerts` from package-level
      function to method on `*Engine` (already partially done by
      Phase 1 §2.4 logger-injection work; finalise here).
- [ ] **(INV §1.8)** Mirror handler DTOs into `internal/api/web/
      viewmodels`; convert at the handler edge. *Note: this may
      become moot if INV-0003 SPA refactor lands first — flag with
      operator before doing the work.*
- [ ] **(INV §5.A7)** Define `QuotaProvider` interface in
      `internal/api/handlers/quota.go`; accept it in
      `NewQuotaHandler` instead of `*ebay.RateLimiter`.
- [ ] **(INV §5.A8)** Convert the four remaining wide-Store handlers
      to narrow per-entity interfaces:
      `internal/api/handlers/listings.go:15` (`ListingsStore`),
      `watches.go:15` (`WatchStore`),
      `baselines.go:15` (`BaselinesStore`),
      `health.go:15` (`HealthStore`).

**3.2 Style polish:**

- [ ] **(INV §2.2)** `resp := &OutputType{}` → `var resp OutputType`
      sweep across 13 Huma handlers.
- [ ] **(INV §2.6)** Drop `judge.NewWorker:` redundant prefix from
      `errors.New` strings in `pkg/judge/worker.go:92,95`.
- [ ] **(INV §2.8)** Replace `fmt.Sprintf("%d", ...)` with
      `strconv.Itoa(...)` in `cmd/spt/output.go:71`.
- [ ] **(INV §5.S5)** Drop unreachable `panic` in OTel noop meter
      registration at `internal/engine/meter.go:31` and
      `pkg/extract/meter.go:41`. Return the noop histogram directly.
- [ ] **(INV §5.S6)** Define HTTP-timeout constants:
      `defaultAnthropicTimeout = 60 * time.Second` (and symmetric
      for Ollama/OpenAI-compat).
- [ ] **(INV §5.S7)** Add `t *testing.T` param and `t.Helper()` call
      to `expectCountMethods` and `newTestEngine` in
      `internal/engine/engine_test.go:39,44`.
- [ ] **(INV §5.S8)** Hoist scheduler lock TTLs (`30*time.Minute`,
      `60*time.Minute`) in `internal/engine/scheduler.go` to named
      constants or config values.

**3.3 Performance polish:**

- [ ] **(INV §3.4)** Pre-size `parts := make([]string, 0, len(specs))`
      in `formatItemSpecifics` (`pkg/extract/prompts.go:417`).
- [ ] **(INV §3.5)** Replace `json.Unmarshal([]byte(content), &out)`
      with `json.NewDecoder(strings.NewReader(content)).Decode(&out)`
      in `pkg/extract/extractor.go:289`.
- [ ] **(INV §3.6)** Pool `bytes.Buffer` instances in the prompt
      builders (`pkg/extract/prompts.go:360,378,397`) via
      `sync.Pool[*bytes.Buffer]`. Defer if profiling shows it isn't
      hot — current 30-50 extractions/day makes this academic
      (verify before / after via `go test -bench`).
- [ ] **(INV §3.7)** Pre-compute a `[600]string` array indexed by
      HTTP status code in `internal/metrics/metrics.go:52`; replace
      `strconv.Itoa` per-request with array lookup.
- [ ] **(INV §5.P6)** Add migration
      `017_add_listings_composite_partial_indices.sql`:

      ```sql
      CREATE INDEX idx_listings_unextracted ON listings (first_seen_at DESC)
          WHERE active = true AND component_type IS NULL;
      CREATE INDEX idx_listings_unscored ON listings (first_seen_at DESC)
          WHERE active = true AND component_type IS NOT NULL AND score IS NULL;
      ```
- [ ] **(INV §5.P7)** Detach `scoreOperatorDismissValue` loop
      (`internal/api/handlers/alerts_ui.go:128`) onto a background
      goroutine; derive its context from the request context with
      `context.WithoutCancel`. Note: with the existing buffered
      Langfuse client this is defensive (the client is non-blocking);
      the fix protects against future client swaps.

**3.4 Tech debt polish:**

- [ ] **(INV §4.6)** Audit `internal/config/config.go` (22 structs in
      461 LOC). Collapse single-field wrapper structs into their
      parents. Target: ≤350 LOC, ≤15 structs (subject to taste).
- [ ] **(INV §4.8)** Refactor pre-classification hooks in
      `(*LLMExtractor).ClassifyAndExtract` from a hard-coded
      three-call sequence to a `[]PreClassifier` slice ranged in
      order. `Insert(idx, hook)` becomes trivial.

**3.5 Tests + verification:**

- [ ] All §3.1-3.4 changes have unit-test coverage where applicable.
- [ ] CLAUDE.md updates: note the `viewmodels` layer (if §1.8
      landed); note the pre-classifier slice (§4.8).

#### Success Criteria

- `make lint` and `make test` pass.
- `make test-db` passes (from Phase 2).
- All 19 INV-0002 Nice-to-have findings are checked off above.
- INV-0002 status updated to "Concluded".
- The follow-up INV docs (INV-0003 app-centric refactor, INV-0004
  sdk-booty-sh migration) become the next units of work.

---

## Dependencies

- **Phase 1 → Phase 2C:** Phase 2C's `Get`-prefix sweep (§2.3)
  depends on Phase 1's Store interface split (§1.3) because the
  methods being renamed are the interface methods.
- **Phase 1 → Phase 2D:** Phase 2D's testcontainers `dbtest` build
  tag (§4.1) builds on Phase 1's pgx scany migration (§4.4) — the
  struct-tag-driven scanner is what the new tests will exercise.
- **Phase 1 → Phase 3:** Phase 3's `ProcessAlerts` → method move
  (§1.7) was partially done by Phase 1's logger-injection work; the
  finalisation is a cleanup task.
- **Phase 1 + Phase 2 → Phase 3:** Phase 3's narrow-interface
  handler migration (§5.A8) uses the interfaces defined by Phase 1
  (§1.3) and migrates the four remaining handlers (listings, watches,
  baselines, health) off the deprecated `Store` union to their narrow
  per-entity equivalents. Once this lands, the `Store` union has zero
  callers outside its declaration; Phase 3 finishes by deleting the
  union entirely.
- **External:** IMPL-0021 (ComponentType registry) is independent of
  every Phase except for Phase 1's Store split (it consumes the
  narrow store interfaces). It can land any time after Phase 1
  completes; sequencing relative to Phase 2A-2D is operator's
  choice.
- **Phase 2 internal:** sub-phases 2A, 2B, 2C, 2D are independent
  once Phase 1 has landed. They can run in any order or in parallel.
  Recommended order: 2A (architecture) → 2B (performance) → 2C
  (style sweeps) → 2D (tech debt), but only because the architectural
  cleanup makes the performance work easier to land safely.

## Open Questions

### Resolved

- **Q1 — Phase 2 PR size (RESOLVED 2026-05-15).** Split Phase 2 into
  2A (architecture) / 2B (performance) / 2C (style) / 2D (tech
  debt). Each ships as its own PR. Done — see updated phase
  structure above.
- **Q2 — Critical-finding count discrepancy (RESOLVED 2026-05-15).**
  Fix INV-0002's Conclusion table to match the actual catalog
  (11 C / 27 I / 19 N = 57). Filed as part of the same commit as
  this IMPL update.
- **Q3 — Store interface as union? (RESOLVED 2026-05-15).** Keep the
  union as a deprecated alias with a `// Deprecated:` doc-comment so
  `staticcheck SA1019` flags any new code that takes it. Phase 3
  §5.A8 migrates the four remaining wide-`Store` consumers to narrow
  interfaces; the union can be deleted entirely at the end of Phase 3
  once call-site count is zero. Phase 1 §1.3 task updated to reflect
  this.
- **Q5 — ComponentType registry scope (RESOLVED 2026-05-15).** §4.5
  moves to **IMPL-0021** (dedicated phased doc). Out of scope for
  IMPL-0020 Phase 2D. IMPL-0021 can land any time after IMPL-0020
  Phase 1 completes (it consumes the narrow store interfaces).
- **Q8 — Phase 1 within-phase ordering (RESOLVED 2026-05-15).**
  Foundation-first order stays as proposed. Big refactors land
  before the bug fixes that depend on (or might be replaced by)
  them — premature bug-fix work risks being deleted by the
  refactor it depends on.

### Still open

- **Q4 — Testcontainers vs sqlmock for Phase 1 §1.8 tests.** Phase 2D
  §4.1 adds testcontainers-backed `PostgresStore` tests gated behind
  `dbtest`. Phase 1's new batch methods (`ListingsByIDs`,
  `AlertsWithNotificationStatus`) need test coverage too. Should
  Phase 1 ship the testcontainers harness ahead of Phase 2, or use
  mock-based tests in Phase 1 and add testcontainers in Phase 2D?
  Recommend: Phase 1 uses mock-based tests for the new batch methods;
  Phase 2D retroactively adds testcontainers tests as part of §4.1.
  Keeps Phase 1 scope focused on Critical fixes.
- **Q6 — Migration numbering.** This IMPL proposes 015 (cooldown
  index), 016 (notification success index), 017 (composite partial
  indices for listings). If any other in-flight work claims these
  slots, renumber here.
- **Q7 — CLAUDE.md update granularity.** Each phase updates CLAUDE.md
  in-flight (as tasks remove warnings or note new structure). Is a
  single end-of-phase summary commit preferable to inline updates?
  Inline keeps the doc in sync with the code at each task completion;
  end-of-phase keeps history cleaner. Recommend inline (matches
  existing convention from IMPL-0017, IMPL-0018, IMPL-0019).
- **Q9 — Lint rule for `slog.Default()` enforcement.** Phase 2D §4.3
  commits a lint rule forbidding `slog.Default()` outside `cmd/spt`
  and `*_test.go`. Use `forbidigo`. Are there any other legitimate
  callers to grandfather? Recommend: `forbidigo` with an exemption
  for `cmd/spt/` and `*_test.go` files only.
- **Q10 — Phase 3 deferred indefinitely?** Nice-to-have items are by
  definition optional. If the operator chooses to defer Phase 3
  indefinitely after Phase 2 ships, that's acceptable — INV-0002 can
  be marked "Mostly Resolved" with the Nice-to-have items tracked
  separately. Confirm: does the user want Phase 3 explicitly scheduled,
  or shipped opportunistically when nearby work touches the relevant
  files?

## References

- **INV-0002** — Architectural review of pre-style-guide codebase
  (source of every finding in this IMPL)
- **INV-0001** — IMPL-0019 post-merge code review findings (provides
  the BufferMetrics adapter pattern that Phase 1 §1.1 and §5.A1
  mirror)
- **INV-0003** — App-centric refactor (intersects: Phase 3 §1.8
  templ viewmodels become moot if INV-0003's SPA refactor lands
  first)
- **INV-0004** — sdk-booty-sh evaluation (parallel track; doesn't
  block IMPL-0020)
- **CLAUDE.md** — repo-level conventions; updated in-flight by each
  phase as findings are resolved
- **DESIGN-0001** — Server Price Tracker architecture (the
  interface-first design this IMPL reinforces)
