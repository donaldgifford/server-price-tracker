---
id: IMPL-0017
title: "DESIGN-0012 GPU component type phase plan"
status: Draft
author: Donald Gifford
created: 2026-05-01
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0017: DESIGN-0012 GPU component type phase plan

**Status:** Draft
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

- [ ] Add `ComponentGPU ComponentType = "gpu"` to the const block in
  `pkg/types/types.go` (slot it before `ComponentOther` so the
  catch-all stays last).
- [ ] Add `case "gpu":` to `pkg/extract/productkey.go::ProductKey`
  that returns `gpu:<manufacturer>:<family>:<model>:<vram>gb`. Use
  `normalizeStr` for manufacturer/family/model and `pkInt` for
  `vram_gb`. Format: `fmt.Sprintf("gpu:%s:%s:%s:%dgb", ...)`.
- [ ] Add `case domain.ComponentGPU:` to
  `pkg/extract/validate.go::ValidateExtraction` calling
  `validateGPU(attrs)`.
- [ ] Implement `validateGPU(attrs)`:
  - `manufacturer`: required enum (`NVIDIA`, `AMD`, `Intel`).
  - `model`: required non-empty string.
  - `vram_gb`: required, range 1–256.
  - `family`: optional, free-form string (no enum check at this layer).
  - `memory_type`: optional enum (`GDDR5`, `GDDR6`, `GDDR6X`, `HBM2`,
    `HBM2e`, `HBM3`).
  - `interface`: optional enum (`PCIe 3.0 x16`, `PCIe 4.0 x16`,
    `PCIe 5.0 x16`, `SXM2`, `SXM4`, `SXM5`).
  - `tdp_watts`: optional, range 15–700.
  - `form_factor`: optional enum (`single_slot`, `dual_slot`,
    `triple_slot`, `FHFL`, `HHHL`, `LP`).
  - `cooling`: optional enum (`passive`, `active`, `blower`).
  - Add validator-level constants `validGPUManufacturers`,
    `validGPUMemoryTypes`, `validGPUInterfaces`, `validGPUFormFactors`,
    `validGPUCoolings` mirroring the existing per-type pattern.
- [ ] Write `pkg/extract/validate_test.go` GPU cases (table-driven,
  `t.Parallel()`):
  - Happy path with all fields populated.
  - Each required-field omission (manufacturer, model, vram_gb).
  - Each enum-out-of-range case for the optional enums.
  - `vram_gb` boundary cases (0, 1, 256, 257).
  - `tdp_watts` boundary cases (14, 15, 700, 701).
- [ ] Write `pkg/extract/productkey_test.go` GPU cases:
  - Standard: Tesla P40 24GB → `gpu:nvidia:tesla:p40:24gb`.
  - Family null → `unknown` segment.
  - VRAM zero → `0gb` segment (validation will catch real cases).
  - AMD Instinct MI210 64GB → `gpu:amd:instinct:mi210:64gb`.
- [ ] Run `make fmt`, `make lint`, `go test ./pkg/extract/...`.
- [ ] Commit `feat(extract): add gpu component type — enum, validator,
  product key`.

#### Success Criteria

- `go build ./...` succeeds.
- `go test ./pkg/extract/...` passes.
- `pkg/extract/validate.go` and `pkg/extract/productkey.go` reach ≥90%
  coverage.
- `make lint` clean.
- Existing tests for ram/drive/server/cpu/nic still pass — additive
  change.

---

### Phase 2: LLM surface (extraction prompt + classifier prompt + pre-classifier primaries)

Wire the LLM-facing surface so newly-ingested GPU listings are routed
to `gpu` instead of `other`. The classifier prompt addition is the
load-bearing change for forward classification. The pre-classifier
primaries protect the regex pre-pass from misclassifying real GPUs as
accessories.

#### Tasks

- [ ] Add `gpuTmpl` constant to `pkg/extract/prompts.go` per
  DESIGN-0012 §2 schema. Required fields: `manufacturer`, `model`,
  `vram_gb`, `condition`, `confidence`. Include the rules block:
  - `manufacturer` derived from title brand token.
  - `vram_gb` is in **GB** integer (not bytes/MB).
  - `family` is a free-form short string (e.g., "Tesla", "Quadro",
    "RTX", "Ampere", "Hopper") — the normaliser canonicalises later.
  - `condition` defaults to `"unknown"` if not specified.
- [ ] Register the new template in the `init()` block:
  `domain.ComponentGPU: template.Must(template.New("gpu").Parse(gpuTmpl))`.
- [ ] Update `classifyTmpl` to:
  - Add `gpu` to the `Types:` enumeration line.
  - Add a new rule: `Pick "gpu" for actual graphics cards / accelerators
    (Tesla, Quadro, RTX, A/L/H-series, Radeon Pro, Instinct, Arc).`
  - Add a clarifying rule: `"gpu riser", "GPU bracket", and "GPU power
    cable" stay in "other".`
- [ ] Extend `primaryComponentPatterns` in
  `pkg/extract/preclassify.go` with one GPU primary regex:
  ```go
  regexp.MustCompile(`\b(tesla|quadro|rtx\s+a\d+|a100|h100|l40|mi\d{3}|radeon\s+pro)\b`),
  ```
  Reasoning per DESIGN-0012 §6: bare-accessory regex matching ("tray",
  "rail", "heatsink") on a "Dell R740 + Tesla P40" listing should
  defer to the LLM.
- [ ] Write `pkg/extract/preclassify_test.go` GPU cases:
  - "Tesla P40 + heatsink" → defers to LLM (false from
    `IsAccessoryOnly`).
  - "GPU riser cable" → still classified as accessory (true).
  - "Quadro RTX 4000 8GB" → not accessory (true neg).
  - "Nvidia A100 40GB SXM4 cooling kit" → defers to LLM (mixed).
- [ ] Write `pkg/extract/prompts_test.go` GPU cases:
  - Template renders without error for sample `PromptData`.
  - Rendered output contains `"gpu"`, `"vram_gb"`, `"NVIDIA"`,
    `"Tesla"`, `"family"`.
  - Classifier prompt now contains `gpu` in the types list.
- [ ] Run `make fmt`, `make lint`, `go test ./pkg/extract/...`.
- [ ] Commit `feat(extract): wire gpu into prompts and pre-classifier`.

#### Success Criteria

- `go test ./pkg/extract/...` passes.
- `preclassify.go` coverage stays ≥90% (no regression from new
  primaries).
- A "Tesla P40 + heatsink"-style title path is exercised by a test
  and confirmed to *not* short-circuit to accessory.
- Manual sanity check: render the classifier prompt with a "Quadro RTX
  4000 8GB" title and verify it lists `gpu` as a routing target.

---

### Phase 3: Normalisation (canonicalisation, family inference, VRAM repairs)

Add the GPU-specific repairs that run before validation. This phase
exists separately from Phase 1 because it depends on the prompt being
in place to know what shapes the LLM tends to return wrongly. Repairs
defend the validator from common LLM mistakes.

#### Tasks

- [ ] Create `pkg/extract/gpu_normalize.go` with:
  - `CanonicalizeGPUFamily(s string) string` — lowercase, whitespace
    trim/collapse, mapped to canonical token per DESIGN-0012 §7
    (`tesla`, `quadro`, `geforce`, `rtx`, `a-series`, `l-series`,
    `h-series`, `radeon-pro`, `instinct`, `arc`). Unknown values fall
    through as lowercased + spaces-to-hyphens.
  - `DetectGPUFamilyFromModel(model string) string` — regex-based
    inference: `^P\d{2,3}$|^V100$|^K\d{2}$` → `tesla`, `^A\d{1,3}$`
    → `a-series`, etc. Returns empty string if no match.
  - `NormalizeGPUExtraction(attrs map[string]any)` — top-level entry
    point that performs:
    1. **VRAM unit confusion**: if `vram_gb` is in 1024–262144,
       divide by 1024. If in 1000–256000, divide by 1000.
    2. **Family canonicalisation**: if `family` is non-empty, replace
       with `CanonicalizeGPUFamily(family)`.
    3. **Family inference**: if `family` is empty after canonicalisation
       and `model` is non-empty, fill from
       `DetectGPUFamilyFromModel(model)`.
    4. **VRAM rounding**: round `vram_gb` to nearest valid SKU (8, 12,
       16, 24, 32, 40, 48, 80, 96, 128) when within ±1 GB of one.
- [ ] Hook `NormalizeGPUExtraction` into
  `pkg/extract/normalize.go::NormalizeExtraction` under
  `case domain.ComponentGPU:`. Run before the existing common
  normalisation (capacity unit confusion is RAM-specific; GPU has its
  own).
- [ ] Write `pkg/extract/gpu_normalize_test.go` (table-driven):
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
    7 → 8, 15 → 16, exact (24, 80, etc.) → unchanged.
- [ ] Update `pkg/extract/normalize_test.go` to confirm GPU
  normalisation runs in the right order (before validation).
- [ ] Run `make fmt`, `make lint`, `go test ./pkg/extract/...`.
- [ ] Commit `feat(extract): add gpu normalisation — vram, family
  canonicalisation, model inference`.

#### Success Criteria

- `go test ./pkg/extract/...` passes.
- `gpu_normalize.go` coverage ≥95% (deterministic logic, fully
  exercisable).
- A round-trip test confirms a representative "messy" extraction
  (VRAM=24576, family=" Tesla ", model="P40") normalises cleanly to
  validate-passing input.

---

### Phase 4: Sample watch + integration smoke

Seed one example watch in dev/example configs and add an integration
test that exercises the full flow (classify → extract → product key
→ baseline placeholder) end-to-end with a stub LLM.

#### Tasks

- [ ] Add a sample watch block to `configs/config.example.yaml`:
  ```yaml
  - name: "NVIDIA Tesla P40"
    search_query: "NVIDIA Tesla P40 24GB"
    component_type: gpu
    score_threshold: 65
    enabled: false   # operator opts in
  ```
- [ ] Add the same watch to `configs/config.dev.yaml` with
  `enabled: true` so local dev runs ingest GPU listings.
- [ ] Write `pkg/extract/extractor_test.go::TestLLMExtractor_GPUFlow`
  — table-driven cases that:
  - Stub `Classify` to return `"gpu"`.
  - Stub `Extract` to return representative GPU JSON for 3 cards
    (Tesla P40 24GB, A100 40GB, MI210 64GB).
  - Assert `ClassifyAndExtract` returns `domain.ComponentGPU` and
    a non-empty attrs map with the expected `vram_gb`, `family`,
    `model`.
  - Assert `ProductKey("gpu", attrs)` produces the expected key.
- [ ] Run `make fmt`, `make lint`, `go test ./...`.
- [ ] Commit `feat(extract): seed example gpu watch and integration
  smoke test`.

#### Success Criteria

- All existing tests still pass; new GPU integration test green.
- `make lint` clean.
- Local `make dev-setup && make run` (manual) ingests at least one GPU
  listing under the example watch and produces a non-empty
  `gpu:<...>` product_key in the DB.

---

### Phase 5: PR + dev deploy + smoke validation

Open the PR, get CI green, deploy the dev image, watch the first few
ingestion cycles surface GPU listings into the new bucket. Validate the
classification rate looks right (i.e., real GPU titles land in `gpu`,
not `other`) before approving prod.

#### Tasks

- [ ] Push the branch, open PR with `feature` label.
- [ ] CI must be green (lint, test, build, security, docker-build,
  helm-lint, helm-unittest, helm-ct).
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
- [ ] Spot-check the `other` bucket for *missed* GPUs:
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
- [ ] Document the production transition in
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

- [ ] Add a "Backfill misclassified GPUs (IMPL-0017 Phase 7)" section
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
- [ ] Reference `feedback_dry_run_bulk_sql.md` discipline — always
  `BEGIN;` first, eyeball `RETURNING`, then commit.
- [ ] After commit, listings will have stale `attributes` (extracted
  under the wrong type). Two options:
  - (a) Re-queue them for extraction:
    `INSERT INTO extraction_queue (listing_id) SELECT id FROM listings WHERE component_type = 'gpu' AND attributes = '{}'::jsonb ON CONFLICT DO NOTHING;`
  - (b) Leave attributes empty until next ingestion refresh.
  - Operator chooses based on volume.
- [ ] Run `POST /api/v1/baselines/refresh` and `POST /api/v1/rescore`
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

These are **implementation-level** questions (the design's open
questions are all resolved). Operator review requested before starting
Phase 1:

1. **Should family canonicalisation live in `gpu_normalize.go` (new
   file) or extend `normalize.go`?** Existing pattern: RAM normalisation
   has its own file (`pc4.go`), server has `server_tier.go`. Proposal:
   new file `gpu_normalize.go` for consistency. Confirm.

2. **Where does the GPU primary regex go in `preclassify.go`?**
   Option (a): append a single line to `primaryComponentPatterns`
   (simpler, one list to scan when reading code). Option (b): create
   a dedicated `gpuPrimaryPatterns` slice and merge at use site
   (clearer intent, more boilerplate). Proposal: (a) — keeps the
   existing single-list architecture.

3. **Required `vram_gb` failure mode**: design says `vram_gb` is
   required. If the LLM can't determine it (title="Tesla P40" with
   no GB marker), validation will fail and the listing stays
   unextracted. Acceptable, or should we relax `vram_gb` to optional
   and let product-key default to `0gb` for those rows? Proposal:
   keep required. The bucket should hold proper data; cold-start
   listings with unknown VRAM aren't useful for baselines anyway.

4. **VRAM rounding boundary**: `±1 GB` covers typical mismatches
   (23→24, 81→80). But what about a 14GB VRAM card (exists in some
   workstation cards)? It would round to 16, breaking the product
   key. Proposal: keep ±1; add an exact-match list of known
   real-world SKUs (8, 12, 16, 24, 32, 40, 48, 80, 96, 128) and only
   round when within ±1 of one. 14 isn't in the list, so it stays
   14. Same for any other oddball value.

5. **Family inference regex precision**: the proposed
   `^P\d{2,3}$` for Tesla matches `P40`, `P100`, `P4000`. But
   `P4000` is a *Quadro* P4000, not a Tesla. Need a more precise
   regex like `^P(40|100|6|6000)$|^V100$`. Or, since the LLM is
   supposed to fill `family` reliably, treat inference as a
   last-ditch fallback that's deliberately conservative — only
   match the most-confident prefixes (`P40`, `P100`, `V100`). The
   risk of a wrong inference (`P4000` → `tesla` for a Quadro card)
   is worse than no inference (`unknown` segment in product key).
   Proposal: tighten the inference regex to high-confidence
   patterns only.

6. **Sample watch threshold timing**: design says bump 65 → 80
   "after ~1 week or when sample_count ≥ 10". Should the threshold
   bump be automated (a job that runs daily and updates watches
   when their bucket matures), or stay manual? Proposal: manual for
   now. Automation is easy to add later if it becomes a recurring
   chore.

7. **Phase 7 trigger**: should backfill be automatic (run as part of
   the migration on first deploy) or operator-gated (run only if
   Phase 6 baselines aren't maturing fast enough)? Proposal: gated.
   `feedback_dry_run_bulk_sql.md` showed that even careful regex
   backfills can have surprising false-positive rates; better to
   leave it to operator discretion.

8. **Discord embed for `gpu`**: existing notifier renders embeds with
   `{Name: "Type", Value: alert.ComponentType}` — does "gpu" render
   cleanly in Discord, or should we display "GPU" (uppercased)? This
   is cosmetic. Proposal: leave as-is; if it looks wrong post-deploy,
   small follow-up to capitalise the type label.

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
