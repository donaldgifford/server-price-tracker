---
id: IMPL-0018
title: "DESIGN-0015 workstation and desktop component types phase plan"
status: Draft
author: Donald Gifford
created: 2026-05-02
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0018: DESIGN-0015 workstation and desktop component types phase plan

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-05-02

<!--toc:start-->
- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Domain wiring (enum + product key + validator)](#phase-1-domain-wiring-enum--product-key--validator)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: LLM surface (extraction prompts + classifier prompt + pre-classifier primaries)](#phase-2-llm-surface-extraction-prompts--classifier-prompt--pre-classifier-primaries)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Normaliser](#phase-3-normaliser)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
  - [Phase 4: DB migration + integration smoke test](#phase-4-db-migration--integration-smoke-test)
    - [Tasks](#tasks-3)
    - [Success Criteria](#success-criteria-3)
  - [Phase 5: Dev validation](#phase-5-dev-validation)
    - [Tasks](#tasks-4)
    - [Success Criteria](#success-criteria-4)
  - [Phase 6: Production rollout](#phase-6-production-rollout)
    - [Tasks](#tasks-5)
    - [Success Criteria](#success-criteria-5)
  - [Phase 7 (optional): Backfill misclassified workstations and desktops](#phase-7-optional-backfill-misclassified-workstations-and-desktops)
    - [Tasks](#tasks-6)
    - [Success Criteria](#success-criteria-6)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Objective

Implement DESIGN-0015 — add `workstation` and `desktop` as peer
`ComponentType` values. Mirrors the seven-phase shape of IMPL-0017
(GPU rollout) so the playbook is familiar; both types ship in one PR
per DESIGN-0015 Q6.

**Implements:** DESIGN-0015

## Scope

### In Scope

- New `ComponentWorkstation` and `ComponentDesktop` enum values
  through every existing ComponentType touchpoint (the eight-point
  checklist captured in `gpu_component_type.md` memory).
- Product keys `workstation:<vendor>:<line>:<model>` and
  `desktop:<vendor>:<line>:<model>`.
- Classifier prompt rules + pre-classifier primary regex coverage
  for the watch shopping list from DESIGN-0015 Q4 / Q5 (Dell
  Precision T-series, Dell Pro Max, Lenovo ThinkStation P-series,
  HP Z-series, Dell OptiPlex, Dell Pro, Lenovo ThinkCentre, HP
  EliteDesk).
- Per-type extraction prompt + validator + normaliser.
- DB migration adding both values to `watches_component_type_check`
  and `listings_component_type_check`.
- Dev + prod rollout playbook mirroring IMPL-0017 Phase 5/6.
- Optional backfill SQL helper (Phase 7) for re-classifying
  historical workstation/desktop listings currently bucketed as
  `server` or `other`.

### Out of Scope

- Cross-bucket alert matching (DESIGN-0015 Q1 — single
  classification per listing).
- Tier suffix on workstation product key (DESIGN-0015 Q2 — defer
  to v2).
- Bundle-detection / GPU-bundled-with-desktop handling (DESIGN-0015
  Q3 — defer until real desktop data accumulates).
- Mobile workstations (laptops). Different category, different
  watches, different baselines.
- Item-specifics-aware classifier (Open Question 1 below — design
  decision required before implementation starts).

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all
its tasks are checked off and its success criteria are met.

---

### Phase 1: Domain wiring (enum + product key + validator)

Add the new ComponentType values and the type-specific support code.
Mirrors IMPL-0017 Phase 1.

#### Tasks

- [ ] Add `ComponentWorkstation = "workstation"` and
  `ComponentDesktop = "desktop"` to `pkg/types/types.go`.
- [ ] Add both entries to the `validComponentTypes` map in
  `pkg/extract/extractor.go`. (This is the easy-to-miss allowlist
  — see `gpu_component_type.md` memory.)
- [ ] Add `case "workstation"` and `case "desktop"` switch arms in
  `pkg/extract/productkey.go`. Initial shape:
  `<type>:<vendor>:<line>:<model>` using `normalizeStr` on each
  attribute (mirrors the pre-tier server shape, no integer
  segment).
- [ ] Add validators in `pkg/extract/validate.go`:
  - `validateWorkstation` and `validateDesktop`. Required:
    `vendor`, `model`. Optional: `line`, `cpu`, `gpu`, `ram_gb`,
    `storage_gb`, `form_factor`. (See Open Question 3 for required-
    set debate.)
  - Shared helpers if validation logic overlaps significantly
    (vendor must be non-empty string, model must be non-empty
    string).
- [ ] Add `case domain.ComponentWorkstation` and
  `case domain.ComponentDesktop` arms to the `Validate` switch.
- [ ] Add product-key tests for both new types in
  `pkg/extract/productkey_test.go`.
- [ ] Add validator tests in `pkg/extract/validate_test.go`.
- [ ] Add type-string tests in `pkg/types/types_test.go` if such
  tests exist.

#### Success Criteria

- `go build ./...` clean.
- `make test` passes including new test cases.
- `make lint` clean.
- Validator tests cover required + optional + missing-required
  failure modes.

---

### Phase 2: LLM surface (extraction prompts + classifier prompt + pre-classifier primaries)

Teach the LLM about the two new types and the deterministic regex
about workstation/desktop title patterns. Mirrors IMPL-0017 Phase 2.

#### Tasks

- [ ] Add `workstationTmpl` and `desktopTmpl` extraction prompt
  templates to `pkg/extract/prompts.go`. Schema fields:
  `vendor`, `line`, `model`, `cpu` (optional, free-form string),
  `gpu` (optional), `ram_gb` (optional, integer), `storage_gb`
  (optional, integer), `form_factor` (optional, enum:
  `tower|sff|micro|mini`), `condition`, `quantity`, `confidence`.
  (See Open Question 2 for shared-vs-separate template debate.)
- [ ] Wire both templates into the `extractTemplates` map.
- [ ] Update `classifyTmpl` rules:
  - Add `workstation, desktop` to the `Types:` line.
  - Add classification rules:
    - "Pick `workstation` for vendor-defined workstation product
      lines: Dell Precision (T-series), Dell Pro Max, Lenovo
      ThinkStation (P-series), HP Z-series."
    - "Pick `desktop` for tower-form general-purpose computers
      without a workstation product line: Dell OptiPlex / Dell
      Pro, Lenovo ThinkCentre, HP EliteDesk, custom builds."
    - "Workstations and desktops that contain a GPU still classify
      as `workstation` or `desktop` — not `gpu`. The GPU is part
      of the bundled system."
    - "Servers stay rack-mountable: Dell PowerEdge / HP ProLiant /
      Cisco UCS / etc. A tower with workstation lineage is NOT a
      server, regardless of CPU class."
- [ ] Extend `primaryComponentPatterns` in
  `pkg/extract/preclassify.go` with workstation+desktop chassis
  patterns (so a `Precision T7920 + 80mm fan` listing defers to
  the LLM rather than short-circuiting to `other`):
  - `\b(precision\s+t?\d{4}|precision\s+\d{4}|pro\s+max|thinkstation|workstation|hp\s+z[0-9])\b`
    for workstation
  - `\b(optiplex|elitedesk|thinkcentre|dell\s+pro\b)\b` for
    desktop. Be careful with `dell pro` — too generic; require
    word boundary and possibly tighten.
- [ ] Update `prompts_test.go` to assert the new templates render.
- [ ] Update `preclassify_test.go` with workstation+desktop
  primary-pattern test cases.

#### Success Criteria

- Classifier prompt explicitly handles workstation/desktop with no
  ambiguous overlap with `server`/`gpu`.
- Pre-classifier primary regex catches all DESIGN-0015 Q4/Q5
  shopping-list chassis names.
- All prompt and pre-classify tests pass.

---

### Phase 3: Normaliser

Per-type normaliser to canonicalise vendor / line / model spellings.
Mirrors `gpu_normalize.go` shape but smaller — the LLM is less
flaky on these fields than on GPU family.

#### Tasks

- [ ] Create `pkg/extract/system_normalize.go` with shared
  `NormalizeWorkstationExtraction` and `NormalizeDesktopExtraction`
  entry points (or a single `NormalizeSystemExtraction` taking the
  ComponentType — see Open Question 4).
- [ ] Implement `vendorAliases` map: `Dell`/`dell`/`DELL`/`Dell
  Inc.` → `dell`; `HP`/`Hewlett-Packard`/`hp`/`Hewlett Packard
  Enterprise` → `hp`; `Lenovo`/`lenovo`/`IBM`-prefixed-Lenovo →
  `lenovo`. Apply at validate-time-canonicalisation, not as a hard
  enum.
- [ ] Implement `lineAliases` map for legacy ↔ post-rebrand Dell
  branding: `Precision Tower`/`Dell Precision`/`Dell Pro Max` ...
  decide whether to collapse `Pro Max` → `precision` or keep
  separate. (See Open Question 5.)
- [ ] Implement model canonicalisation similar to
  `CanonicalizeGPUModel` — strip `dell\s+`/`hp\s+`/`lenovo\s+`
  brand prefixes from model, lowercase, collapse separator
  variants. e.g., `T7920` / `t7920` / `T-7920` → `t7920`.
- [ ] Wire into `pkg/extract/normalize.go` switch.
- [ ] Table-driven tests for each helper, plus a round-trip
  `NormalizeSystemExtraction` table in
  `pkg/extract/system_normalize_test.go`.

#### Success Criteria

- 5 spelling variants of "Dell Precision T7920" produce the same
  canonical form `dell:precision:t7920`.
- `make test` includes >=20 normaliser test cases per the
  DESIGN-0015 watch shopping list.
- 100% line coverage on `system_normalize.go`.

---

### Phase 4: DB migration + integration smoke test

Drop and re-add CHECK constraints. Adds a passing smoke test that
exercises the full classify + extract path for one workstation and
one desktop title.

#### Tasks

- [ ] Create `migrations/011_add_workstation_and_desktop_component_types.sql`
  and the embedded copy in
  `internal/store/migrations/011_add_workstation_and_desktop_component_types.sql`.
  Both files identical, mirrors migration 010 shape:
  `ALTER TABLE watches DROP CONSTRAINT watches_component_type_check;`
  followed by re-add with `'workstation', 'desktop'` added to
  the IN list. Same for `listings_component_type_check`.
- [ ] Verify `internal/store/migrations/` is included in
  `embed.FS` (it is — same path as migration 010).
- [ ] Add `pkg/extract/extractor_test.go` integration smoke cases
  for `TestLLMExtractor_ClassifyAndExtract`:
  - Workstation case (Dell Precision T7920 listing) — expect
    `ComponentWorkstation` + populated attributes + canonical
    product key.
  - Desktop case (Dell OptiPlex 7080 listing) — expect
    `ComponentDesktop` + populated attributes + canonical
    product key.
  Use `MockLLMBackend` returning canned JSON responses (same shape
  as the existing GPU case).

#### Success Criteria

- Migration applies cleanly on a freshly-migrated dev DB.
- `make test` includes the new integration smoke cases and they
  pass.
- `spt watches create --type workstation` and `--type desktop`
  succeed against a dev API server (no SQLSTATE 23514).

---

### Phase 5: Dev validation

Operator-driven. Deploy the dev image, create at least one watch
per type, observe ingestion, spot-check, ensure no leakage into
`server`/`gpu`/`other` buckets.

#### Tasks

- [ ] Push the branch, open PR with `feature` label.
- [ ] CI must be green (lint, test, build, security, docker-build,
  helm-lint, helm-unittest, helm-ct).
- [ ] Operator deploys dev image
  (`ghcr.io/donaldgifford/server-price-tracker:dev`).
- [ ] Create one workstation watch per chassis line from
  DESIGN-0015 Q4 (Precision T-series, Pro Max, ThinkStation
  P-series, Z-series). Threshold 65, cold-start.
- [ ] Create one desktop watch per line from DESIGN-0015 Q5
  (OptiPlex, Dell Pro, ThinkCentre, EliteDesk). Threshold 65.
- [ ] Wait for one full ingestion cycle.
- [ ] SQL smoke check — confirm at least one listing in each
  new bucket:
  ```sql
  SELECT component_type, COUNT(*)
  FROM listings
  WHERE component_type IN ('workstation', 'desktop')
    AND active = true
  GROUP BY component_type;
  ```
- [ ] Spot-check 5 listings per type — verify product_keys look
  right, attributes populated, no leakage.
- [ ] Spot-check `server` bucket for residual workstations
  (should be small or zero):
  ```sql
  SELECT id, title FROM listings
  WHERE component_type = 'server' AND active = true
    AND title ~* '\y(precision\s+t?\d|thinkstation|workstation|hp\s+z[0-9])\y'
  LIMIT 10;
  ```
- [ ] Spot-check `gpu` bucket for workstations classified as GPU
  (the IMPL-0017 over-eager-classifier failure mode).
- [ ] Spot-check `other` bucket for missed workstations/desktops:
  ```sql
  SELECT id, title FROM listings
  WHERE component_type = 'other' AND active = true
    AND title ~* '\y(precision|thinkstation|optiplex|elitedesk|thinkcentre)\y'
  LIMIT 10;
  ```
- [ ] Document smoke findings in PR comment.

#### Success Criteria

- CI green; PR approved.
- Each new type has ≥1 listing classified within 30 min of dev
  deploy.
- Spot-check confirms attributes and product_key shape.
- `server`-bucket residual workstation count ≤ 5.
- `other`-bucket missed-workstation/desktop count ≤ 2.
- No spike in `gpu`-bucket misclassification.

---

### Phase 6: Production rollout

Mirrors IMPL-0017 Phase 6. Merge → release tag → prod deploy →
trigger baseline + rescore → monitor → bump thresholds when
baselines mature.

#### Tasks

- [ ] Merge PR to main.
- [ ] Confirm release workflow tags + builds + publishes prod
  image.
- [ ] Operator deploys prod (Helm release tagged with new
  `appVersion`).
- [ ] Trigger initial baseline + rescore:
  ```bash
  curl -X POST https://spt.fartlab.dev/api/v1/baselines/refresh
  curl -X POST https://spt.fartlab.dev/api/v1/rescore
  ```
- [ ] Monitor `spt_alerts_created_total{component_type=~"workstation|desktop"}`
  over ~24h. Expect: low volume initially because baselines start
  cold and thresholds are 65.
- [ ] Check baseline maturity once per week:
  ```sql
  SELECT product_key, sample_count, p50 AS p50_usd
  FROM price_baselines
  WHERE product_key LIKE 'workstation:%'
     OR product_key LIKE 'desktop:%'
  ORDER BY sample_count DESC;
  ```
- [ ] Bump each watch's threshold from 65 → 80 once its
  product_key reaches `sample_count >= 10`.
- [ ] Document the production transition in `docs/SQL_HELPERS.md`
  ("Workstation/desktop baseline maturity check").

#### Success Criteria

- Prod has at least one `workstation:<...>` and one
  `desktop:<...>` product_key with `sample_count >= 10` within
  21 days. (Loose timeline — desktop volume on eBay may be lower
  than GPU was.)
- No spike in misclassified `server` or `other` listings —
  bucket growth rate unchanged versus pre-deploy 7-day baseline.
- No alert noise spike for type=workstation/desktop
  (alerts/unique_keys ratio stays ~1, per the dedup logic from
  PR #46).

---

### Phase 7 (optional): Backfill misclassified workstations and desktops

Historical listings classified as `server` or `other` whose titles
match the new workstation/desktop primaries should be re-classified
so the new buckets aren't slow to mature. **Optional** — skip if
Phase 6 baselines mature on their own without backfill.

#### Tasks

- [ ] Add a "Backfill misclassified workstations and desktops
  (IMPL-0018 Phase 7)" section to `docs/SQL_HELPERS.md` with the
  SQL pattern that mirrors the new `primaryComponentPatterns`
  workstation+desktop regex (Postgres regex uses `\y` for word
  boundaries):
  ```sql
  -- DRY-RUN FIRST
  BEGIN;
  UPDATE listings
  SET component_type = 'workstation',
      updated_at = now()
  WHERE component_type IN ('server', 'other')
    AND active = true
    AND title ~* '\y(precision\s+t?\d|thinkstation|workstation|hp\s+z[0-9]|pro\s+max)\y'
  RETURNING id, title, component_type;
  ```
  And the desktop equivalent.
- [ ] Reference `feedback_dry_run_bulk_sql.md` discipline — always
  `BEGIN;` first, eyeball `RETURNING`, then commit.
- [ ] After commit, re-queue the affected listings for extraction
  so attributes are populated under the new type:
  `INSERT INTO extraction_queue (listing_id, priority) SELECT id, 1 FROM listings WHERE component_type IN ('workstation', 'desktop') AND attributes = '{}'::jsonb ON CONFLICT DO NOTHING;`
- [ ] After queue drain, run `POST /api/v1/baselines/refresh` and
  `POST /api/v1/rescore`.
- [ ] Run the orphan-baseline cleanup from `baseline_refresh_orphans_followup.md`
  to drop stale `server:%` baselines whose listings moved to
  `workstation:%`.

#### Success Criteria

- Phase 6 workstation baselines reach `sample_count >= 10` faster
  (within ~7 days instead of ~21).
- No regression in non-system baselines (gpu, ram, etc.) —
  backfill is scoped to `server` + `other`.
- `docs/SQL_HELPERS.md` documents the procedure for future
  ComponentType additions.

---

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `pkg/types/types.go` | Modify | Add `ComponentWorkstation` and `ComponentDesktop` constants |
| `pkg/extract/extractor.go` | Modify | Add to `validComponentTypes` map |
| `pkg/extract/productkey.go` | Modify | Add switch cases for both types |
| `pkg/extract/productkey_test.go` | Modify | Add product-key tests |
| `pkg/extract/validate.go` | Modify | Add `validateWorkstation` + `validateDesktop` |
| `pkg/extract/validate_test.go` | Modify | Add validator tests |
| `pkg/extract/prompts.go` | Modify | Add `workstationTmpl` + `desktopTmpl`; update classifyTmpl |
| `pkg/extract/prompts_test.go` | Modify | Add prompt-render tests |
| `pkg/extract/preclassify.go` | Modify | Extend `primaryComponentPatterns` |
| `pkg/extract/preclassify_test.go` | Modify | Add primary-pattern test cases |
| `pkg/extract/normalize.go` | Modify | Add switch arms calling system normaliser |
| `pkg/extract/system_normalize.go` | Create | Vendor + line + model canonicalisation |
| `pkg/extract/system_normalize_test.go` | Create | Table-driven tests |
| `pkg/extract/extractor_test.go` | Modify | Add `ClassifyAndExtract` smoke tests for both types |
| `migrations/011_add_workstation_and_desktop_component_types.sql` | Create | DB CHECK constraint update |
| `internal/store/migrations/011_add_workstation_and_desktop_component_types.sql` | Create | Embedded copy |
| `docs/EXTRACTION.md` | Modify | Document workstation/desktop schema + normalisation |
| `docs/OPERATIONS.md` | Modify | Sample workstation/desktop watches with cold-start threshold |
| `docs/SQL_HELPERS.md` | Modify | Phase 6 baseline maturity check + Phase 7 backfill |
| `CLAUDE.md` | Modify | Add workstation/desktop paragraph (mirrors GPU paragraph) |

## Testing Plan

- **Unit tests** alongside each new function (validator, prompt
  renderer, pre-classifier, normaliser). Table-driven with `testify/assert`.
- **Integration smoke tests** in `extractor_test.go` for
  `ClassifyAndExtract` covering one workstation and one desktop
  title with a `MockLLMBackend`.
- **Migration test** — extend the existing migration test in
  `internal/store/` to assert migration 011 applies without error.
- **Pre-classifier coverage** — at least 8 cases per primary
  pattern (each shopping-list line × accessory combo). Mirrors
  `preclassify_test.go` shape.
- **Coverage target** — `pkg/extract/system_normalize.go` at 100%;
  validators >= 90%; preclassify additions >= 95%.
- No new package-level integration tests
  (`//go:build integration`) needed; unit + smoke is enough.

## Dependencies

- DESIGN-0015 (Accepted) — source of all design decisions.
- DESIGN-0012 / IMPL-0017 (Implemented, merged in PR #47) —
  template for this implementation; plus the `validComponentTypes`
  allowlist + `recompute_baseline` orphan-cleanup follow-up are
  pre-existing concerns this implementation will hit.
- Memory: `gpu_component_type.md` — eight-touchpoint checklist for
  adding a new ComponentType.
- Memory: `workstation_component_type_followup.md` — design
  context captured during IMPL-0017 dev validation.

## Open Questions

These are implementation-level questions that benefit from
operator review before Phase 1 starts.

1. **Item-specifics-aware classifier.** Today
   `LLMExtractor.Classify(ctx, title)` only takes the title.
   `Most Suitable For: Workstation` is the highest-reliability
   workstation signal per DESIGN-0015 §Classification signals,
   and it lives in eBay item specifics — invisible to the
   classifier today. Three options:

   a. **Plumb item specifics into Classify**, adding a new method
      argument. Highest fidelity; small refactor (Classify
      signature change → all callers update). Item-specific text
      is already in scope at the orchestrator (`ClassifyAndExtract`
      already passes `itemSpecifics` to Extract).
   b. **Title-only classifier** with strengthened title regex
      coverage. No code change to the classifier path; relies on
      every workstation listing having a recognisable chassis
      token in the title. Riskier — some sellers omit the chassis
      name and rely on item specifics.
   c. **Pre-class hook**: a new step before `Classify` that
      inspects item specifics and short-circuits to
      `workstation`/`desktop` if `Most Suitable For` or `Series`
      matches a known workstation/desktop value, falling back to
      LLM otherwise. Most flexible; mirrors the `IsAccessoryOnly`
      pre-classifier shape.

   **Recommendation:** option (c). Mirrors existing pre-classifier
   architecture and is cheaper than always-on LLM classification.
   Implementation cost is small: one regex map per type, item-
   specifics dict lookup, return early if matched.

2. **Shared vs. separate extraction templates.** Workstation and
   desktop have nearly-identical attribute shapes (vendor, line,
   model, cpu, gpu, ram_gb, storage_gb, form_factor, condition,
   confidence). Three options:

   a. **One shared template** with the ComponentType passed in as
      a template parameter. Smallest LLM token surface; one place
      to evolve the schema.
   b. **Two separate templates** that are 95% identical text.
      Matches the existing per-type template structure (one
      `workstationTmpl`, one `desktopTmpl`).
   c. **One shared template, two prompt-rendering wrappers.**

   **Recommendation:** option (b). Matches existing prompts.go
   convention; tiny duplication cost; easier to evolve one
   independently if their schemas diverge.

3. **Required attribute set per type.** What's `required` vs.
   `optional` for workstation and desktop?

   - `vendor` + `model` clearly required (these drive product key
     and have to be non-empty).
   - `line` is the middle segment of the product key. Required or
     optional?
     - If required: a listing missing `line` fails extraction →
       stays unextracted. Stricter, smaller bucket.
     - If optional: missing `line` falls back to `unknown` in the
       product key (`workstation:dell:unknown:t7920`). More
       permissive but pollutes baselines.
   - `cpu`, `gpu`, `ram_gb`, `storage_gb`, `form_factor` — all
     useful for filters but none drive baselines. Optional.

   **Recommendation:** `vendor` + `model` required, `line`
   optional with normaliser-fill from known SKU patterns
   (T-series → precision, P-series → thinkstation, Z-series →
   z-by-hp, M-series → thinkcentre). Same shape as GPU's
   `family` inference from `model`.

4. **Single normalisation entry point or two?** GPU has one
   exported `NormalizeGPUExtraction`. Workstation+desktop share
   logic — should it be one `NormalizeSystemExtraction(componentType, attrs)`
   or two `NormalizeWorkstationExtraction` /
   `NormalizeDesktopExtraction`?

   **Recommendation:** one shared entry point taking
   `componentType`. They share vendor + line + model
   normalisation; only the inference rules differ slightly.
   Single function called from two switch arms is the cleanest.

5. **Dell Pro Max → Precision aliasing.** Dell rebranded their
   workstation lineup in 2024-25; Pro Max replaces Precision at
   the top tier. Two options for product key consolidation:

   a. **Treat as separate lines** —
      `workstation:dell:precision:t7920` vs.
      `workstation:dell:pro-max:<model>`. Matches eBay listing
      vocabulary; legacy Precision listings stay separate from
      post-rebrand Pro Max. Cleaner taxonomy.
   b. **Alias Pro Max → precision** in `lineAliases` so all Dell
      workstations share one line segment. Better baseline
      consolidation; risk of conflating actually-different
      product positioning.

   **Recommendation:** option (a) for v1. They are different
   product lines in Dell's catalog, even if buyers cross-shop.
   Keep them separable; revisit if baselines on either side stay
   thin for >30 days.

6. **Pre-classifier `dell pro` ambiguity.** "Dell Pro" is
   currently Dell's post-rebrand business desktop (Q5 watch
   line). It's also a token that appears in title text for
   unrelated Dell listings ("Dell ProSupport", "Dell Pro Stand",
   etc.). The pre-classifier regex needs tightening to avoid
   false-positive accessory short-circuit prevention.

   **Recommendation:** require `dell\s+pro\b` AND a desktop-
   specific co-occurring token (`tower|desktop|optiplex|sff|
   micro`). If neither co-occurs, the listing isn't a desktop —
   defer to the LLM and accept potential mis-classification, but
   don't grab false positives at the regex layer.

7. **Re-extraction strategy on rollout.** When this lands in
   prod, do we re-queue all existing `server` and `other`
   listings with workstation/desktop signals (so historical
   baselines populate)? Or just let new ingestions fill it in?

   **Recommendation:** Phase 7 backfill is the answer — it's
   already gated as optional in IMPL-0018 and we can decide
   based on Phase 6 maturity rate. No mandatory backfill on
   rollout.

8. **Watch CLI help-text staleness.** The existing
   `spt watches create` help text lists "ram, drive, server,
   cpu, nic" — already missed `gpu` and `other` (noted as
   pre-existing during the GPU PR). Adding `workstation` and
   `desktop` will widen the gap. Worth fixing in Phase 1 since
   we're already touching adjacent code, or punt to a separate
   chore PR?

   **Recommendation:** fix in this PR's Phase 1. One-line edit
   to `cmd/spt/cmd/watches.go` flag description string;
   compounding the staleness is uglier than fixing it now.

## References

- `docs/design/0015-add-workstation-and-desktop-as-component-types.md`
  — DESIGN-0015 (the source of all design decisions)
- `docs/impl/0017-design-0012-gpu-component-type-phase-plan.md` —
  IMPL-0017 (the template this doc's shape mirrors)
- `docs/EXTRACTION.md` — extraction pipeline reference
- `pkg/extract/gpu_normalize.go` — example of a per-component
  normalisation file (pattern this implementation mirrors with
  `system_normalize.go`)
- `pkg/extract/preclassify.go` — primary-component pattern list
  this implementation extends
- `migrations/010_add_gpu_component_type.sql` — CHECK-constraint
  migration template
- Memory: `gpu_component_type.md` — eight-touchpoint checklist
- Memory: `workstation_component_type_followup.md` — design
  context captured during IMPL-0017 dev validation
- Memory: `baseline_refresh_orphans_followup.md` — orphan
  cleanup pattern needed in Phase 7
- Memory: `feedback_dry_run_bulk_sql.md` — Phase 7 SQL discipline
