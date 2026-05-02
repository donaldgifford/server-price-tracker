---
id: IMPL-0017
title: "DESIGN-0012 GPU component type phase plan"
status: In Progress
author: Donald Gifford
created: 2026-05-01
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0017: DESIGN-0012 GPU component type phase plan

**Status:** In Progress
**Author:** Donald Gifford
**Date:** 2026-05-01

<!--toc:start-->
- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Domain wiring (enum + product key + validator)](#phase-1-domain-wiring-enum--product-key--validator)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: LLM surface (extraction prompt + classifier prompt + pre-classifier primaries)](#phase-2-llm-surface-extraction-prompt--classifier-prompt--pre-classifier-primaries)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Normalisation (canonicalisation, family inference, VRAM repairs)](#phase-3-normalisation-canonicalisation-family-inference-vram-repairs)
    - [Tasks](#tasks-2)
      - [Deferred sub-task (skip in Phase 1; revisit if needed)](#deferred-sub-task-skip-in-phase-1-revisit-if-needed)
    - [Success Criteria](#success-criteria-2)
  - [Phase 4: Sample watch + integration smoke](#phase-4-sample-watch--integration-smoke)
    - [Tasks](#tasks-3)
    - [Success Criteria](#success-criteria-3)
  - [Phase 5: PR + dev deploy + smoke validation](#phase-5-pr--dev-deploy--smoke-validation)
    - [Tasks](#tasks-4)
    - [Success Criteria](#success-criteria-4)
  - [Phase 6: Production rollout + 1 week watch](#phase-6-production-rollout--1-week-watch)
    - [Tasks](#tasks-5)
    - [Success Criteria](#success-criteria-5)
  - [Phase 7 (post-work, optional): Backfill misclassified GPUs](#phase-7-post-work-optional-backfill-misclassified-gpus)
    - [Tasks](#tasks-6)
    - [Success Criteria](#success-criteria-6)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Open Questions](#open-questions)
- [Dependencies](#dependencies)
- [References](#references)
<!--toc:end-->

## Objective

Implement DESIGN-0012 — add `gpu` as a first-class `ComponentType` with a
dedicated extraction prompt, validator, product-key format, pre-classifier
primaries, and normalisation. Seed one example watch with a cold-start
threshold. Validate the new bucket fills correctly in dev before promoting
to prod, then watch baseline growth over ~1 week.

**Implements:** DESIGN-0012

## Scope

### In Scope

- New `domain.ComponentGPU = "gpu"` enum value in `pkg/types/types.go`.
- New `gpuTmpl` extraction prompt in `pkg/extract/prompts.go` and
  registration in the `templates` map.
- New `validateGPU(attrs)` in `pkg/extract/validate.go` covering the
  required and optional fields per DESIGN-0012 Section 3.
- New `gpu:<manufacturer>:<family>:<model>:<vram>gb` product-key format
  in `pkg/extract/productkey.go`.
- Update `classifyTmpl` (also in `prompts.go`) to enumerate `gpu` and
  give the LLM accessory-vs-GPU routing rules.
- Extend `primaryComponentPatterns` in `pkg/extract/preclassify.go`
  with GPU brand/family tokens (Tesla, Quadro, RTX A-series, A/H/L100
  series, MI series, Radeon Pro) so chassis+GPU titles defer to the
  LLM correctly. Existing `gpu riser` accessory rule untouched.
- New `gpu_normalize.go` (or extension to `normalize.go`) implementing
  the Section 7 normalisation: VRAM unit confusion repair, family
  canonicalisation map, family inference from model prefix, power-of-2
  VRAM rounding.
- Sample watch in `configs/config.example.yaml` and `configs/config.dev.yaml`
  (Tesla P40, threshold 65 during cold start).
- Unit tests in every touched package; integration test fixtures for
  3–5 representative GPU titles.
- Single PR opened, CI green, deploy to dev, run smoke validation,
  promote to prod.

### Out of Scope

- **Database migrations.** None required — `attributes` is JSONB.
- **Scoring weight changes.** Shared `Score()` is component-agnostic.
- **Retroactive backfill** of historical `other` listings. Captured as
  Phase 7 follow-up.
- **GPU-aware server scoring** (where a server's score factors in
  whether it includes GPUs). Filed as a future enhancement in
  DESIGN-0012 §Open Questions.
- **Discord channel routing for GPU alerts** — that work happens in
  DESIGN-0013, separate PR.

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its
tasks are checked off and its success criteria are met.

---

### Phase 1: Domain wiring (enum + product key + validator)

Add the deterministic, fully-tested foundation: enum, product-key
format, and validator. All three are independent of LLM behavior so
they can ship and stay green without any prompt iteration.

#### Tasks

- [x] Add `ComponentGPU ComponentType = "gpu"` to the const block in
  `pkg/types/types.go` (slot it before `ComponentOther` so the
  catch-all stays last).
- [x] Add `case "gpu":` to `pkg/extract/productkey.go::ProductKey`
  that returns `gpu:<manufacturer>:<family>:<model>:<vram>gb`. Use
  `normalizeStr` for manufacturer/family/model and `pkInt` for
  `vram_gb`. Format: `fmt.Sprintf("gpu:%s:%s:%s:%dgb", ...)`.
- [x] Add `case domain.ComponentGPU:` to
  `pkg/extract/validate.go::ValidateExtraction` calling
  `validateGPU(attrs)`.
- [x] Implement `validateGPU(attrs)` split into
  `validateGPURequired` + `validateGPUOptional` to keep
  cyclomatic-complexity under 15. Constants
  `validGPUManufacturers`, `validGPUMemoryTypes`,
  `validGPUInterfaces`, `validGPUFormFactors`, `validGPUCoolings`
  mirror the per-type pattern.
- [x] Write `pkg/extract/validate_test.go` GPU cases — happy path,
  required-field omissions, vram boundary cases, tdp boundary cases,
  free-form family, and per-enum invalid cases. 22 sub-tests.
- [x] Write `pkg/extract/productkey_test.go` GPU cases — Tesla P40,
  Instinct MI210, family-null fallback, float64 vram, empty attrs.
- [x] Run `make fmt`, `make lint`, `go test ./pkg/extract/...`.
- [x] Commit `feat(extract): add gpu component type — enum, validator,
  product key` (commit `4116b57`).

#### Success Criteria

- [x] `go build ./...` succeeds.
- [x] `go test ./pkg/extract/...` passes.
- [x] `pkg/extract/` package coverage 92.4%.
- [x] `make lint` clean — 0 issues.
- [x] Existing tests for ram/drive/server/cpu/nic still pass — additive
  change.

---

### Phase 2: LLM surface (extraction prompt + classifier prompt + pre-classifier primaries)

Wire the LLM-facing surface so newly-ingested GPU listings are routed
to `gpu` instead of `other`. The classifier prompt addition is the
load-bearing change for forward classification. The pre-classifier
primaries protect the regex pre-pass from misclassifying real GPUs as
accessories.

#### Tasks

- [x] Add `gpuTmpl` constant to `pkg/extract/prompts.go` per
  DESIGN-0012 §2 schema. Required fields: `manufacturer`, `model`,
  `vram_gb`, `condition`, `confidence`. Includes the rules block.
- [x] Register the new template in the `init()` block.
- [x] Update `classifyTmpl`: added `gpu` to the types list, added GPU
  routing rule, added explicit "gpu riser/bracket/cable stay in
  other" rule.
- [x] Extend `primaryComponentPatterns` in
  `pkg/extract/preclassify.go` with the GPU primary regex.
- [x] Write `pkg/extract/preclassify_test.go` GPU cases — 8 new
  sub-tests covering Tesla, Quadro, A100, H100, MI210, RTX A-series,
  Radeon Pro, plus the "gpu riser cable" still-accessory case.
- [x] Write `pkg/extract/prompts_test.go` GPU cases — classifier
  guidance check + GPU extract template render assertions.
- [x] Run `make fmt`, `make lint`, `go test ./pkg/extract/...`.
- [x] Commit `feat(extract): wire gpu into prompts and pre-classifier`
  (commit `efcb001`).

#### Success Criteria

- [x] `go test ./pkg/extract/...` passes.
- [x] `pkg/extract/` package coverage 92.4%.
- [x] "Tesla P40 + heatsink" test confirms primary-defer behaviour.
- [x] `gpu` appears in the classifier prompt types enumeration; GPU
  routing rule and accessory-exception rule both render.

---

### Phase 3: Normalisation (canonicalisation, family inference, VRAM repairs)

Add the GPU-specific repairs that run before validation. This phase
exists separately from Phase 1 because it depends on the prompt being
in place to know what shapes the LLM tends to return wrongly. Repairs
defend the validator from common LLM mistakes.

#### Tasks

- [x] Create `pkg/extract/gpu_normalize.go` with:
  - `CanonicalizeGPUFamily(s string) string` — lowercase, whitespace
    trim/collapse, mapped to canonical token per DESIGN-0012 §7
    (`tesla`, `quadro`, `geforce`, `rtx`, `a-series`, `l-series`,
    `h-series`, `radeon-pro`, `instinct`, `arc`). Unknown values fall
    through as lowercased + spaces-to-hyphens.
  - `DetectGPUFamilyFromModel(model string) string` — regex-based
    inference, **tightened to high-confidence patterns only** per
    Open Q5:
    - `tesla`: `^(P40|P100|V100|K80|M40|M60|T4)$`
    - `a-series`: `^A(10|30|40|100)$`
    - `l-series`: `^L(4|40|40S)$`
    - `h-series`: `^H(100|200)$`
    - `instinct`: `^MI(50|60|100|210|250|300)$`
    Returns empty string for unknown / ambiguous (P4000, RTX 4000,
    etc.) — better to leave `family` blank than mis-infer.
  - `NormalizeGPUExtraction(attrs map[string]any)` — top-level entry
    point that performs:
    1. **VRAM unit confusion**: if `vram_gb` is in 1024–262144,
       divide by 1024. If in 1000–256000, divide by 1000.
    2. **Family canonicalisation**: if `family` is non-empty, replace
       with `CanonicalizeGPUFamily(family)`.
    3. **Family inference**: if `family` is empty after canonicalisation
       and `model` is non-empty, fill from
       `DetectGPUFamilyFromModel(model)`.
    4. **VRAM rounding**: round `vram_gb` to nearest valid SKU
       (`[8, 12, 16, 24, 32, 40, 48, 80, 96, 128]`) **only when
       within ±1 GB of one**. Out-of-list values (14, 20, 28, etc.)
       stay unchanged — defends legitimate oddball-VRAM cards.
- [x] Hook `NormalizeGPUExtraction` into
  `pkg/extract/normalize.go::NormalizeExtraction` under
  `case domain.ComponentGPU:`. Run before the existing common
  normalisation (capacity unit confusion is RAM-specific; GPU has its
  own).
- [x] Write `pkg/extract/gpu_normalize_test.go` (table-driven):
  - VRAM unit cases: `24576` (MB) → `24`; `24000000` (KB-ish, out
    of range) → unchanged; `24` (already GB) → `24`.
  - Canonicalisation: `"Tesla"` → `"tesla"`, `"  TESLA  "` → `"tesla"`,
    `"Ampere"` → `"a-series"`, `"hopper"` → `"h-series"`,
    `"Radeon Pro"` → `"radeon-pro"`, `"some new family"` →
    `"some-new-family"`.
  - Family inference: model `"P40"` with empty family → `"tesla"`;
    model `"A100"` → `"a-series"`; model `"MI210"` → `"instinct"`;
    model `"unknown-x"` → empty (no inference).
  - VRAM rounding: 23 → 24, 25 → 24, 39 → 40, 81 → 80, 13 → 12,
    7 → 8, 15 → 16, exact (24, 80, etc.) → unchanged. Out-of-list
    cases stay unchanged: 14 → 14, 20 → 20, 28 → 28.
- [x] Update `pkg/extract/normalize_test.go` to confirm GPU
  normalisation runs in the right order (before validation).
- [x] Run `make fmt`, `make lint`, `go test ./pkg/extract/...`.
- [x] Commit `feat(extract): add gpu normalisation — vram, family
  canonicalisation, model inference` (commit `c532dae`).

##### Deferred sub-task (skip in Phase 1; revisit if needed)

Per Open Q3, when `vram_gb` is missing the listing fails validation.
If post-deploy telemetry shows a meaningful "stuck unextracted" rate
on cards we care about, add a `LookupVRAMByModel(model string) int`
helper to `gpu_normalize.go` covering single-variant cards only:

| Model | VRAM (GB) |
|-------|-----------|
| P40 | 24 |
| P100-PCIe | 16 |
| K80 | 24 (12 per GPU × 2; treat as one unit) |
| MI100 | 32 |
| H100-PCIe | 80 |
| H200 | 141 |

**Multi-variant cards excluded** — A100 (40/80), V100 (16/32),
MI250 (64/128) need title-text inference and aren't covered by a
static lookup. Defer until there's evidence the table would help.

#### Success Criteria

- [x] `go test ./pkg/extract/...` passes.
- [x] `gpu_normalize.go` coverage 100%.
- [x] Round-trip test confirms `{vram_gb=24576, family=" Tesla ",
  model="P40"}` → `{vram_gb=24, family="tesla", model="P40"}`.

---

### Phase 4: Sample watch + integration smoke

Seed one example watch in dev/example configs and add an integration
test that exercises the full flow (classify → extract → product key
→ baseline placeholder) end-to-end with a stub LLM.

#### Tasks

- [x] Document GPU sample watch in `docs/OPERATIONS.md` (configs/
  files don't carry watches — managed via API/CLI). Includes the
  cold-start threshold rationale and a baseline-maturity check
  query.
- [x] `pkg/extract/extractor_test.go::TestLLMExtractor_ClassifyAndExtract`
  gains 3 GPU sub-tests covering Tesla P40, A100 with VRAM unit
  confusion (81920 → 80), and MI210 with family canonicalisation
  (Instinct → instinct).
- [x] Add GPU `case domain.ComponentGPU` to `validComponentTypes`
  map in `extractor.go` so `Classify` accepts the LLM response.
  (Discovered during integration testing; would have shipped silent
  bug otherwise.)
- [x] Update existing "invalid type from LLM" test which used `gpu`
  as a placeholder — now uses `psu` (still invalid, real edge case).
- [x] Run `make fmt`, `make lint`, `go test ./...`.
- [x] Commit `feat(extract): gpu integration smoke test and classify map entry`
  (commit `47ff329`).

#### Success Criteria

- [x] All existing tests still pass; new GPU integration test green.
- [x] `make lint` clean.
- [x] Manual e2e deferred to Phase 5 dev deploy.

---

### Phase 5: PR + dev deploy + smoke validation

Open the PR, get CI green, deploy the dev image, watch the first few
ingestion cycles surface GPU listings into the new bucket. Validate the
classification rate looks right (i.e., real GPU titles land in `gpu`,
not `other`) before approving prod.

#### Tasks

- [x] Push the branch, open PR with `feature` label (PR #47).
- [x] CI must be green (lint, test, build, security, docker-build,
  helm-lint, helm-unittest, helm-ct). All 11 checks pass on PR #47
  as of commit `c1ca108`.
- [ ] Operator deploys dev image (`ghcr.io/donaldgifford/server-price-tracker:dev`).
- [ ] Wait for one full ingestion cycle on the example GPU watch.
- [ ] SQL smoke check (run via psql against dev):
  ```sql
  SELECT component_type, COUNT(*)
  FROM listings
  WHERE component_type = 'gpu'
    AND created_at > NOW() - INTERVAL '1 hour'
  GROUP BY component_type;
  ```
  Confirms at least one GPU listing was classified.
- [ ] Spot-check 5 GPU listings:
  ```sql
  SELECT id, title, component_type, attributes, product_key
  FROM listings
  WHERE component_type = 'gpu'
  ORDER BY created_at DESC
  LIMIT 5;
  ```
  Verify product_keys look right (`gpu:nvidia:tesla:p40:24gb`),
  attributes are populated, no "unknown:unknown:..." keys outside
  cold-start expectations.
- [ ] Spot-check the `other` bucket for _missed_ GPUs:
  ```sql
  SELECT id, title FROM listings
  WHERE component_type = 'other'
    AND title ILIKE ANY (ARRAY['%tesla%', '%quadro%', '%rtx %', '%a100%', '%h100%'])
    AND created_at > NOW() - INTERVAL '24 hours'
  LIMIT 10;
  ```
  If results > 0, classifier prompt may need tuning. If zero, ship.
- [ ] Document smoke findings in PR comment.

#### Success Criteria

- CI green; PR approved.
- Dev deploy ingests at least 1 GPU listing in the first 30 min.
- Spot-check confirms attributes and product_key look correct.
- Missed-GPU query returns ≤ 1 result (allowing for legitimate edge
  cases like ambiguous "RTX 4000" titles).

---

### Phase 6: Production rollout + 1 week watch

Merge the PR, deploy to prod, observe GPU listings populating the new
bucket. Bump the watch threshold from 65 → 80 once the baseline reaches
`MinBaselineSamples=10` for at least one product_key.

#### Tasks

- [ ] Merge PR #47 to main.
- [ ] Confirm release workflow tags + builds + publishes prod image.
- [ ] Operator deploys prod (Helm release tagged with new
  `appVersion`).
- [ ] Trigger initial baseline + rescore:
  ```bash
  curl -X POST https://spt.fartlab.dev/api/v1/baselines/refresh
  curl -X POST https://spt.fartlab.dev/api/v1/rescore
  ```
- [ ] Monitor `spt_alerts_created_total{component_type="gpu"}` over
  ~24h. Expect: low volume initially because the bucket is small and
  baselines neutral.
- [ ] After ~7 days, check baseline maturity:
  ```sql
  SELECT product_key, sample_count
  FROM price_baselines
  WHERE product_key LIKE 'gpu:%'
  ORDER BY sample_count DESC;
  ```
  When at least one key has sample_count ≥ 10, scoring becomes
  non-neutral.
- [ ] Bump the GPU watch threshold from 65 → 80 via
  `spt watches update --id <id> --score-threshold 80`.
- [x] Document the production transition in
  `docs/SQL_HELPERS.md` ("GPU baseline maturity check") so the
  operator has a reusable query.

#### Success Criteria

- Prod has at least one `gpu:<...>` product_key with sample_count ≥ 10
  within 14 days.
- No spike in misclassified `other` listings — accessory bucket
  growth rate unchanged versus pre-deploy 7-day baseline.
- No alert noise spike for type=gpu (alerts/unique_keys ratio stays
  ~1, per the dedup logic from PR #46).

---

### Phase 7 (post-work, optional): Backfill misclassified GPUs

Historical listings classified as `other` whose titles match the new
GPU primaries should be re-classified as `gpu` so the new bucket isn't
slow to mature. **Optional** — skip if Phase 6 baselines mature on
their own without backfill.

#### Tasks

- [x] Add a "Backfill misclassified GPUs (IMPL-0017 Phase 7)" section
  to `docs/SQL_HELPERS.md` with the SQL pattern that mirrors the new
  `primaryComponentPatterns` GPU regex (Postgres regex uses `\y` for
  word boundaries):
  ```sql
  -- DRY-RUN FIRST
  BEGIN;
  UPDATE listings
  SET component_type = 'gpu'
  WHERE component_type = 'other'
    AND title ~* '\y(tesla|quadro|rtx\s+a\d+|a100|h100|l40|mi\d{3}|radeon\s+pro)\y'
  RETURNING id, title, component_type;
  -- eyeball results, then COMMIT or ROLLBACK
  ```
- [x] Reference `feedback_dry_run_bulk_sql.md` discipline — always
  `BEGIN;` first, eyeball `RETURNING`, then commit.
- [x] After commit, listings will have stale `attributes` (extracted
  under the wrong type). Two options:
  - (a) Re-queue them for extraction:
    `INSERT INTO extraction_queue (listing_id) SELECT id FROM listings WHERE component_type = 'gpu' AND attributes = '{}'::jsonb ON CONFLICT DO NOTHING;`
  - (b) Leave attributes empty until next ingestion refresh.
  - Operator chooses based on volume.
- [x] Run `POST /api/v1/baselines/refresh` and `POST /api/v1/rescore`
  after the backfill.

#### Success Criteria

- Phase 6 GPU baselines reach `sample_count ≥ 10` faster (within
  ~3 days instead of ~7).
- No regression in non-GPU baselines (server, ram, etc.) — backfill is
  scoped to `component_type = 'other'`.
- `docs/SQL_HELPERS.md` documents the procedure for future
  ComponentType additions.

---

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `pkg/types/types.go` | Modify | Add `ComponentGPU` enum value |
| `pkg/extract/prompts.go` | Modify | Add `gpuTmpl`, register in `templates`, update `classifyTmpl` |
| `pkg/extract/validate.go` | Modify | Add `validateGPU` + supporting enum slices |
| `pkg/extract/productkey.go` | Modify | Add `case "gpu"` returning `gpu:<mfg>:<family>:<model>:<vram>gb` |
| `pkg/extract/preclassify.go` | Modify | Append GPU primary regex to `primaryComponentPatterns` |
| `pkg/extract/gpu_normalize.go` | Create | `NormalizeGPUExtraction`, `CanonicalizeGPUFamily`, `DetectGPUFamilyFromModel` |
| `pkg/extract/normalize.go` | Modify | Wire GPU normalisation into `NormalizeExtraction` switch |
| `pkg/extract/validate_test.go` | Modify | Add GPU validation table |
| `pkg/extract/productkey_test.go` | Modify | Add GPU product-key cases |
| `pkg/extract/preclassify_test.go` | Modify | Add Tesla+heatsink and other primary tests |
| `pkg/extract/prompts_test.go` | Modify | Add GPU template render assertion |
| `pkg/extract/gpu_normalize_test.go` | Create | Full coverage on canonicalisation, inference, VRAM repair |
| `pkg/extract/extractor_test.go` | Modify | New `TestLLMExtractor_GPUFlow` end-to-end stub |
| `configs/config.example.yaml` | Modify | Add disabled GPU sample watch |
| `configs/config.dev.yaml` | Modify | Add enabled Tesla P40 watch |
| `docs/SQL_HELPERS.md` | Modify | Add Phase 7 backfill procedure (after Phase 7 lands) |
| `docs/EXTRACTION.md` | Modify | Document GPU schema and product-key format |
| `CLAUDE.md` | Modify | Mention `gpu` in pipeline step descriptions |

## Testing Plan

- **Unit tests**: each touched file in `pkg/extract/` gets table-driven
  tests covering happy path, error paths, and boundary cases.
  `t.Parallel()` everywhere.
- **Coverage targets**: ≥90% on `pkg/extract/` package.
- **Integration smoke**: `TestLLMExtractor_GPUFlow` runs the full
  classify→normalise→validate pipeline with a stub LLM and three
  representative cards.
- **Manual e2e in Phase 5**: real eBay ingest against the example
  watch in dev. Spot-check 5 listings + the `other`-leakage query.

## Open Questions

All implementation-level questions resolved before Phase 1.

1. **Family canonicalisation file placement** — _resolved: new file
   `gpu_normalize.go`._ Mirrors the existing per-type pattern
   (`pc4.go` for RAM, `server_tier.go` for server).

2. **GPU primary regex placement in `preclassify.go`** — _resolved:
   append to existing `primaryComponentPatterns` (option a)._ One list
   to read; consistent with existing architecture.

3. **Required `vram_gb` failure mode** — _resolved: keep required at
   validation._ Cards without VRAM in the title fail validation and
   stay unextracted.

   _Deferred enhancement (filed as Phase 3 follow-up, see Section 3
   sub-task)_: a model→VRAM lookup table for high-value
   _single-variant_ cards (P40=24, P100-PCIe=16, K80=24, MI100=32).
   Multi-variant cards (A100 ships in 40 and 80, V100 in 16 and 32,
   MI210 in 64 only but MI250 in 128) need title-text inference and
   aren't covered by a static table. Skip in Phase 1; revisit if the
   "stuck unextracted" rate is high enough to matter.

4. **VRAM rounding boundary** — _resolved: ±1 GB only when within
   range of a known SKU._ Valid SKUs are
   `[8, 12, 16, 24, 32, 40, 48, 80, 96, 128]`. Out-of-list values
   (14, 20, 28) stay unchanged — the product key gets the literal
   number, e.g., `14gb`. Defends against both LLM mis-rounding and
   over-eager normalisation of legitimate odd-VRAM cards.

5. **Family inference regex precision** — _resolved: tighten to
   high-confidence patterns only._ Initial set:
   - `tesla`: `^(P40|P100|V100|K80|M40|M60|T4)$`
   - `a-series`: `^A(10|30|40|100)$`
   - `l-series`: `^L(4|40|40S)$`
   - `h-series`: `^H(100|200)$`
   - `instinct`: `^MI(50|60|100|210|250|300)$`
   No inference for ambiguous prefixes (P4000, RTX 4000, etc.) — let
   `family` stay empty and the product key uses `unknown` for that
   segment.

6. **Sample watch threshold timing** — _resolved: manual._ Operator
   bumps via `spt watches update` after Phase 6 baseline-maturity
   check. Automation is a future enhancement.

7. **Phase 7 trigger** — _resolved: operator-gated._ Backfill SQL is
   documented but only run if Phase 6 baselines aren't maturing fast
   enough. `feedback_dry_run_bulk_sql.md` discipline applies.

8. **Discord embed casing for `gpu`** — _resolved: leave as-is._ The
   existing label rendering shows the lowercased ComponentType; if it
   looks wrong post-deploy, capitalising the label is a tiny follow-up.

## Dependencies

- DESIGN-0012 (Accepted) — the source of all design decisions.
- DESIGN-0011 / IMPL-0016 (Implemented) — pre-classifier infrastructure
  this implementation extends.
- PR #46 (merged to main) — alert dedup machinery; means GPU alerts
  won't immediately blast on first deploy.
- PR #47 (this branch) — opened on `feat/add-gpu-component-type`.

## References

- `docs/design/0012-add-gpu-as-a-new-component-type.md` — DESIGN-0012
- `docs/design/0011-reduce-alert-noise-via-scoring-recalibration-and-accessory-pre.md` — DESIGN-0011
- `docs/impl/0016-design-0011-alert-noise-reduction-phase-plan.md` — IMPL-0016 (template for this doc's shape)
- `docs/EXTRACTION.md` — extraction pipeline reference; will need GPU section after Phase 1
- `pkg/extract/server_tier.go` — example of a per-component normalisation file (pattern this implementation mirrors with `gpu_normalize.go`)
- `pkg/extract/preclassify.go` — primary-component pattern list this implementation extends
- Memory: `feedback_dry_run_bulk_sql.md` — Phase 7 discipline
- Memory: `preclassify_short_circuit.md` — pre-classifier architecture
