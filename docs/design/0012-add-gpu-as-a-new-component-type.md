---
id: DESIGN-0012
title: "Add GPU as a new component type"
status: Draft
author: Donald Gifford
created: 2026-05-01
---

<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0012: Add GPU as a new component type

**Status:** Draft **Author:** Donald Gifford **Date:** 2026-05-01

<!--toc:start-->

- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Add `gpu` as a first-class `ComponentType` so server-grade and workstation GPUs
(Tesla, A-series, Quadro, RTX server cards, AMD MI/Radeon Pro) get their own
classification, extraction prompt, attribute set, baseline, and alert pathway —
instead of being miscategorised as `other` or buried in `server` listings that
bundle a GPU.

## Goals and Non-Goals

### Goals

- New `domain.ComponentGPU = "gpu"` enum value.
- Dedicated extraction prompt that pulls GPU-specific attributes (manufacturer,
  model, VRAM, interface, TDP, etc.).
- Product-key format that groups by `manufacturer:family:model:vram` so
  baselines distinguish a Tesla P40 from an A100, and a 24GB variant from a 16GB
  variant.
- Validation rules for required and optional fields with sane ranges.
- Classifier prompt updated to route GPU listings to `gpu`, not `other`.
- Pre-classifier primary patterns extended so bare-accessory regex doesn't catch
  real GPUs (existing `gpu riser` accessory pattern stays).
- One new sample watch (e.g., "Tesla P40") seeded in dev/example configs for
  end-to-end validation.

### Non-Goals

- **No new database schema.** Attributes ride on the existing
  `listings.attributes JSONB` column.
- **No new scoring weights.** The shared `Score()` works because the composite
  uses generic factors (price, seller, condition, quality, quantity, time) that
  are component-agnostic.
- **No retroactive backfill** of historical `other` listings into `gpu`. Out of
  scope; future migration if desired.
- **No GPU-specific watch filter UI** beyond existing `WatchFilters` attribute
  matching (which already works against any JSONB field).

## Background

The repo currently supports five component types: ram, drive, server, cpu, nic —
plus the catch-all `other` (DESIGN-0011 routes accessories there). Each type
has:

- An entry in the `ComponentType` enum (`pkg/types/types.go`)
- An extraction prompt template (`pkg/extract/prompts.go`)
- A validation function (`pkg/extract/validate.go`)
- A product-key format (`pkg/extract/productkey.go`)
- Sometimes normalisation logic (`pkg/extract/normalize.go`)
- A case in the classifier prompt's enumeration

Listings carry a `component_type` column plus a free-form `attributes JSONB`
blob. Baselines key off `product_key`. Scoring is shared across all types.
Watches reference a `component_type`, a search query, optional attribute
filters, and a score threshold.

GPUs today get classified as `other` (because the classifier prompt lists gpu
nowhere) or occasionally as `server` (when the title leads with a chassis name
and the GPU is mentioned later). Both outcomes pollute the respective baselines
and prevent watch-driven alerting on GPU deals.

The pre-classifier (DESIGN-0011) has one GPU-related rule today: `gpu riser` is
a bare accessory pattern that routes to `other`. Real GPU cards do not have a
bare-accessory regex match — they will defer to the LLM classifier, which
currently routes them to `other` for lack of a better option.

## Detailed Design

### 1. Enum addition

Add `ComponentGPU` to `pkg/types/types.go`:

```go
const (
    ComponentRAM    ComponentType = "ram"
    ComponentDrive  ComponentType = "drive"
    ComponentServer ComponentType = "server"
    ComponentCPU    ComponentType = "cpu"
    ComponentNIC    ComponentType = "nic"
    ComponentGPU    ComponentType = "gpu" // new
    ComponentOther  ComponentType = "other"
)
```

### 2. Extraction prompt

New `gpuTmpl` in `pkg/extract/prompts.go`. Schema:

```json
{
  "manufacturer": "NVIDIA" | "AMD" | "Intel",
  "family": "Tesla" | "Quadro" | "RTX" | "GeForce" | "A-series" | "L-series" | "H-series" | "Radeon Pro" | "Instinct" | "Arc" | null,
  "model": string,
  "vram_gb": integer (1-256),
  "memory_type": "GDDR5" | "GDDR6" | "GDDR6X" | "HBM2" | "HBM2e" | "HBM3" | null,
  "interface": "PCIe 3.0 x16" | "PCIe 4.0 x16" | "PCIe 5.0 x16" | "SXM2" | "SXM4" | "SXM5" | null,
  "tdp_watts": integer (15-700) | null,
  "form_factor": "single_slot" | "dual_slot" | "triple_slot" | "FHFL" | "HHHL" | "LP" | null,
  "cooling": "passive" | "active" | "blower" | null,
  "power_connectors": string | null,
  "quantity": integer (default 1),
  "condition": "new" | "like_new" | "used_working" | "for_parts" | "unknown",
  "confidence": float (0.0-1.0)
}
```

Required: `manufacturer`, `model`, `vram_gb`, `condition`, `confidence`. The
rest are optional with `null` allowed.

### 3. Validation

`validateGPU(attrs)` in `pkg/extract/validate.go`:

- `manufacturer`: required enum (`NVIDIA`, `AMD`, `Intel`).
- `model`: required non-empty string. Free-form because models proliferate fast
  (P40, P100, V100, A30, A40, A100, L40, H100, MI100, MI210, …).
- `vram_gb`: required, 1–256. Bounded high to allow future cards.
- `family`, `memory_type`, `interface`, `form_factor`, `cooling`: optional
  enums.
- `tdp_watts`: optional, 15–700 (low bound covers entry workstation cards; high
  bound covers H100 SXM at 700W).

### 4. Product key

`gpu:<manufacturer>:<family>:<model>:<vram>gb`

Example keys:

- `gpu:nvidia:tesla:p40:24gb`
- `gpu:nvidia:a-series:a100:80gb`
- `gpu:amd:instinct:mi210:64gb`

VRAM is included because the same model often ships in multiple memory
configurations (A100 40GB vs 80GB, P100 12GB vs 16GB) with very different
prices. Family is included to keep `Quadro RTX 4000` distinct from
`GeForce RTX 4000`-class cards which the LLM might both label "RTX 4000".

If `family` is null, the key uses `unknown` for that segment — same fallback
pattern as other types.

### 5. Classifier prompt

`classifyTmpl` enumerates types. Add `gpu` and clarify routing:

```text
Types: ram, drive, server, cpu, nic, gpu, other

Rules:
- ...
- Pick "gpu" for actual graphics cards / accelerators (Tesla, Quadro,
  RTX, A/L/H-series, Radeon Pro, Instinct, Arc).
- "gpu riser" / "GPU bracket" / "GPU power cable" stay in "other".
```

The pre-classifier already routes `gpu riser` → `other`. Real GPU cards don't
match any bare-accessory pattern, so they defer to the LLM classifier — which
now has explicit GPU guidance.

### 6. Pre-classifier primaries

Add GPU brand/family tokens to `primaryComponentPatterns` so titles like "Dell
PowerEdge R740 with 2x Tesla P40" don't get caught by an accessory keyword
(e.g., "tray", "rail") that may also appear in the listing:

```go
regexp.MustCompile(`\b(tesla|quadro|rtx\s+a\d+|a100|h100|l40|mi\d{3}|radeon\s+pro)\b`),
```

This matches real GPU markers that the existing primary set doesn't cover.
Listings with both a GPU primary and an accessory keyword (e.g., "Tesla P40 +
heatsink") defer to the LLM, which now classifies them as `gpu`.

### 7. Normalisation

`NormalizeGPUExtraction` runs in `pkg/extract/normalize.go` before
validation, mirroring the RAM/CPU repairs:

- **VRAM unit confusion** — if `vram_gb` lands in 1024–262144, divide by 1024
  (treat as MB). If it lands in 1000–256000, divide by 1000. Same pattern as
  `capacity_gb` repair for RAM.
- **Family inference from model** — when `family` is null but `model` matches
  a known prefix, fill it: `P\d{2,3}|V100|K\d{2}` → `Tesla`,
  `^A\d{1,3}$` → `A-series`, `^L\d{1,3}$` → `L-series`,
  `^H\d{1,3}$` → `H-series`, `^MI\d{2,3}$` → `Instinct`.
- **Power-of-2 VRAM rounding** — round to nearest valid sku (8, 12, 16, 24,
  32, 40, 48, 80, 96, 128) when within ±1 GB. Defends against `vram_gb=23`
  for a 24GB card.

### 8. Sample watch (cold-start threshold)

Seed one example watch in dev/example config for smoke-testing. Use a low
threshold during the cold-start window because `priceScore` falls back to
neutral 50 until the bucket has 10+ samples — composite stays ~55:

```yaml
- name: "NVIDIA Tesla P40"
  search_query: "NVIDIA Tesla P40 24GB"
  component_type: gpu
  score_threshold: 65   # bump to 80 once baseline has ≥10 samples
  enabled: true
```

Watch instructions for operator: revisit threshold after ~1 week or when
`/api/v1/listings?component_type=gpu&limit=1` confirms enough rows have
landed for baselines to compute.

## API / Interface Changes

- `domain.ComponentGPU` added to the public component-type enum.
- Huma OpenAPI spec auto-includes the new value (component_type field on
  Listing/Watch is a string with enum tag — Huma generates from runtime).
- No new endpoints. Existing CRUD on `/api/v1/watches` and queries on
  `/api/v1/listings?component_type=gpu` work via the new enum value.
- `spt watches create --component-type gpu …` works without CLI changes (the CLI
  passes the string through).

## Data Model

- **No migration.** `listings.component_type` is a free-form string in Postgres
  (no enum constraint at the DB level). `attributes JSONB` carries the
  GPU-specific fields. The GIN index on `attributes` handles any
  attribute-filter watches.
- **Baseline rows** auto-populate from listings as soon as enough GPU ingestions
  land (`MinBaselineSamples=10`).

## Testing Strategy

- **Unit tests, table-driven, in each touched package**:
  - `pkg/extract/validate_test.go` — happy path + each required-field omission +
    each enum-out-of-range case.
  - `pkg/extract/productkey_test.go` — family/model/vram permutations, `unknown`
    fallbacks.
  - `pkg/extract/prompts_test.go` — template renders without error, contains
    "gpu" / "Tesla" / "VRAM" tokens.
  - `pkg/extract/preclassify_test.go` — new GPU primaries trip primary-defer for
    a "Tesla P40 + heatsink" title; the existing `gpu riser` rule still fires
    for an accessory-only title.
- **Integration**: existing extractor integration test gets a fixtures/gpu/
  directory with 3–5 representative titles + expected attributes.
- **End-to-end**: seeded dev watch ingests, a real or mock listing scores, and
  Discord receives a payload with `Type: gpu`.

## Migration / Rollout Plan

1. Land code change behind no flag — new enum value is purely additive.
2. Deploy. Existing classify path still routes most things correctly; only
   formerly-`other`-classified GPUs start getting `gpu` going forward.
3. Seed one or two GPU watches against a known model.
4. Watch the new baseline grow over a week. Once `MinBaselineSamples=10` is
   reached for at least one product_key, scoring becomes non-neutral and alerts
   can fire.
5. **Optional follow-up**: backfill existing listings whose title trips the new
   GPU primaries. SQL pattern that mirrors the regex, gated by
   `BEGIN; … RETURNING …; ROLLBACK;` per `feedback_dry_run_bulk_sql.md`.
   Document in `docs/SQL_HELPERS.md`.

## Open Questions

1. **Family enumeration** — _pending sample inspection._ Operator is
   pulling a sample of listings via the search strings in
   `Implementation Notes` to check whether sellers consistently
   disambiguate Quadro RTX 4000 vs RTX A4000 vs RTX 4000 Ada in titles.
   Three candidate resolutions:
   - **(a) Free-form string + normaliser** (preferred) — sturdiest to
     NVIDIA renaming; product-key construction collapses common spellings.
   - **(b) Hard enum** — safer at validation time but breaks when a new
     family ships.
   - **(c) Skip family entirely** — product key becomes
     `gpu:<manufacturer>:<model>:<vram>gb`. Simplest. Viable if model+VRAM
     is unique enough in practice (sample inspection will tell us).

2. **Bundled GPUs in server listings** — _resolved: leave as `server`._
   "Dell R740 with 2x Tesla P40" stays in the server bucket. Future
   enhancement (out of scope here): a quality factor that boosts a
   server's score when GPUs are detected, so a server with included GPUs
   beats an otherwise-equivalent server without. Filed as a follow-up,
   not part of this design.

3. **Watch threshold default** — _resolved: seed at 65 and bump later._
   See Section 8. Operator revisits after ~1 week.

4. **Normalisation** — _resolved: ship with normalisation._ See Section 7.
   VRAM unit repair, family inference from model, and power-of-2 rounding
   land in the first PR.

5. **Prompt size budget** — adding gpu to the classifier prompt grows the
   routing rules slightly. Keep an eye on classify-token spend after
   deploy via `spt_extraction_tokens_total{backend}`. Not blocking.

## Implementation Notes

### Sample search strings for design validation (open question 1)

Run via `POST /api/v1/search` or eBay UI; eyeball the returned titles to
decide between family-as-free-form vs hard-enum vs skip-family:

- `nvidia tesla p40 24gb` — Tesla family, GDDR5
- `nvidia tesla v100 32gb` — HBM2, both SXM and PCIe variants exist
- `nvidia a100 40gb` — A-series (no longer "Tesla" branded)
- `nvidia h100 80gb` — H-series
- `nvidia l40 48gb` — L-series
- `quadro rtx 4000` vs `nvidia rtx a4000` vs `nvidia rtx 4000 ada` —
  the family-disambiguation stress test
- `amd radeon pro w6800`, `amd instinct mi210`, `intel arc pro a40` —
  multi-vendor coverage
- `gpu riser cable`, `nvidia tesla cooling kit` — should still hit
  `other` via existing pre-classifier
- `dell poweredge r730 tesla p40` — bundled; should stay `server`

## References

- DESIGN-0001 — original architecture, ComponentType enum
- DESIGN-0002 — extraction pipeline and prompt structure
- DESIGN-0011 — accessory pre-classifier (preserves `gpu riser` rule)
- `docs/EXTRACTION.md` — product-key generation reference
- `pkg/extract/{prompts,validate,productkey,preclassify}.go` — files the
  implementation will touch
