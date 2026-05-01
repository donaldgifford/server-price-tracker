---
id: DESIGN-0011
title: "Reduce alert noise via scoring recalibration and accessory pre-classifier"
status: Draft
author: Donald Gifford
created: 2026-04-30
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0011: Reduce alert noise via scoring recalibration and accessory pre-classifier

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-04-30

<!--toc:start-->
- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
  - [Symptom 1: noise floor at 89](#symptom-1-noise-floor-at-89)
  - [Symptom 2: server parts classified as servers](#symptom-2-server-parts-classified-as-servers)
- [Detailed Design](#detailed-design)
  - [Part A: accessory pre-classifier](#part-a-accessory-pre-classifier)
  - [Part B: priceScore recalibration](#part-b-pricescore-recalibration)
  - [Why we don't version the scoring](#why-we-dont-version-the-scoring)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [Follow-ups](#follow-ups)
- [References](#references)
<!--toc:end-->

## Overview

After IMPL-0015 deployed and alerts started firing, the alerts UI surfaced two
quality problems: (a) all RAM listings score ~89 — uniformly above any
reasonable alert threshold — so every fresh listing fires; and (b) a backplane
listing classified as `server` because the LLM weighted "POWEREDGE R740xd"
above the "BACKPLANE" hint in the title. This doc proposes a deterministic
accessory pre-classifier and a recalibration of the price scoring curve so
"average" listings stop earning 88+ composite scores.

A second-order effect of (b) makes (a) worse: misclassified accessories
contaminate the price baselines they get assigned to. A $50 backplane bucketed
under `server` pulls the server P10/P25 down, so real servers then score
artificially well against the polluted distribution. Fixing the classifier is
also defending baseline integrity.

## Goals and Non-Goals

### Goals

- Stop bare server-part listings (backplanes, caddies, rails, bezels) from
  reaching the LLM classification pass as a primary `server`/`drive`/`ram`/etc.
- Spread the composite score distribution so that the median listing earns
  ~50–60 and only genuinely good deals exceed 80.
- Apply the new scoring to all existing listings via the existing
  `POST /api/v1/rescore` endpoint — no rescore tooling work, no migrations.
- Ship both fixes in one PR + one rescore run; users see the dashboard quiet
  down within a single deploy cycle.

### Non-Goals

- **Not** changing watch threshold defaults. Existing watches keep their
  configured thresholds; the curve change does the heavy lifting.
- **Not** versioning `score_breakdown` JSON. The schema is unchanged; values
  shift on rescore.
- **Not** retraining or swapping LLM backends. The classifier prompt stays
  untouched; we just gate it behind a regex pre-pass.
- **Not** addressing other extraction quality issues (capacity unit confusion,
  speed_mhz nulls). Those have their own normalization layer
  (`pkg/extract/normalize.go`) and aren't blocking alerts.

## Background

### Symptom 1: noise floor at 89

The composite score formula in `pkg/scorer/scorer.go` weights six factors:

| Factor    | Weight | Typical eBay listing |
|-----------|-------:|----------------------|
| Price     |  40%   | 100 (any price ≤ baseline P10) |
| Seller    |  20%   | 100 (top-rated seller, 5000+ feedback, ≥99.5%) |
| Condition |  15%   | 70  (`used_working`) |
| Quantity  |  10%   | 60  (1–4 unit lots) |
| Quality   |  10%   | 100 (has images + item specifics + 50+ char description) |
| Time      |   5%   | 30  (BIN, not new, not auction-ending) |

Composite for the "typical" listing:

```text
100·0.40 + 100·0.20 + 70·0.15 + 60·0.10 + 100·0.10 + 30·0.05
= 40 + 20 + 10.5 + 6 + 10 + 1.5
= 88
```

That matches the "everything is 89" report exactly. The dominant problem is
`priceScore`:

```go
case unitPrice <= b.P10:
    return 100
case unitPrice <= b.P25:
    return lerp(unitPrice, b.P10, b.P25, 100, 85)
case unitPrice <= b.P50:
    return lerp(unitPrice, b.P25, b.P50, 85, 50)
```

A listing priced at the **median** scores 50, but anything below P25 scores
85+ — and used server RAM sellers undercut aggressively, so the bulk of
active listings cluster at or below P25. Effectively most listings hit
`priceScore = 100`.

### Symptom 2: server parts classified as servers

Title `"DELL EMC POWEREDGE R740xd 24 BAY SFF SERVER BACKPLANE K2Y8N7 58D2W
P1MJ3"` was classified as `server`. The classifier prompt
(`pkg/extract/prompts.go:21`) does mention backplanes:

> Accessories and parts that are not themselves the component go to `other`.
> Examples: drive caddies/trays, rack rails, bezels, brackets, mounting kits,
> cables, fans, heatsinks, risers, **backplanes (when sold alone)**, power
> supplies (when sold alone).

But the LLM apparently weights the strong product-line signal ("POWEREDGE
R740xd") above the accessory hint. We've seen the same failure mode for drive
caddies (CLAUDE.md memory: "Classifier accessory routing: classifyTmpl
explicitly routes drive caddies, rails, bezels, brackets..."). Prompt tweaks
help but aren't reliable, and each false-positive accessory becomes either an
extraction-validation failure (stuck NULL `component_type`) or — worse — a
mis-classified row that fires alerts.

## Detailed Design

### Part A: accessory pre-classifier

Add a deterministic regex-based pre-pass that runs **before** `LLMExtractor.classify`.
If the title matches, the listing is short-circuited to `component_type =
other` with `extraction_confidence = 1.0` and skips the LLM entirely.

**Where:** new file `pkg/extract/preclassify.go`. Called from
`(*LLMExtractor).Extract` in `pkg/extract/extractor.go` before the classify
template runs.

**Patterns:** word-boundary matches on the lowercased title. Anchored to whole
words so "racks" doesn't match "rack" — keep the regex specific.

```go
var accessoryPatterns = []*regexp.Regexp{
    regexp.MustCompile(`\bbackplane\b`),
    regexp.MustCompile(`\b(drive\s+)?(caddy|caddies|tray|trays|sled|sleds)\b`),
    regexp.MustCompile(`\brails?\b`),
    regexp.MustCompile(`\bbezels?\b`),
    regexp.MustCompile(`\b(mounting\s+)?brackets?\b`),
    regexp.MustCompile(`\brisers?\b`),
    regexp.MustCompile(`\bheat[\s-]?sinks?\b`),
    regexp.MustCompile(`\bfan\s+(assembly|kit|tray|module)\b`),
    regexp.MustCompile(`\bcable\b`),  // narrow: only when "cable" is the noun
    regexp.MustCompile(`\bgpu\s+riser\b`),
}
```

**Match guard:** if a title hits an accessory pattern AND a primary-component
pattern (e.g. has "RAM" / "DDR4" / "NVME" alongside "tray"), defer to the LLM
— this avoids killing legit listings that mention accessories incidentally.

```go
func IsAccessoryOnly(title string) bool {
    lower := strings.ToLower(title)
    if !matchesAny(lower, accessoryPatterns) {
        return false
    }
    if matchesAny(lower, primaryComponentPatterns) {
        return false  // mixed → let LLM decide
    }
    return true
}
```

**Metric:** `spt_extraction_preclass_short_circuit_total{pattern}` counter, so
we can see which patterns fire most and validate the regex isn't catching
legit listings. Sample dashboard panel: top accessory short-circuit reasons.

### Part B: priceScore recalibration

Reshape the percentile curve so the median listing earns ~30, not 50. Current
vs proposed:

| Percentile | Current score | Proposed score |
|-----------:|--------------:|---------------:|
| ≤ P10      | 100           | 100            |
| P25        | 85            | 70             |
| P50        | 50            | 30             |
| P75        | 25            | 10             |
| P90        | 0             | 0              |

This collapses the noise floor: the "typical" listing
(median price, top-rated seller, used_working, has images) now scores:

```text
30·0.40 + 100·0.20 + 70·0.15 + 60·0.10 + 100·0.10 + 30·0.05
= 12 + 20 + 10.5 + 6 + 10 + 1.5
= 60
```

A genuinely good deal (priced ≤ P10, top-rated, used_working, has images,
new listing) still hits:

```text
100·0.40 + 100·0.20 + 70·0.15 + 60·0.10 + 100·0.10 + 80·0.05
= 40 + 20 + 10.5 + 6 + 10 + 4
= 90.5
```

So the gap between "median listing" (60) and "actual deal" (90+) is a clean
30 points — easy threshold-tuning. With watch threshold defaults at 80,
median listings stop firing, deals still fire.

The function change is mechanical:

```go
func priceScore(unitPrice float64, b *Baseline) float64 {
    switch {
    case unitPrice <= b.P10:
        return 100
    case unitPrice <= b.P25:
        return lerp(unitPrice, b.P10, b.P25, 100, 70) // was 85
    case unitPrice <= b.P50:
        return lerp(unitPrice, b.P25, b.P50, 70, 30)  // was 85→50
    case unitPrice <= b.P75:
        return lerp(unitPrice, b.P50, b.P75, 30, 10)  // was 50→25
    case unitPrice <= b.P90:
        return lerp(unitPrice, b.P75, b.P90, 10, 0)   // was 25→0
    default:
        return 0
    }
}
```

### Why we don't version the scoring

`score_breakdown` is a JSONB column on `listings` that gets overwritten
in-place by `(*PostgresStore).SaveListing` and by `/api/v1/rescore`. There's no
historical record of past scores anywhere. So:

- We can't accidentally render stale scores in the UI — the most recent
  rescore is the only score that exists.
- A "version" field would be lying: the rescore would overwrite it anyway.
- Alerts have their own `score` column captured at evaluation time
  (`internal/store/queries.go` insert path). Those keep their original values
  even after rescore — no drift in alert history.

## API / Interface Changes

None. Existing endpoints unchanged:

- `POST /api/v1/rescore` — already does what we need.
- `POST /api/v1/extract` — `Extractor.Extract` signature unchanged; just
  short-circuits earlier for accessories.
- `GET /api/v1/listings` — score values shift but column types are stable.

## Data Model

No migrations. `score_breakdown` JSONB schema unchanged. `component_type`
enum unchanged (`other` already exists). Alert `score` column unchanged.

## Testing Strategy

**Unit tests (TDD, table-driven):**

- `pkg/extract/preclassify_test.go` — table cases:
  - Pure accessory titles (backplane, caddy, rails, bezel) → `true`
  - Mixed titles ("4U server with rack rails included") → `false`
  - Primary-component-only titles ("Dell R740xd Server") → `false`
  - Edge cases: GPU riser, mounting bracket, fan assembly
- `pkg/scorer/scorer_test.go` — extend existing tests with the new curve
  values. Update assertions for P25, P50, P75 boundary scores.
- `pkg/extract/extractor_test.go` — verify `Extract` returns the
  short-circuit path without calling the LLM backend (mock should record zero
  classify calls when `IsAccessoryOnly` is true).

**Integration validation post-deploy:**

- Run a `psql` query before and after rescore to compare distributions:
  ```sql
  SELECT component_type,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY score) AS p50,
         percentile_cont(0.9) WITHIN GROUP (ORDER BY score) AS p90,
         COUNT(*) AS n
  FROM listings
  WHERE active = true
  GROUP BY component_type;
  ```
  Expect P50 to drop from ~85 to ~60.
- Spot-check the alerts UI: the noise should drop substantially.

## Migration / Rollout Plan

1. Land PR with both changes + tests.
2. Merge, CI tags release, image publishes to GHCR.
3. Deploy dev → confirm new scoring distribution via `psql`.
4. Hit `POST /api/v1/rescore` once.
5. Watch `/alerts` for ~24h to confirm noise dropped without losing real
   deals.
6. Promote to prod (Helm chart appVersion bump is automatic).

**Rollback:** If the new curve over-corrects (real deals stop firing), revert
the PR, redeploy, and re-run rescore. ~5 minutes end-to-end.

**Stale alerts:** Existing alerts in the DB keep their original `score`
column values — they're snapshots, not derived. The alerts UI sorts by score
descending, so old high-noise alerts will float to the top until the user
dismisses them. Optional cleanup query (operator-run, not automated):

```sql
UPDATE alerts SET dismissed_at = now()
WHERE dismissed_at IS NULL
  AND created_at < now() - interval '7 days';
```

## Open Questions

All resolved 2026-04-30:

1. ~~Should the regex live in config or in code?~~ **Resolved: code-first.**
   Once the patterns are dialed in we can revisit moving to config.
2. ~~Should we emit a metric for suppressed false positives?~~ **Resolved:
   skip.** We aren't tracking cables / backplanes as a component type, so
   short-circuited rows just land in `other` and are effectively dropped from
   alerting. No measurement signal needed yet.
3. ~~Drop the seller score weight?~~ **Resolved: defer to follow-up.** Good
   idea but ship one change at a time. Tracked under "Follow-ups" below.
4. ~~`\bcable\b` regex risk?~~ **Resolved: ship as-is.** Servers that
   incidentally mention "cables" or "backplanes" in their title/description
   are exactly the kind of mis-classification we're trying to clean up. If
   the simple regex isn't enough, fallback is image classification (see
   follow-ups).

## Follow-ups

Tracked here so we don't lose them; not in scope for this design.

- **Seller weight rebalance.** Currently 20% — high enough that a top-rated
  seller selling a mediocre deal gets a free 20 points, while a no-name
  seller offering a real deal gets penalized. Candidate: move 5–10 points
  from seller → price.
- **Image-based classification fallback.** If the title-only regex doesn't
  cleanly separate accessories from full-component listings, the next step
  is multimodal classification — pass the eBay primary image to a vision
  model and ask "is this a full server, or a server part?". This would
  catch accessories whose titles include the host product name as a SEO
  spam pattern (`"FOR DELL R740xd ..."`) without any accessory keyword.
  Higher cost per listing, but bounded by ingestion volume (~30–50/day).

## References

- IMPL-0015 — alert review UI rollout that surfaced these issues
- DESIGN-0001 — original scoring algorithm definition
- `pkg/scorer/scorer.go` — current scoring implementation
- `pkg/extract/prompts.go` — current classifier prompt (accessory routing)
- `pkg/extract/normalize.go` — sibling pattern for deterministic LLM-output
  repair
- CLAUDE.md memory: "Classifier accessory routing" entry documenting prior
  prompt-only attempts at this problem
