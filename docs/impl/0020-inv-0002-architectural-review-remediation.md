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
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Critical findings](#phase-1-critical-findings)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: Important findings](#phase-2-important-findings)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Nice-to-have findings](#phase-3-nice-to-have-findings)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Remediate the 57 findings catalogued in INV-0002 (architectural review
of the pre-style-guide codebase). Three sequential phases tier the work
by severity: every Critical fix lands before any Important fix; every
Important fix lands before any Nice-to-have. Each phase ships as a
single PR (subject to Open Question Q1 — Phase 2 may need to be split
given its 27-item footprint).

**Implements:** INV-0002 (Architectural review of pre-style-guide
codebase)

This doc is the execution plan. INV-0002 is the source of truth for
*what* each finding is and *why* it matters — IMPL-0020 catalogues
*when* each is fixed and *how* the work is sequenced.

## Scope

### In Scope

- All 11 Critical findings from INV-0002 (§1-§5)
- All 27 Important findings from INV-0002
- All 19 Nice-to-have findings from INV-0002
- DB migrations required by §5.P2, §5.P3, §5.P6
- Test additions required by changes in scope
- CLAUDE.md updates that follow from the architectural changes

### Out of Scope

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

**1.3 Store interface split (foundation for Phase 2 naming sweep):**

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
- [ ] Update each consumer to depend on the narrow interface it
      actually uses. `PostgresStore` keeps every method (satisfies
      all interfaces).
- [ ] Update `MockStore` generation: `make mocks` regenerates
      per-interface mocks.

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

**Goal:** address the 27 Important findings.

These don't block production (Phase 1 already shipped) but they're
the next-tier maintainability and performance work. Grouped by
domain (architecture / performance / style sweeps / tech debt) so
the PR can be reviewed in coherent chunks even though it ships as one
unit. See Q1 about whether to subdivide.

#### Tasks

**2.1 Architecture:**

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

**2.2 Performance:**

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

**2.3 Style sweeps:**

- [ ] **(INV §2.1)** "failed to" sweep — mechanical PR-task:
      `rg "failed to" --type go` and rewrite each call site as a
      verb-phrase ("start scheduler", "scan alert", "call LLM
      backend", etc.).
- [ ] **(INV §2.3)** Drop `Get` prefix on 14 methods bundled with
      §1.3 rename (the methods are part of the interfaces split in
      Phase 1; the rename lands here).
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

**2.4 Tech debt:**

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
      `fmt.Println` outside `cmd/spt` (already partially mandated
      by Phase 1; finalised here with linter enforcement).
- [ ] **(INV §4.5)** Build the ComponentType registry — single new
      file per ComponentType in `pkg/extract/component/` that
      registers patterns, prompt template, validator, normaliser,
      product-key generator, CHECK constraint name. `Registry.Register`
      wires everything. Adding a ComponentType becomes one new file
      + one CHECK migration. The current eight-step ritual collapses.
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

**2.5 Tests + verification:**

- [ ] All §2.1-2.4 changes have unit-test coverage for the new code
      paths.
- [ ] `make test-db` (new build-tag target) passes.
- [ ] Forbidden-pattern lint catches any regression of
      `slog.Default` / `fmt.Println` outside `cmd/spt`.
- [ ] CLAUDE.md updates: remove the "eight-touchpoint ComponentType"
      warning; remove the orphan-baseline cleanup warning; note the
      new `internal/bootstrap/*` structure; note the new
      `pkg/session` package.

#### Success Criteria

- `make lint` and `make test` pass.
- `make test-db` (new target) passes against a local testcontainers
  Postgres.
- `cmd/server-price-tracker/cmd/serve.go` is ≤200 LOC (down from
  677).
- A new ComponentType can be added by creating one file under
  `pkg/extract/component/` + one DB migration — no edits to
  validator, normaliser, product-key, classifier, or prompt template
  files (the registry pulls them in).
- `RecomputeAllBaselines` completes in <2s on the production dataset
  (down from ~15s).
- `spt_alerts_fired_total` is labelled by `watch_id`, not
  `watch_name`. Cardinality on `/metrics` measured before/after to
  prove the bound.
- Zero `internal/` imports in any `pkg/` package
  (`! rg "internal/" pkg/`).
- All Huma handlers return error chains intact —
  `errors.Is/As` works against the original error from any returned
  500.
- Migrations 015 and 016 applied; orphan baselines cleaned up at
  `recompute_baseline` time.

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

- **Phase 1 → Phase 2:** Phase 2's `Get`-prefix sweep (§2.3) depends
  on Phase 1's Store interface split (§1.3) because the methods being
  renamed are the interface methods.
- **Phase 1 → Phase 2:** Phase 2's testcontainers `dbtest` build tag
  (§4.1) builds on Phase 1's pgx scany migration (§4.4) — the
  struct-tag-driven scanner is what the new tests will exercise.
- **Phase 1 → Phase 3:** Phase 3's `ProcessAlerts` → method move
  (§1.7) was partially done by Phase 1's logger-injection work; the
  finalisation is a cleanup task.
- **Phase 2 → Phase 3:** Phase 3's narrow-interface handler migration
  (§5.A8) uses the interfaces defined by Phase 1 (§1.3) but converts
  the *remaining* handlers that Phase 1 didn't touch (because Phase 1
  focused on the Critical surface only — the Store split happened but
  not every handler was migrated to consume the narrow form).
- **External:** Phase 2's ComponentType registry (§4.5) is the
  largest single piece of work. If timeline pressure exists, this
  could be deferred to its own follow-up IMPL doc without blocking
  the rest of Phase 2 — see Q5.

## Open Questions

1. **Phase 2 PR size.** Phase 2 has 27 findings spanning architecture,
   performance, style sweeps, and tech debt. Even with the four
   sub-task groupings (2.1-2.4), this is a very large PR — likely
   2,500-4,000 LOC of changes touching ~50 files. **Should Phase 2
   be split into 4 sub-phases**, each shipped as a separate PR
   (2A architecture / 2B performance / 2C style / 2D tech debt)?
   The original prompt said "in a single PR" per phase, but the
   real-world reviewability of a 3,000-LOC mixed-concerns PR is
   poor. Recommend: split.

2. **Critical-finding count discrepancy.** INV-0002's Conclusion
   table reports "9 Critical, 20 Important, 12 Nice-to-have" but
   the actual catalog (counted from the §1-§5 headings) is
   **11 Critical, 27 Important, 19 Nice-to-have = 57 total**. This
   IMPL doc uses the actual catalog. Should I file a small fix to
   INV-0002's Conclusion to match the catalog? (Yes; small, separate
   commit.)

3. **Store interface — keep monolithic `Store` as a union alias?**
   Phase 1 §1.3 splits the 45-method `Store` into eight per-entity
   interfaces. Two options for the legacy `Store` type:
   (a) Keep it as a union interface (`type Store interface { WatchStore;
   ListingStore; ... }`) — preserves backward compatibility for any
   straggling consumers.
   (b) Delete it entirely — forces every consumer to declare a
   narrow type, no exceptions.
   Option (b) is the principled choice; option (a) is the pragmatic
   choice. Recommend (b) since the Important phase §5.A8 already
   migrates the remaining wide-Store consumers — this leaves no
   reason to retain the union.

4. **Testcontainers vs sqlmock for Phase 1 §1.8 tests.** Phase 2
   §4.1 adds testcontainers-backed `PostgresStore` tests gated
   behind `dbtest`. Phase 1's new batch methods
   (`ListingsByIDs`, `AlertsWithNotificationStatus`) need test
   coverage too. Should Phase 1 ship the testcontainers harness
   ahead of Phase 2, or use mock-based tests in Phase 1 and add
   testcontainers in Phase 2?
   Recommend: Phase 1 uses mock-based tests for the new batch
   methods; Phase 2 retroactively adds testcontainers tests as part
   of §4.1. Keeps Phase 1 scope focused on Critical fixes.

5. **ComponentType registry scope.** Phase 2 §4.5 (the registry) is
   the largest single piece of work in the entire IMPL — likely
   1,000-1,500 LOC by itself (one file per ComponentType, plus the
   registry, plus consumer migration). Two options:
   (a) Ship as part of Phase 2 (PR size already large; see Q1).
   (b) Split into IMPL-0021 (dedicated registry doc) and remove from
   Phase 2 scope.
   Recommend (b) if Q1 is answered "split"; recommend (a) only if
   Phase 2 stays a single PR.

6. **Migration numbering.** The repo is at migration 014 (013 was
   `judge_scores`; I'm assuming the next slot is 014 — verify
   against `migrations/` listing). This IMPL proposes 015, 016, 017.
   If any other in-flight work claims these slots, renumber here.

7. **CLAUDE.md update granularity.** Each phase updates CLAUDE.md
   in-flight (as tasks remove warnings or note new structure). Is a
   single end-of-phase summary commit preferable to inline updates?
   Inline keeps the doc in sync with the code at each task
   completion; end-of-phase keeps history cleaner.
   Recommend inline (matches existing convention from IMPL-0017,
   IMPL-0018, IMPL-0019).

8. **Phase 1 priority within-phase ordering.** The task list within
   Phase 1 is ordered foundation-first (boundary → logger → Store
   split → scan safety → query collapse → indices → N+1 batching).
   The user originally asked for severity-tiered phases without
   specifying within-phase order. The proposed order minimises
   rework (each task's prerequisites are already done) but means
   the *bug-fix* tasks (PoolSize §5.A3, N+1 §3.1/§3.2) ship later
   in the PR than the *refactor* tasks. If the priority is "ship
   the bug fixes ASAP", we could split Phase 1 into 1A (bug fixes
   only, ships first) and 1B (refactor foundations). Recommend
   defaulting to the proposed order — Phase 1 is one PR, so the
   bug fixes ship together with everything else.

9. **Lint rule for `slog.Default()` enforcement.** Phase 2 §4.3
   commits a lint rule forbidding `slog.Default()` outside
   `cmd/spt`. golangci-lint has `forbidigo` for this. Are there
   any legitimate `slog.Default()` callers in the codebase the rule
   should grandfather (e.g., in tests, or in CLI bootstrapping
   before the configured logger is constructed)? Recommend:
   `forbidigo` with an exemption for `cmd/spt/` and `*_test.go`
   files.

10. **Phase 3 deferred indefinitely?** Nice-to-have items are by
    definition optional. If the operator chooses to defer Phase 3
    indefinitely after Phase 2 ships, that's acceptable — INV-0002
    can be marked "Mostly Resolved" with the Nice-to-have items
    tracked separately. Confirm: does the user want Phase 3
    explicitly scheduled, or shipped opportunistically when nearby
    work touches the relevant files?

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
