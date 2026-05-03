---
id: DESIGN-0015
title: "Add workstation and desktop as component types"
status: Accepted
author: Donald Gifford
created: 2026-05-02
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0015: Add workstation and desktop as component types

**Status:** Accepted
**Author:** Donald Gifford
**Date:** 2026-05-02

<!--toc:start-->
- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [Type definitions](#type-definitions)
  - [Classification signals](#classification-signals)
  - [Product key shape](#product-key-shape)
  - [Cross-bucket matching](#cross-bucket-matching)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Add `workstation` and `desktop` as peer `ComponentType` values alongside
the existing `server`/`gpu`/`cpu`/`ram`/`drive`/`nic`/`other` set.
Surfaced during DESIGN-0012 / IMPL-0017 GPU rollout: workstations
(Dell Precision, Lenovo ThinkStation, HP Z-series) were polluting both
`server` baselines (when classified as server) and `gpu` baselines
(when the LLM picked "RTX 3090" out of the title and missed the
chassis context).

## Goals and Non-Goals

### Goals

- Carve workstations out of `server` and `gpu` buckets so each gets a
  clean baseline.
- Track generic desktop towers (OptiPlex, ThinkCentre, consumer)
  separately from workstations — different price band, different buyer.
- Reuse the existing `ComponentType` machinery; mirror the GPU
  rollout shape (DESIGN-0012 / IMPL-0017).
- Cleanly separable from rack servers using `Form Factor` and eBay
  category breadcrumb signals.

### Non-Goals

- **Cross-bucket alert matching.** A workstation containing a Xeon
  Gold + RTX 3090 will only fire alerts on `workstation` watches, not
  on `cpu` or `gpu` watches. See Q1.
- **Workstation tier suffix.** No `barebone|partial|configured`
  segmentation in v1 — kept flat. Could be added later if pricing
  variance within a model line is noisy enough to warrant it.
- **Detecting prosumer / gaming PCs vs business desktops.** Anything
  tower-form without a workstation product line goes in `desktop`.
  Refinement is a follow-up.
- **Mobile workstations** (laptops). Out of scope; eBay's listings
  for mobile workstations are a different product category and watch
  surface.

## Background

GPU rollout (DESIGN-0012 / IMPL-0017) ran into two specific cases:

1. **Lenovo P620** — `Threadripper Pro 3995WX, 256GB RAM, 4TB M.2,
   RTX 3090, Win11`. Classified as `server` (correctly under current
   rules) but distorted server baselines.
2. **Dell Precision T7920** — dual `Xeon Gold 6262V, 512GB RAM,
   4TB M.2`. Classified as `server` because Xeon Gold is a strong
   server signal, even though the chassis is a Tower workstation.

Workstation pricing follows a different curve than rack servers
(Threadripper Pro release cycles vs. Xeon Scalable cycles, single-user
buyer vs. cluster operator, tower vs. rack form factor). Mixing them
in one bucket guarantees mis-scoring.

eBay's `Type` item-specific is inconsistent across workstation
listings: sometimes `Desktop` (Lenovo P620), sometimes `Precision`
(Dell T7920), occasionally `Workstation`. Multiple signals must
combine to classify reliably.

## Detailed Design

### Type definitions

**`workstation`** — single-user computing workstation with a vendor-
defined workstation product line. Signals (any one is sufficient):

- `Series` field contains `ThinkStation`, `Z by HP`, or `Dell Precision`
- `Most Suitable For: Workstation` (eBay's explicit flag, when present)
- Title regex matches workstation chassis names:
  `precision\s+t?\d+|hp\s+z[0-9]|thinkstation|workstation`
- Known workstation SKU in model: T7920, P620, Z8 G4, Z6 G4, etc.

**`desktop`** — tower-form general-purpose computer without a
workstation product line. Signals:

- `Form Factor: Tower` AND eBay category in
  `Computers/Tablets & Networking`
- Brand+line not in the workstation set above
- Examples: Dell OptiPlex, Lenovo ThinkCentre, HP Pavilion / EliteDesk,
  custom builds, generic refurbished towers

**Boundary against `server`:**

- `Form Factor: Rack Mountable` → `server` (regardless of GPU/RAM
  contents)
- eBay category `Enterprise Networking, Servers` → `server`
- Product lines `PowerEdge`, `ProLiant`, `PowerVault`, `UCS` →
  `server`

### Classification signals

The classifier prompt is augmented with workstation/desktop rules and
the relevant item-specific fields are passed in. Signal reliability
ranking (used for prompt engineering and prioritisation):

1. `Most Suitable For: Workstation` — eBay's explicit flag
2. `Series` field containing a workstation product line
3. `Product Line` / `Type` field
4. Known SKU in `Model` field
5. Title regex (last-resort fallback)

**Do NOT anchor on CPU.** The Dell Precision T7920 ships with dual
Xeon Gold (server-class Scalable chips); CPU alone misclassifies it
as `server`. Use chassis/series signals first; CPU is a tiebreaker
at best.

The pre-classifier (`pkg/extract/preclassify.go`) gets workstation
chassis primary patterns added to `primaryComponentPatterns` so
"Precision T7920 + 80mm fan" defers to the LLM rather than short-
circuiting to `other`.

### Product key shape

Mirrors the existing `server` and `gpu` shapes:

- `workstation:<vendor>:<line>:<model>` — e.g.,
  `workstation:dell:precision:t7920`,
  `workstation:lenovo:thinkstation:p620`,
  `workstation:hp:z8-g4`
- `desktop:<vendor>:<line>:<model>` — e.g.,
  `desktop:dell:optiplex:7080`,
  `desktop:lenovo:thinkcentre:m920q`

No tier suffix in v1. If pricing variance within a model line proves
noisy (e.g., barebone T7920 vs. fully-configured T7920) we can add a
`barebone|partial|configured` suffix as a follow-up — same shape as
the existing server tier work in IMPL-0016.

### Cross-bucket matching

**Resolved (Q1):** single classification per listing. A workstation
listing classifies as `workstation`, scored against workstation
baselines, only matches `component_type = 'workstation'` watches.
GPU/CPU/RAM watches do **not** fire on workstation listings even
when the title names a watched part.

Rationale:

- Matches existing architecture (every other ComponentType works
  this way).
- Workstation prices include chassis + PSU + RAM + SSD; bare-
  component baselines don't compare cleanly to workstation prices.
- Same listing firing 3 watch alerts (workstation + cpu + gpu) is
  noise the cooldown machinery doesn't suppress (per-watch cooldown,
  not per-listing).
- Buyer intent: a `gpu` watch is for buying a GPU, not parting out
  a workstation.

If the "this workstation contains a watched part" use case becomes
real, an opt-in filter (`include_contained: true`) is the cleanest
follow-up surface — see Open Questions.

## API / Interface Changes

- `ComponentType` enum gains `workstation` and `desktop` values
  (`pkg/types/types.go`).
- `validComponentTypes` map in `pkg/extract/extractor.go` extended.
- `spt watches create --type workstation` / `--type desktop` newly
  valid.
- `GET /api/v1/listings?component_type=workstation` and `=desktop`
  newly valid query values.
- No changes to existing endpoints, watch filter shape, or alert
  payload shape.

## Data Model

DB migration adds both values to existing CHECK constraints:

```sql
-- Migration 011 (or next available)
ALTER TABLE watches DROP CONSTRAINT watches_component_type_check;
ALTER TABLE watches ADD CONSTRAINT watches_component_type_check
  CHECK (component_type IN
    ('ram', 'drive', 'server', 'cpu', 'nic', 'gpu',
     'workstation', 'desktop', 'other'));

ALTER TABLE listings DROP CONSTRAINT listings_component_type_check;
ALTER TABLE listings ADD CONSTRAINT listings_component_type_check
  CHECK (component_type IN
    ('ram', 'drive', 'server', 'cpu', 'nic', 'gpu',
     'workstation', 'desktop', 'other'));
```

Same shape as migration 010 (GPU). Postgres can't add to CHECK in
place; drop + recreate.

## Testing Strategy

Mirrors IMPL-0017's testing structure:

- **Unit:** validator tests, prompt-rendering tests, pre-classifier
  primary-regex tests, normalisation tests for any per-type repairs
  needed.
- **Integration smoke:** at least one workstation listing and one
  desktop listing classify cleanly with non-empty product keys.
- **Dev validation (operator):** create one watch per type, spot-
  check 5 listings each, ensure no leakage into `server`/`gpu`/
  `other` buckets via the mirror of IMPL-0017's smoke queries.

## Migration / Rollout Plan

7-phase plan in IMPL-0018 (to be drafted), shape lifted from IMPL-0017:

1. Domain wiring — enum, product key, validator
2. LLM surface — classifier prompt + extraction prompts + pre-
   classifier primaries
3. Normaliser — per-type cleanups if needed
4. Migration 011 — DB CHECK constraint update
5. Dev validation — deploy, watch, smoke
6. Production rollout — merge, deploy, monitor, threshold-bump
7. Optional backfill — re-classify historical workstation/desktop
   listings currently bucketed as `server` or `other`

## Open Questions

1. **Cross-bucket alert matching.** Resolved as **A — single
   classification, no cross-bucket matching**. (Memory:
   `workstation_component_type_followup.md`.) An opt-in filter for
   the "watch contained components" use case is a future RFC if
   needed in practice.

2. **Tier suffix on workstation product key (v1 vs. follow-up).**
   Resolved as **defer to v2**. Ship v1 flat
   (`workstation:dell:precision:t7920`). T7920 barebone vs. fully-
   configured can vary 3-4x so this may need to be reopened quickly
   if baseline noise is high; tier suffix mirrors the existing
   `pkg/extract/server_tier.go` pattern when it does.

3. **GPU-bundled handling for `desktop`.** Resolved as **defer
   until we see real `desktop` data**. Many desktops are sold with
   a discrete GPU pre-installed (especially refurbished gaming
   PCs that overlap with the desktop bucket); the same baseline-
   pollution concern as workstations applies. Once we have a few
   weeks of `desktop` listings, decide between tier suffix, bundle
   filter, or accepting the noise.

4. **Workstation watch examples.** Resolved. Separate watches per
   chassis line (matches the per-line-watches pattern that worked
   for GPU rollout). Initial set captures both legacy and post-
   rebrand Dell branding:
   - `Dell Precision T-series` (T7820, T7920, T5820, T3640) —
     legacy branding, still dominant on eBay's secondary market
   - `Dell Pro Max` — current workstation branding (replaces
     Precision at top tier in Dell's 2024-25 rebrand)
   - `Lenovo ThinkStation P-series` (P620, P920)
   - `HP Z-series` (Z8 G4, Z6 G4)
   Each at threshold 65 (cold-start), bumped to 80 once their
   `workstation:%` baselines mature. Same playbook as IMPL-0017
   Phase 6.

5. **Desktop watch examples.** Resolved. Initial set covers
   Dell's pre- and post-rebrand business desktop lines plus the
   common Lenovo / HP equivalents:
   - `Dell OptiPlex` (7080, 7090, 7095) — legacy business
     desktop
   - `Dell Pro` — current business desktop (post-rebrand)
   - `Lenovo ThinkCentre` (M920q, M920s) — small-form-factor
     business
   - `HP EliteDesk` (800 G4 / G5) — business class
   Threshold + maturity playbook same as workstation. Skip auto-
   alerting initially if volume is low; bucket exists for tracking
   first.

6. **PR shape.** Resolved as **one combined PR**. Both types share
   infrastructure (one migration, one prompt update, one set of
   tests). If workstation prompt iteration ends up gating desktop
   work in practice, we'll split mid-flight; default is combined.

## References

- `docs/design/0012-add-gpu-as-a-new-component-type.md` — DESIGN-0012,
  the most recent ComponentType addition
- `docs/impl/0017-design-0012-gpu-component-type-phase-plan.md` —
  IMPL-0017, the implementation template this design will follow
- `migrations/010_add_gpu_component_type.sql` — CHECK-constraint
  migration template
- `pkg/extract/server_tier.go` — server tier suffix precedent
  (referenced in tier-suffix open question)
- Memory: `workstation_component_type_followup.md` — design context
  captured during IMPL-0017 dev validation, including signal-
  reliability ranking and the "do NOT anchor on CPU" finding
- Memory: `gpu_component_type.md` — eight-touchpoint checklist for
  adding a new ComponentType
