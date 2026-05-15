---
id: IMPL-0021
title: "ComponentType registry pattern"
status: Draft
author: Donald Gifford
created: 2026-05-15
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0021: ComponentType registry pattern

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-05-15

<!--toc:start-->
- [Objective](#objective)
  - [Why this matters](#why-this-matters)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Registry foundation](#phase-1-registry-foundation)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: Pilot migration (ram)](#phase-2-pilot-migration-ram)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Migrate remaining ComponentTypes](#phase-3-migrate-remaining-componenttypes)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
  - [Phase 4: Cleanup + documentation](#phase-4-cleanup--documentation)
    - [Tasks](#tasks-3)
    - [Success Criteria](#success-criteria-3)
- [Dependencies](#dependencies)
- [Risks](#risks)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Replace the **eight-touchpoint** ComponentType pattern (documented as
a workaround in CLAUDE.md) with a **registry pattern** where each
ComponentType is a single self-contained object: one file in
`pkg/extract/component/` declares its patterns, prompt template,
validator, normaliser, product-key generator, and DB CHECK constraint
value. Adding a new ComponentType becomes **one file + one DB
migration**, not eight coordinated edits across the codebase.

**Implements:** INV-0002 §4.5 (Eight-touchpoint ComponentType
duplication).

**Parent:** IMPL-0020 (this work was carved out as a sibling doc
because its size — ~1,000-1,500 LOC across 4 PRs — warrants its own
phased plan).

### Why this matters

From CLAUDE.md: *"Adding a new ComponentType — eight touchpoints to
keep in sync."* Three of the last four ComponentTypes (GPU, Workstation,
Desktop) shipped with at least one missed touchpoint that triggered
production bugs:

- **IMPL-0017 (GPU)**: missed `validComponentTypes` map → SQLSTATE
  23514 on first watch create.
- **IMPL-0018 (Workstation/Desktop)**: missed DB CHECK migration →
  same failure mode, fixed in migration 011.
- **IMPL-0017 (GPU)**: `recompute_baseline` left orphan baselines
  when product_key shape changed — still parked as a follow-up.

A registry pattern makes the dependency graph explicit: the registry
*requires* every Component to declare every step, and a missing step
is a Go compile error or a registry validation panic at init.

## Scope

### In Scope

- New `pkg/extract/component/` package containing:
  - `Component` struct (one per ComponentType)
  - `Registry` type (collection + lookup)
  - Per-ComponentType files (one for each of the 9 current types:
    ram, drive, server, cpu, nic, gpu, workstation, desktop, other)
- Migration of consumers (`pkg/extract/extractor.go`,
  `prompts.go`, `preclassify.go`, `productkey.go`, `validate.go`,
  `normalize.go`) to consume the registry instead of per-type
  switches and maps
- CLAUDE.md update: remove the "eight touchpoints" warning;
  replace with "one file in `pkg/extract/component/`" guidance
- Regression test gate: `make test-regression` must show no
  per-component accuracy regression after migration

### Out of Scope

- DB CHECK constraint *enforcement* via Go (the DB still owns the
  constraint; the registry merely declares the canonical string for
  documentation and migration generation; the migration itself
  remains a hand-written SQL file)
- Adding any new ComponentType (this IMPL preserves the 9 existing
  types as-is; new types come in separate work)
- Refactoring scoring or baseline logic — only the
  classification/extraction/validation pipeline is in scope

## Implementation Phases

Four phases, each a separate PR. Phases must land in order (Phase 2
depends on Phase 1's foundation; Phase 3 depends on Phase 2's pilot
proving the pattern; Phase 4 deletes code Phase 3 made obsolete).

---

### Phase 1: Registry foundation

**Goal:** define the `Component` and `Registry` types without
migrating any existing ComponentType. The registry sits alongside
the existing per-file logic; consumers still use the old switches
and maps. Phase 1 only adds new code — no behaviour change.

#### Tasks

- [ ] Create `pkg/extract/component/component.go` with the `Component`
      struct:

      ```go
      type Component struct {
          // Name is the canonical ComponentType value.
          Name domain.ComponentType
          // DBConstraint is the value used in the listings/watches
          // CHECK constraint (typically equal to string(Name), but
          // kept separate for clarity).
          DBConstraint string
          // PreClassifyPatterns matches title tokens that strongly
          // suggest this component type. Used by the pre-classifier
          // before the LLM runs.
          PreClassifyPatterns *regexp.Regexp
          // PromptTemplate is the schema/instructions block injected
          // into the extraction prompt for this component type.
          PromptTemplate string
          // Validator returns nil if the extracted attributes pass
          // type-specific range/enum checks; non-nil otherwise.
          Validator func(*domain.Listing) error
          // Normaliser repairs common LLM mistakes (capacity unit
          // confusion, placeholder enum values, etc.) before
          // validation runs.
          Normaliser func(*domain.Listing) error
          // ProductKey derives the canonical baseline grouping key
          // for a fully-extracted listing.
          ProductKey func(*domain.Listing) string
      }
      ```
- [ ] Create `pkg/extract/component/registry.go` with the `Registry`
      type:

      ```go
      type Registry struct {
          components map[domain.ComponentType]*Component
      }

      func New() *Registry { ... }
      func (r *Registry) Register(c *Component) { ... } // panics on dup
      func (r *Registry) For(t domain.ComponentType) (*Component, bool)
      func (r *Registry) All() []*Component
      func (r *Registry) Validate() error // ensures every required field non-nil
      ```
- [ ] Create `pkg/extract/component/default.go` exposing
      `Default() *Registry` — a package-level lazy-initialised
      registry populated by `init()` calls in each per-type file
      (Phase 2 onwards).
- [ ] Add table-driven tests for `Registry.Register`,
      `Registry.For`, `Registry.Validate`. Test edge cases: duplicate
      registration, lookup miss, validation of partial Component
      (missing Validator, etc.).
- [ ] CLAUDE.md: add a forward-pointer note that the registry exists
      and will absorb component-specific logic over Phase 2-4.

#### Success Criteria

- `make lint` and `make test` pass.
- `pkg/extract/component` exists with `Component`, `Registry`,
  `Default()`, and tests at ≥85% coverage.
- The registry is *empty* at end of Phase 1 (`Registry.All()`
  returns `[]`). No existing logic has been migrated yet.
- No consumer of `pkg/extract` imports `pkg/extract/component` yet —
  the foundation is opt-in for Phase 2.

---

### Phase 2: Pilot migration (ram)

**Goal:** migrate the simplest ComponentType (`ram`) end-to-end to
prove the pattern. Every other ComponentType migration in Phase 3
follows this same shape.

`ram` is chosen because:

- Smallest validator (a handful of enum checks)
- Smallest normaliser (`NormalizeRAMSpeed` + PC4 markers — already
  well-tested via the IMPL-0014 work)
- Simple product key (`ram:ddr4:ecc_reg:32gb:2666`)
- No second-pass-specific edge cases (compared to server tier,
  GPU family disambiguation, system workstation/desktop inference)

#### Tasks

- [ ] Create `pkg/extract/component/ram.go` declaring `ramComponent`:
  - `Name = domain.ComponentTypeRAM`
  - `DBConstraint = "ram"`
  - `PreClassifyPatterns`: regex moved from
    `preclassify.go::primaryComponentPatterns["ram"]`
  - `PromptTemplate`: ram-specific block moved from
    `prompts.go::ramPromptTmpl` (or equivalent constant)
  - `Validator`: move `validateRAM` from `validate.go`
  - `Normaliser`: move `normalizeRAM` (including `NormalizeRAMSpeed`,
    PC4 marker logic) from `normalize.go`
  - `ProductKey`: move `ramProductKey` from `productkey.go`
- [ ] Add `init()` in `pkg/extract/component/ram.go` that calls
      `defaultRegistry.Register(ramComponent)`.
- [ ] Update consumers to use the registry **for `ram` only**:
  - `extractor.go`: when component type is RAM, use
    `component.Default().For(...)` to fetch the prompt template,
    validator, normaliser, product-key generator. For all other
    types, fall through to the existing switches/maps.
  - This dual-path approach minimises Phase 2 risk — only the RAM
    code path goes through the registry; everything else stays
    untouched.
- [ ] Run `make test-regression` against the configured backend.
      Confirm `ram` per-component accuracy is unchanged (or within
      noise) compared to baseline.
- [ ] Add unit tests for `ram.go` that exercise the validator,
      normaliser, and product-key generator with table-driven cases.
- [ ] CLAUDE.md update: add a note that `ram` is now in the
      registry; remaining types follow in Phase 3.

#### Success Criteria

- `make lint`, `make test`, `make test-regression` pass.
- `pkg/extract/component/ram.go` exists with all five required
  fields populated.
- `component.Default().For(domain.ComponentTypeRAM)` returns the
  component; `All()` returns `[ramComponent]`.
- Extraction quality for RAM listings is unchanged (regression
  runner shows no per-component accuracy regression).
- The dual-path code in `extractor.go` is clearly demarcated with
  a comment explaining it's transitional — to be deleted in Phase 4.

---

### Phase 3: Migrate remaining ComponentTypes

**Goal:** migrate the remaining 8 ComponentTypes (drive, server,
cpu, nic, gpu, workstation, desktop, other) following the Phase 2
pattern.

This is the largest phase. **Open Q1** asks whether to ship as one
PR (8 migrations bundled) or split into 2-3 PRs (e.g., simple types
first: drive/cpu/nic/other; then complex: server/gpu/workstation/
desktop).

#### Tasks

For **each** ComponentType (drive, server, cpu, nic, gpu,
workstation, desktop, other):

- [ ] Create `pkg/extract/component/<type>.go` mirroring the
      `ram.go` shape from Phase 2:
  - Define `<type>Component` with all five Component fields
  - Move validator, normaliser, product-key, prompt template, and
    pre-classify pattern from the existing files
  - Add `init()` registration
- [ ] Update consumers to use the registry for this type.
- [ ] Run `make test-regression`; confirm per-component accuracy
      is unchanged.
- [ ] Add unit tests for the type's component file.

**Component-specific gotchas to preserve:**

- **server**: `server_tier.go` logic (barebone/partial/configured
  tier suffix in product key) must move into the component. The
  `tier` derivation is part of `serverProductKey`; do not let it
  drift.
- **gpu**: `CanonicalizeGPUModel` + `DetectGPUFamilyFromModel` +
  `CanonicalizeGPUFamily` logic stays in the gpu component's
  normaliser. The canonical-model override behaviour (LLM family
  field discarded for known SKU patterns) must be preserved
  byte-for-byte — accidental regression here re-fragments
  baselines.
- **workstation/desktop**: `NormalizeSystemExtraction` (vendor
  canonicalisation, line canonicalisation, line-from-model
  inference) must move intact. `systemServerLineDenylist` (drops
  PowerEdge/ProLiant/UCS hallucinations) is also part of the
  workstation/desktop normaliser, not the extractor.
- **other**: minimal — no extracted attributes; product key empty.
  The pre-classify pattern is the only meaningful field; validator
  is essentially a no-op.

#### Success Criteria

- `make lint`, `make test`, `make test-regression` pass.
- All 9 ComponentTypes are registered:
  `component.Default().All()` returns 9 entries.
- `Registry.Validate()` returns nil — every component has every
  required field populated.
- Per-component regression accuracy is unchanged (or improved) for
  all 9 types, verified by `make test-regression`.
- Each ComponentType has a dedicated `*_test.go` file with
  validator, normaliser, and product-key coverage.

---

### Phase 4: Cleanup + documentation

**Goal:** delete the now-redundant per-step switches/maps that
duplicated the per-component data; collapse the dual-path consumer
code into single registry calls; update CLAUDE.md to reflect the
new shape.

#### Tasks

- [ ] Delete `validComponentTypes` map from `extractor.go` —
      `Registry.For(t)` already returns `(*Component, bool)` and
      the `bool` is the allowlist signal.
- [ ] Delete component-specific switches from `productkey.go` —
      callers use `c.ProductKey(listing)`.
- [ ] Delete component-specific switches from `validate.go` —
      callers use `c.Validator(listing)`.
- [ ] Delete component-specific switches from `normalize.go` —
      callers use `c.Normaliser(listing)`. Shared normaliser
      helpers (e.g., placeholder-enum stripping, capacity-unit
      repair) stay as package-level functions called by the
      component-specific normalisers.
- [ ] Delete `primaryComponentPatterns` map from `preclassify.go` —
      callers iterate `Registry.All()` to find the matching
      pattern.
- [ ] Delete per-component prompt template constants from
      `prompts.go` — the registry owns them.
- [ ] Delete the dual-path code in `extractor.go` introduced in
      Phase 2; route every type through the registry.
- [ ] CLAUDE.md: remove the "Adding a new ComponentType — eight
      touchpoints" warning. Replace with: "Adding a new
      ComponentType: create one file in `pkg/extract/component/<name>.go`
      with all five `Component` fields populated; add a CHECK
      constraint migration. The eight-touchpoint section is now
      obsolete."
- [ ] Update INV-0002 §4.5 status: mark as resolved, link to
      IMPL-0021 phases.
- [ ] Update IMPL-0020 Phase 2D §4.5 task as out-of-scope (already
      noted; verify no stragglers).

#### Success Criteria

- `make lint`, `make test`, `make test-regression` pass.
- The following no longer exist anywhere in the codebase:
  - `validComponentTypes` map
  - `primaryComponentPatterns` map
  - Per-component switches in `productkey.go`, `validate.go`,
    `normalize.go`
  - Per-component prompt template constants in `prompts.go`
- `pkg/extract/component/` is the single source of truth for
  every ComponentType.
- A hypothetical 10th ComponentType could be added by:
  1. Creating one new file in `pkg/extract/component/`
  2. Adding one DB CHECK constraint migration
  3. (Adding the enum value to `domain.ComponentType` in
     `pkg/types/types.go` — still required because Go enums)
  — total of three files touched, not eight.
- CLAUDE.md's "Adding a new ComponentType" note describes the new
  flow.

---

## Dependencies

- **IMPL-0020 Phase 1** — Phase 2 of IMPL-0021 doesn't strictly
  require it, but the narrow Store interfaces from §1.3 are useful
  if the registry needs DB access (it doesn't, today — `Component`
  is pure functions over `*domain.Listing` — but the convention
  should be: registry never imports `internal/store/Store`; if it
  ever needs persistence it takes a narrow interface).
- **No external dependencies.** IMPL-0021 only restructures existing
  code; no new libraries, no new services.
- **`make test-regression`** must be wired and producing per-component
  accuracy numbers before Phase 2 starts. This is the safety net
  for the whole migration.

## Risks

1. **Regression risk on `server` tier logic.** `server_tier.go` is
   the most complex per-type code; an accidental drop of the
   `barebone|partial|configured` suffix would re-mix server
   baselines. Mitigation: dedicated tests for `serverProductKey`
   before and after migration; spot-check `product_key` values
   on production data.

2. **Regression risk on `gpu` family canonicalisation.** The LLM-
   override logic (`DetectGPUFamilyFromModel` short-circuits the
   LLM's `family` field for known canonical models) was hard-won.
   Migrating it without breaking the override semantics requires
   careful test coverage; the gpu component's `Normaliser` must
   preserve the exact decision tree.

3. **Prompt template drift.** Per-component prompt templates are
   large blocks of string content. A whitespace or comment change
   during migration could subtly alter LLM behaviour. Use byte-for-
   byte preservation; verify with `git diff --word-diff` on the
   migrated template.

4. **Baseline orphans during migration.** If a normaliser change
   inadvertently produces a different product key for the same
   listing, baselines drift. CLAUDE.md already warns about this for
   any normaliser change. Mitigation: run the orphan-cleanup query
   (INV-0002 §4.7 / IMPL-0020 Phase 2D) on production after each
   Phase 3 type migration.

5. **`make test-regression` cost.** The regression runner uses real
   LLM calls — Anthropic costs money, Ollama is free but slow.
   Running once per phase is fine; running once per ComponentType
   migration in Phase 3 could become expensive. Use the smallest
   regression dataset that still covers per-component breakdowns.

## Open Questions

1. **Phase 3 PR shape — one big PR or three smaller?** Phase 3
   migrates 8 ComponentTypes. Three options:
   (a) Single PR — all 8 types in one diff. Largest PR size, but
   coherent: "Phase 3 complete".
   (b) Two PRs — simple types first (drive, cpu, nic, other),
   complex types second (server, gpu, workstation, desktop).
   (c) Eight PRs — one per type. Most reviewable per-PR; most
   coordination cost.
   Recommend (b) — keeps PR size manageable while batching related
   work. The simple-types PR proves the pattern at scale (4 types
   ≠ 1 pilot); the complex-types PR concentrates the gotchas
   (server tier, GPU canonicalisation, workstation/desktop
   inference) in one reviewable diff.

2. **Should `Component.DBConstraint` actually be exposed?** Today
   the DB CHECK constraint string equals `string(Name)` for every
   type. Two views:
   (a) Keep `DBConstraint` as a separate field — defensive against
   future divergence (e.g., a ComponentType whose constraint value
   is different from the canonical name).
   (b) Drop it; document that `string(Name)` is the constraint
   value.
   Recommend (a) — defensive; the field is one string per type and
   makes the contract explicit.

3. **Should the registry validate at `init()` or lazily?**
   `Registry.Validate()` checks every component has every required
   field non-nil. Two options:
   (a) Call `Validate()` from each component file's `init()` (or
   from `Default()`'s lazy init) and panic on first invalid
   component. Fails loud at startup.
   (b) Make `Validate()` an opt-in method callers run if they want
   the guarantee.
   Recommend (a) — startup failure is much better than a runtime
   nil-deref three weeks later.

4. **Test-regression budget for Phase 3 incremental runs.** See
   risk #5. Should each ComponentType migration trigger a full
   regression run, or a per-component subset?
   Recommend: full regression at the end of each PR (so each
   reviewable diff is gated on regression-runner green), not
   per-individual-type within a PR.

5. **Where do shared normaliser helpers live?** Functions like
   placeholder-enum stripping, capacity-unit repair,
   `NormalizeExtraction` (the cross-component normaliser that runs
   *before* component-specific normalisation) need a home. Options:
   (a) Stay in `pkg/extract/normalize.go` as package-level helpers
   imported by each component file.
   (b) Move to `pkg/extract/component/shared.go`.
   Recommend (a) — keeps the component files focused on their own
   type; shared helpers in the parent package is the natural
   layering.

6. **Should Phase 4 also extract the embedded CHECK migration text
   into a registry helper?** Today, adding a ComponentType requires
   editing the migration manually (DROP + ADD the CHECK). A
   registry helper that emits the CHECK clause given the current
   `Registry.All()` would eliminate the chance of forgetting a
   type. But this drifts toward "registry generates migrations",
   which is scope creep. Recommend: defer to a follow-up; document
   the "add a CHECK migration" step in CLAUDE.md as the second of
   three remaining touchpoints.

## References

- **INV-0002 §4.5** — "Eight-touchpoint ComponentType duplication"
  (the finding this IMPL resolves)
- **IMPL-0020** — parent IMPL doc; §4.5 explicitly carved out into
  this sibling doc
- **CLAUDE.md** — "Adding a new ComponentType — eight touchpoints
  to keep in sync" section (to be removed in Phase 4)
- **IMPL-0017** — GPU ComponentType wiring (case study: 7 Go
  touchpoints + 1 DB migration; the shape this IMPL formalises)
- **IMPL-0018** — Workstation/Desktop ComponentType wiring (case
  study: same eight-touchpoint pattern; the IMPL-0018 migration 011
  incident illustrates the failure mode)
- **DESIGN-0012** — GPU component design (the reference for GPU
  family canonicalisation; risk #2 above)
- **DESIGN-0015** — Workstation/desktop design (the reference for
  system normalisation; risk discussed in CLAUDE.md)
