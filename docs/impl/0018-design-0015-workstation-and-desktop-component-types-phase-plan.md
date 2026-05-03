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
- New `pkg/extract/system_preclassify.go` — item-specifics-aware
  pre-class hook that short-circuits to `workstation`/`desktop`
  when `Most Suitable For` / `Series` / `Product Line` matches a
  known value (Open Question 1, option c).
- Classifier prompt rules + pre-classifier primary regex coverage
  for the watch shopping list from DESIGN-0015 Q4 / Q5 (Dell
  Precision T-series, Dell Pro Max, Lenovo ThinkStation P-series,
  HP Z-series, Dell OptiPlex, Dell Pro, Lenovo ThinkCentre, HP
  EliteDesk).
- Per-type extraction prompt + validator.
- Shared `pkg/extract/system_normalize.go` exposing
  `NormalizeSystemExtraction(componentType, attrs)` (Open Question
  4) — vendor / line / model canonicalisation; line inferred from
  model SKU patterns when LLM omits it (Open Question 3).
- DB migration adding both values to `watches_component_type_check`
  and `listings_component_type_check`.
- `cmd/spt/cmd/watches.go` `--type` help-text refresh covering all
  current `validComponentTypes` (Open Question 8).
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
- Dell Pro Max → Precision line aliasing (Open Question 5 — kept
  separate for v1).
- Mandatory backfill of historical listings on rollout (Open
  Question 7 — Phase 7 is operator-gated).

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all
its tasks are checked off and its success criteria are met.

---

### Phase 1: Domain wiring (enum + product key + validator)

Add the new ComponentType values and the type-specific support code.
Mirrors IMPL-0017 Phase 1.

#### Tasks

- [x] Add `ComponentWorkstation = "workstation"` and
  `ComponentDesktop = "desktop"` to `pkg/types/types.go`.
- [x] Add both entries to the `validComponentTypes` map in
  `pkg/extract/extractor.go`. (This is the easy-to-miss allowlist
  — see `gpu_component_type.md` memory.)
- [x] Add `case "workstation"` and `case "desktop"` switch arms in
  `pkg/extract/productkey.go`. Initial shape:
  `<type>:<vendor>:<line>:<model>` using `normalizeStr` on each
  attribute (mirrors the pre-tier server shape, no integer
  segment).
- [x] Add validators in `pkg/extract/validate.go`:
  - `validateWorkstation` and `validateDesktop`. Required:
    `vendor`, `model`. Optional: `line`, `cpu`, `gpu`, `ram_gb`,
    `storage_gb`, `form_factor`. (See Open Question 3 for required-
    set debate.)
  - Shared helpers if validation logic overlaps significantly
    (vendor must be non-empty string, model must be non-empty
    string).
- [x] Add `case domain.ComponentWorkstation` and
  `case domain.ComponentDesktop` arms to the `Validate` switch.
- [x] Add product-key tests for both new types in
  `pkg/extract/productkey_test.go`.
- [x] Add validator tests in `pkg/extract/validate_test.go`.
- [x] Add type-string tests in `pkg/types/types_test.go` if such
  tests exist. (No `pkg/types/types_test.go` exists — N/A.)
- [x] Refresh `cmd/spt/cmd/watches.go` `--type` flag help text to
  list `ram, drive, server, cpu, nic, gpu, workstation, desktop,
  other` (Open Question 8). Same edit picks up the missing `gpu`
  / `other` entries as a side benefit. Also updated
  `watches_update.go`.

#### Success Criteria

- `go build ./...` clean.
- `make test` passes including new test cases.
- `make lint` clean.
- Validator tests cover required + optional + missing-required
  failure modes.
- `spt watches create --help` shows the full ComponentType list.

---

### Phase 2: LLM surface (extraction prompts + classifier prompt + pre-classifier primaries)

Teach the LLM about the two new types and the deterministic regex
about workstation/desktop title patterns. Mirrors IMPL-0017 Phase 2.

#### Tasks

- [x] Add `workstationTmpl` and `desktopTmpl` extraction prompt
  templates to `pkg/extract/prompts.go`. Schema fields:
  `vendor`, `line`, `model`, `cpu` (optional, free-form string),
  `gpu` (optional), `ram_gb` (optional, integer), `storage_gb`
  (optional, integer), `form_factor` (optional, enum:
  `tower|sff|micro|mini`), `condition`, `quantity`, `confidence`.
  (See Open Question 2 for shared-vs-separate template debate.)
- [x] Wire both templates into the `extractTemplates` map.
- [x] Update `classifyTmpl` rules:
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
- [x] Extend `primaryComponentPatterns` in
  `pkg/extract/preclassify.go` with workstation+desktop chassis
  patterns (so a `Precision T7920 + 80mm fan` listing defers to
  the LLM rather than short-circuiting to `other`):
  - Workstation:
    `\b(precision\s+t?\d{4}|precision\s+\d{4}|pro\s+max|thinkstation|workstation|hp\s+z\d)\b`
  - Desktop (specific lines, stand-alone):
    `\b(optiplex|elitedesk|thinkcentre)\b`
  - Desktop "Dell Pro" (per Open Question 6 — require co-occurring
    desktop token to avoid "Dell ProSupport"/"Dell Pro Stand"
    false positives):
    `\bdell\s+pro\b.*\b(tower|desktop|sff|micro|mini\s+tower)\b`
    (or its commutative form)
- [x] Update `prompts_test.go` to assert the new templates render.
- [x] Update `preclassify_test.go` with workstation+desktop
  primary-pattern test cases.
- [x] Create `pkg/extract/system_preclassify.go` with a
  `DetectSystemTypeFromSpecifics(specs map[string]string) domain.ComponentType`
  function that returns `ComponentWorkstation`,
  `ComponentDesktop`, or empty string. Match logic (Open Question
  1, option c):
  - `Most Suitable For: Workstation` → `workstation`
  - `Series` contains `ThinkStation` / `Z by HP` /
    `Dell Precision` → `workstation`
  - `Series` contains `OptiPlex` / `ThinkCentre` / `EliteDesk` →
    `desktop`
  - `Product Line` contains `Precision Tower` / `Pro Max` →
    `workstation`
  - `Product Line` contains `OptiPlex` / `Pro` (with
    `Form Factor: Tower`) → `desktop`
  - Otherwise empty (defer to LLM classifier).
- [x] Wire the pre-class hook into
  `(*LLMExtractor).ClassifyAndExtract` BEFORE `Classify` is
  called. Mirror the `IsAccessoryOnly` short-circuit shape with
  appropriate logging.
- [x] Add `pkg/extract/system_preclassify_test.go` table tests
  covering: each Series/Product Line/Most Suitable For value,
  ambiguous specs return empty, missing keys return empty.

#### Success Criteria

- Classifier prompt explicitly handles workstation/desktop with no
  ambiguous overlap with `server`/`gpu`.
- Pre-classifier primary regex catches all DESIGN-0015 Q4/Q5
  shopping-list chassis names.
- `DetectSystemTypeFromSpecifics` returns the correct type for at
  least 8 representative item-specifics fixtures (one per Q4/Q5
  shopping-list line).
- LLM call count drops measurably for listings with workstation/
  desktop item specifics — verified by spot-checking a few
  ingestions in dev (Phase 5).
- All prompt and pre-classify tests pass.

---

### Phase 3: Normaliser

Per-type normaliser to canonicalise vendor / line / model spellings.
Mirrors `gpu_normalize.go` shape but smaller — the LLM is less
flaky on these fields than on GPU family.

#### Tasks

- [x] Create `pkg/extract/system_normalize.go` exposing
  `NormalizeSystemExtraction(componentType domain.ComponentType, attrs map[string]any)`
  (Open Question 4 resolved as single entry point). Internal
  helpers per concern: `canonicalizeSystemVendor`,
  `canonicalizeSystemLine`, `canonicalizeSystemModel`,
  `inferSystemLineFromModel`.
- [x] Implement `vendorAliases` map: `Dell`/`dell`/`DELL`/`Dell
  Inc.` → `dell`; `HP`/`Hewlett-Packard`/`hp`/`Hewlett Packard
  Enterprise` → `hp`; `Lenovo`/`lenovo`/`IBM`-prefixed-Lenovo →
  `lenovo`. Apply as canonicalisation, not as a hard enum.
- [x] Implement `lineAliases` map. Per Open Question 5, Dell Pro
  Max stays as a separate line (not aliased to `precision`).
  Collapse spelling variants only:
  `Precision Tower`/`Dell Precision`/`precision-tower` →
  `precision`; `Z by HP`/`Z-by-HP`/`hp z-series` → `z-by-hp`;
  `Pro Max`/`pro-max` → `pro-max`.
- [x] Implement `lineInferenceRules` per Open Question 3 — fill
  empty `line` from canonical `model`:
  - `^t\d{4}$` → `precision`
  - `^p\d{3}$` → `thinkstation`
  - `^z\d+\s+g\d+$` → `z-by-hp`
  - `^m\d{3}[a-z]?$` → `thinkcentre`
  - `^optiplex` prefix → `optiplex`
  - `^elitedesk` prefix → `elitedesk`
- [x] Implement model canonicalisation similar to
  `CanonicalizeGPUModel` — strip `dell\s+`/`hp\s+`/`lenovo\s+`
  brand prefixes from model, lowercase, collapse separator
  variants. e.g., `T7920` / `t7920` / `T-7920` → `t7920`.
- [x] Wire into `pkg/extract/normalize.go` switch — both
  `case ComponentWorkstation` and `case ComponentDesktop` arms
  call `NormalizeSystemExtraction(ct, attrs)`.
- [x] Table-driven tests for each helper, plus a round-trip
  `NormalizeSystemExtraction` table in
  `pkg/extract/system_normalize_test.go`. Cover line-from-model
  inference for every shopping-list SKU pattern.

#### Success Criteria

- 5 spelling variants of "Dell Precision T7920" produce the same
  canonical form `dell:precision:t7920`.
- LLM-omitted `line` is filled by inference for every Q4/Q5
  shopping-list SKU pattern (T-series, P-series, Z-series,
  M-series, OptiPlex, EliteDesk).
- Dell Pro Max model SKUs produce `dell:pro-max:<model>` —
  stay separate from `precision` (Open Question 5).
- `make test` includes >=20 normaliser test cases per the
  DESIGN-0015 watch shopping list.
- 100% line coverage on `system_normalize.go`.

---

### Phase 4: DB migration + integration smoke test

Drop and re-add CHECK constraints. Adds a passing smoke test that
exercises the full classify + extract path for one workstation and
one desktop title.

#### Tasks

- [x] Create `migrations/011_add_workstation_and_desktop_component_types.sql`
  and the embedded copy in
  `internal/store/migrations/011_add_workstation_and_desktop_component_types.sql`.
  Both files identical, mirrors migration 010 shape:
  `ALTER TABLE watches DROP CONSTRAINT watches_component_type_check;`
  followed by re-add with `'workstation', 'desktop'` added to
  the IN list. Same for `listings_component_type_check`.
- [x] Verify `internal/store/migrations/` is included in
  `embed.FS` (it is — same path as migration 010).
- [x] Add `pkg/extract/extractor_test.go` integration smoke cases
  for `TestLLMExtractor_ClassifyAndExtract`:
  - Workstation case (Dell Precision T7920 listing) — expect
    `ComponentWorkstation` + populated attributes + canonical
    product key.
  - Desktop case (Dell OptiPlex 7080 listing) — expect
    `ComponentDesktop` + populated attributes + canonical
    product key.
  Use `MockLLMBackend` returning canned JSON responses (same shape
  as the existing GPU case). Plus a separate
  `TestLLMExtractor_ClassifyAndExtract_SystemPreClassHook` that
  verifies the item-specifics short-circuit skips Classify entirely.

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
| `pkg/extract/system_preclassify.go` | Create | Item-specifics short-circuit (`Most Suitable For`, `Series`) → workstation/desktop |
| `pkg/extract/system_preclassify_test.go` | Create | Pre-class hook table tests |
| `pkg/extract/extractor.go` | Modify | Wire `system_preclassify` into `ClassifyAndExtract` before `Classify` |
| `pkg/extract/normalize.go` | Modify | Add switch arms calling system normaliser |
| `pkg/extract/system_normalize.go` | Create | Vendor + line + model canonicalisation, line-from-model inference |
| `pkg/extract/system_normalize_test.go` | Create | Table-driven tests |
| `pkg/extract/extractor_test.go` | Modify | Add `ClassifyAndExtract` smoke tests for both types |
| `cmd/spt/cmd/watches.go` | Modify | Refresh `--type` flag help text to include `gpu, workstation, desktop, other` |
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

All implementation-level questions resolved before Phase 1.

1. **Item-specifics-aware classifier** — _resolved: pre-class hook
   (option c)._ A new step before `Classify` inspects item specifics
   (`Most Suitable For`, `Series`, `Product Line`, `Type`) and short-
   circuits to `workstation` / `desktop` when a known workstation /
   desktop value matches; otherwise falls through to the LLM
   classifier. Mirrors the `IsAccessoryOnly` pre-classifier shape.
   Cheaper than always-on LLM and avoids the `Classify` signature
   change. New file: `pkg/extract/system_preclassify.go` (suggested).

2. **Shared vs. separate extraction templates** — _resolved:
   separate (option b)._ `workstationTmpl` and `desktopTmpl` as two
   discrete `const` strings in `prompts.go`, matching the existing
   per-type convention. Tiny duplication cost is preferable to a
   shared template that gets harder to evolve when schemas diverge.

3. **Required attribute set per type** — _resolved: `vendor` +
   `model` required, `line` optional with normaliser-fill._ The
   normaliser infers `line` from known model patterns:
   - `^t\d{4}$` → `precision` (Dell Precision T-series)
   - `^p\d{3}$` → `thinkstation` (Lenovo ThinkStation P-series)
   - `^z\d+\s+g\d+$` → `z-by-hp` (HP Z-series)
   - `^m\d{3}[a-z]?$` → `thinkcentre` (Lenovo ThinkCentre M-series)
   - `^optiplex` prefix → `optiplex`
   - `^elitedesk` prefix → `elitedesk`
   - Ambiguous → leave empty, product key gets `unknown` segment.
   `cpu`, `gpu`, `ram_gb`, `storage_gb`, `form_factor` all optional.

4. **Single normalisation entry point or two** — _resolved: one
   shared._ `pkg/extract/system_normalize.go` exposes
   `NormalizeSystemExtraction(componentType, attrs)` called from
   both `case ComponentWorkstation` and `case ComponentDesktop`
   arms in `normalize.go`. Vendor + model + line canonicalisation
   shared; per-type inference rules diverge slightly inside the
   function.

5. **Dell Pro Max → Precision aliasing** — _resolved: separate
   lines for v1._ `workstation:dell:precision:t7920` and
   `workstation:dell:pro-max:<model>` stay distinct product keys
   even though buyers cross-shop. Matches Dell's catalog and keeps
   the post-rebrand pricing curve separable. Revisit if baselines
   on either side stay thin >30 days.

6. **Pre-classifier `dell pro` ambiguity** — _resolved: require
   co-occurring desktop token._ The desktop primary regex requires
   `dell\s+pro\b` AND one of `tower|desktop|optiplex|sff|micro|
   mini\s+tower`. Listings without a co-token defer to the LLM
   classifier; don't grab false positives ("Dell ProSupport",
   "Dell Pro Stand") at the regex layer. Other desktop primaries
   (optiplex / elitedesk / thinkcentre) are specific enough to
   stand alone.

7. **Re-extraction strategy on rollout** — _resolved: Phase 7
   backfill (operator-gated)._ No mandatory backfill on rollout.
   Operator decides after observing Phase 6 maturity rate; Phase 7
   SQL helper documented for when needed.

8. **Watch CLI help-text staleness** — _resolved: fix in Phase 1._
   One-line edit to `cmd/spt/cmd/watches.go` flag description
   string. Add `gpu, workstation, desktop, other` to the `--type`
   help text so it accurately reflects the current
   `validComponentTypes` set. Prevents the "wait, can I use type=X?"
   confusion the GPU rollout surfaced.

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
