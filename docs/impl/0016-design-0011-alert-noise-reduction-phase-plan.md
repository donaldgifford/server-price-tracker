---
id: IMPL-0016
title: "DESIGN-0011 alert noise reduction phase plan"
status: Draft
author: Donald Gifford
created: 2026-04-30
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0016: DESIGN-0011 alert noise reduction phase plan

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-04-30

<!--toc:start-->
- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Accessory pre-classifier](#phase-1-accessory-pre-classifier)
  - [Phase 2: priceScore curve recalibration](#phase-2-pricescore-curve-recalibration)
  - [Phase 3: Alerts-fired metric + backfill SQL helper](#phase-3-alerts-fired-metric--backfill-sql-helper)
  - [Phase 4: PR + dev deploy + rescore + backfill validation](#phase-4-pr--dev-deploy--rescore--backfill-validation)
  - [Phase 5: Production rollout + 24h watch](#phase-5-production-rollout--24h-watch)
  - [Phase 6 (post-work): Server config tier in product key](#phase-6-post-work-server-config-tier-in-product-key)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Open Questions](#open-questions)
- [Dependencies](#dependencies)
- [References](#references)
<!--toc:end-->

## Objective

Implement the two changes specified in DESIGN-0011 — a deterministic accessory
pre-classifier that short-circuits the LLM for obvious server-part listings,
and a `priceScore` curve recalibration that drops the composite-score noise
floor from ~88 to ~60 — plus the supporting infrastructure to validate the
fix worked: an `spt_alerts_fired_total` Prometheus counter and a SQL backfill
helper that flips already-misclassified accessory listings to the correct
component type. Validate in dev with the rescore + backfill flow before
promoting to prod.

**Implements:** DESIGN-0011

## Scope

### In Scope

- New `pkg/extract/preclassify.go` with `IsAccessoryOnly(title string) bool`
  and the accessory + primary-component regex tables.
- Hook into `(*LLMExtractor).ClassifyAndExtract` so the LLM is bypassed for
  pure-accessory titles, returning `(domain.ComponentOther, {confidence: 0.95},
  nil)`.
- Recalibrate the `priceScore` percentile curve in `pkg/scorer/scorer.go` to
  the new boundary scores (P25=70, P50=30, P75=10).
- Update existing scorer tests for the new boundary values; add new tests
  asserting the "typical median" listing now scores ~60 and a "real deal"
  scores ~90+.
- New `spt_alerts_fired_total{component_type}` counter in
  `internal/metrics/metrics.go`, incremented in `internal/engine/alert.go`
  when alerts are inserted.
- New SQL backfill helper documented in `docs/SQL_HELPERS.md` that flips
  active listings whose titles match the accessory regex to
  `component_type = 'other'`. Run manually in dev (Phase 4) and prod
  (Phase 5).
- Single PR opened, CI green, deploy to dev, run rescore + backfill,
  validate distribution, promote to prod.

### Out of Scope

- Watch threshold defaults — left untouched per DESIGN.
- `score_breakdown` JSON versioning — left untouched per DESIGN.
- Seller weight rebalance — tracked as DESIGN-0011 follow-up.
- Image-based classification fallback — tracked as DESIGN-0011 follow-up.

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its tasks
are checked off and its success criteria are met.

---

### Phase 1: Accessory pre-classifier

Lock in the deterministic title-regex short-circuit before any scoring change
lands, so that the new price curve operates on a cleaner classification
distribution from the moment of deploy.

#### Tasks

- [x] Create `pkg/extract/preclassify.go` with:
  - `accessoryPatterns []*regexp.Regexp` — backplane, caddy/tray/sled,
    rails, bezels, brackets, risers, heatsinks, fan assembly, cable, gpu
    riser. Patterns per DESIGN-0011 Part A.
  - `primaryComponentPatterns []*regexp.Regexp` — DDR2-5, RDIMM family,
    NVMe/SAS/SATA/SCSI, SSD/HDD, Xeon/EPYC/Opteron/Threadripper,
    capacity markers (`\d+gb|\d+tb`), and form factors (`\d+u`). Form
    factor was added during implementation so "4U server with rails"
    defers to the LLM while the production R740xd backplane example
    (no form factor in title) still routes to `other`.
  - `IsAccessoryOnly(title string) bool` — lowercases title, returns true
    iff `accessoryPatterns` match AND `primaryComponentPatterns` does not.
  - Unexported helper `matchesAny(s string, ps []*regexp.Regexp) bool`.
- [x] Hook the short-circuit into `(*LLMExtractor).ClassifyAndExtract`.
  When `IsAccessoryOnly(title)` is true, return
  `(domain.ComponentOther, map[string]any{"confidence": 0.95}, nil)`
  immediately — do not call `Classify` or `Extract`. Confidence is 0.95
  via `accessoryShortCircuitConfidence` const with explanatory doc
  comment. Log at `Info` level with key `accessory_short_circuit=true`.
- [x] Write `pkg/extract/preclassify_test.go` (table-driven, `t.Parallel`)
  covering pure accessories, primary-component titles, mixed titles,
  casing, empty / whitespace, and unrelated titles. 30 cases, 100%
  coverage on `preclassify.go`.
- [x] Extend `pkg/extract/extractor_test.go::TestLLMExtractor_ClassifyAndExtract`
  with a "accessory short-circuit skips llm" sub-test that supplies no
  `Generate` mock expectations — mockery fails the test if the LLM is
  called. Asserts `attrs["confidence"] == 0.95`.
- [x] Run `make lint` → 0 issues.
- [x] Run `make fmt` and `make test-coverage` → green; preclassify.go
  coverage 100%, package coverage 91.6%.
- [x] Commit with `feat(extract): add accessory title pre-classifier`
  (commit `4c0797c`).

#### Success Criteria

- [x] `go build ./...` succeeds.
- [x] `go test ./pkg/extract/...` passes; `preclassify.go` reaches 100%
  coverage (target ≥90%).
- [x] The new `TestClassifyAndExtract` sub-test confirms zero LLM calls
  on the short-circuit path.
- [x] `make lint` clean.
- [x] All existing extractor tests still pass — the short-circuit is
  additive.

---

### Phase 2: priceScore curve recalibration

Reshape the percentile curve in isolation, with tests proving both the old
"typical listing" composite drops to ~60 and a "real deal" composite stays
≥90. No interface changes; just numeric retuning of `priceScore`.

#### Tasks

- [x] Modify `pkg/scorer/scorer.go::priceScore` lerp boundaries from
  `100, 85` / `85, 50` / `50, 25` / `25, 0` to `100, 70` / `70, 30` /
  `30, 10` / `10, 0` per DESIGN-0011 Part B. Add explanatory doc
  comment.
- [x] Update boundary-value assertions in `pkg/scorer/scorer_test.go::
  TestScore_WithBaseline` and `TestScore_CompositeCalculation` to match
  the new curve (P25 → 70, P50 → 30, P75 → 10).
- [x] Add `TestScore_TypicalListing_Composite` — median listing scores
  in `[55, 65]`. Uses helper `recalibrationBaseline()` for the shared
  fixture.
- [x] Add `TestScore_GoodDeal_Composite` — below-P10 listing with full
  quality + new listing scores ≥ 90. Bumped `DescriptionLen` to 600 so
  the quality factor lands in the top bucket (`> 500`).
- [x] Add `TestScore_BadListing_Composite` — at-P75 / for_parts /
  no-images listing scores ≤ 30 (guards against floor collapse).
- [x] Run `make lint` (0 issues), `make fmt`, `go test ./pkg/scorer/`
  (97.2% coverage, scorer.go priceScore at 100%).
- [x] Commit with `feat(scorer): recalibrate priceScore curve to spread
  the composite distribution` (commit `90a5789`).

#### Success Criteria

- [x] `go test ./pkg/scorer/...` passes.
- [x] `pkg/scorer/scorer.go` coverage stays ≥90% (actual 97.2%).
- [x] The three new composite-score tests demonstrate the spread
  DESIGN-0011 predicts (60 / 90+ / ≤30).
- [x] No changes outside `pkg/scorer/` — this phase is purely numeric.

---

### Phase 3: Alerts-fired metric + backfill SQL helper

Bolt on the supporting infrastructure for Phase 4 / Phase 5 validation: a
counter so we can quantify "alerts dropped" and a SQL helper to backfill
already-misclassified historical listings.

#### Tasks

- [x] Add `AlertsCreatedTotal` to `internal/metrics/metrics.go` — a new
  CounterVec named `spt_alerts_created_total` labeled by
  `component_type`. Distinct from the existing `AlertsFiredTotal`
  (which fires on Discord delivery success); this one fires on alert
  insert so it reflects engine decisions independent of notifier
  outcomes. Help text documents the distinction.
- [x] Increment the counter in
  `internal/engine/engine.go::evaluateAlert` after a successful
  `store.CreateAlert`. Added `return` on the error path so we don't
  count failed inserts.
- [x] Two new tests in `internal/engine/engine_test.go`:
  - `TestEvaluateAlert_IncrementsAlertsCreated` — counter goes up by 1
    for `domain.ComponentRAM`.
  - `TestEvaluateAlert_DoesNotIncrementOnCreateAlertError` — counter
    does not increment on `CreateAlert` returning an error.
- [x] Add panel `AlertsCreatedByComponent` to
  `tools/dashgen/panels/alerts.go`, wire into the Alerts row in
  `tools/dashgen/dashboards/overview.go`, register
  `spt_alerts_created_total` in `KnownMetrics`
  (`tools/dashgen/config.go`), bump `totalPanels` from 33 → 34 in
  `tools/dashgen/dashgen_test.go`, regenerate dashboards via
  `make dashboards`. `TestStaleness` passes.
- [x] Add a "Backfill misclassified accessories" section to
  `docs/SQL_HELPERS.md`. The query mirrors `preclassify.go` regex
  tables (using Postgres `\y` word boundaries instead of Go's `\b`),
  enumerates the primary-component negative-match clauses explicitly
  (rather than smushing them into one regex), and uses `RETURNING` so
  affected rows are auditable.
- [x] `make lint` (0 issues), `make fmt`, `go test ./internal/engine/`,
  `go test ./internal/metrics/`, `go test ./tools/dashgen/...` all
  pass.
- [x] Commit with `feat(metrics): add alerts-created counter and
  backfill SQL helper` (commit `a6aaf02`).

#### Success Criteria

- [x] `spt_alerts_created_total` is registered and increments correctly
  in tests.
- [x] `tools/dashgen` tests pass; regenerated dashboard JSON has the new
  panel.
- [x] `docs/SQL_HELPERS.md` contains the backfill query with the
  word-boundary syntax (`\y`) note.
- [x] `make lint`, scorer/extract/engine/metrics tests, and
  `go test ./tools/dashgen/...` all green.

---

### Phase 4: PR + dev deploy + rescore + backfill validation

Open the PR, ride CI green, deploy to dev, run the rescore and backfill,
verify the distribution shift matches DESIGN-0011's prediction before any
prod move.

#### Tasks

- [x] Push the branch and open PR #46 with `patch` label. Title:
  `fix(scoring): reduce alert noise via accessory pre-classifier and
  priceScore recalibration`.
- [x] PR description references DESIGN-0011 + IMPL-0016 and includes the
  pre/post `psql` distribution query.
- [x] Confirm all 11 CI checks green on PR #46.
- [ ] Merge to main (operator action — destructive against shared `main`).
- [ ] Wait for the auto-tagged release + chart appVersion bump CI to
  publish the dev image to GHCR.
- [ ] Deploy the new image to the dev cluster.
- [ ] Capture pre-rescore distribution (record output for the PR comment):
  ```sql
  SELECT component_type,
         percentile_cont(0.10) WITHIN GROUP (ORDER BY score) AS p10,
         percentile_cont(0.50) WITHIN GROUP (ORDER BY score) AS p50,
         percentile_cont(0.90) WITHIN GROUP (ORDER BY score) AS p90,
         COUNT(*) AS n
  FROM listings
  WHERE active = true
  GROUP BY component_type
  ORDER BY component_type;
  ```
- [ ] Run the backfill UPDATE from `docs/SQL_HELPERS.md` against the dev
  DB. Capture the count of affected rows for the PR comment.
- [ ] `curl -X POST $SPT_URL/api/v1/rescore` (or `spt rescore` if the CLI
  exposes it).
- [ ] Capture post-rescore distribution with the same query.
- [ ] Compare: P50 should drop by ~25 points across active component types
  (server / ram / drive). If it doesn't, **stop** — file a follow-up to
  re-tune the curve (per OQ #4 resolution).
- [ ] Confirm the new `spt_alerts_fired_total` counter is incrementing on
  subsequent ingestion ticks (via Grafana or `curl /metrics`).
- [ ] Hit `/alerts` and confirm:
  - Existing alerts still visible (their `score` column is preserved).
  - New alerts created post-rescore are notably fewer.
  - At least some genuine-deal alerts still fire (next ingestion tick).

#### Success Criteria

- PR merged with `patch` label and all CI green.
- Dev image deployed and reachable.
- Backfill UPDATE returned ≥1 row (else the regex isn't catching anything,
  which suggests either the dev DB has no accessory-misclassified rows or
  the regex is wrong).
- Post-rescore P50 dropped by ≥20 points for active component types with
  `n > 50` listings.
- `spt_alerts_fired_total` incrementing in dev metrics.
- `/alerts` is materially less noisy on subsequent ingestion ticks.
- No errors in dev pod logs related to the new pre-classifier or rescore.

---

### Phase 5: Production rollout + 24h watch

Promote to prod, run the same rescore + backfill flow, watch for 24h,
decide whether the change holds or needs follow-up retuning.

#### Tasks

- [ ] Promote the validated dev image to prod (sync prod ArgoCD app /
  bump Helm release).
- [ ] Run the same pre-rescore distribution capture in prod.
- [ ] Run the backfill UPDATE in prod.
- [ ] Trigger `/api/v1/rescore` once in prod.
- [ ] Capture post-rescore distribution.
- [ ] Watch `/alerts` over the next 24h. Track:
  - `spt_alerts_fired_total` rate (Grafana) — should be materially
    lower than pre-deploy.
  - Whether real deals are still surfacing (subjective: open the alert,
    eyeball the listing).
- [ ] If alert volume is still too high, file a follow-up doc proposing
  the seller-weight rebalance (DESIGN-0011 follow-up).
- [ ] If alert volume is too low (real deals stopped firing), revert the
  PR, redeploy, re-rescore. ~5 minutes per DESIGN-0011 rollback plan.
- [ ] Update `MEMORY.md` with the resolved noise-floor numbers and any
  surprises — particularly which accessory regex patterns fired most.
- [ ] Mark DESIGN-0011 status `Implemented`; mark IMPL-0016 status
  `Completed`.

#### Success Criteria

- Prod is running the new code.
- 24h post-rescore: `spt_alerts_fired_total` rate has dropped by ≥50%
  compared to the pre-deploy baseline.
- At least one alert in the 24h window flagged a listing the user
  considers a genuine deal.
- DESIGN-0011 + IMPL-0016 frontmatter updated to reflect the outcome.

---

### Phase 6 (post-work): Server config tier in product key

Surfaced during dev validation: even with the pre-classifier and curve
recalibration in place, *barebone* server listings (no CPU, no RAM, no
HDDs — sold as chassis-plus-PSU shells) consistently scored 80+ and fired
alerts. A single $200 R740xd shell got bucketed against fully-configured
R740xd listings priced $1500+, landed near the bucket's P10, and earned
a near-perfect price score.

This isn't a misclassification — those listings *are* servers. It's a
**baseline-segmentation** problem: the server product key collapses every
configuration into one bucket, so barebone shells systematically look
like "great deals" against a baseline dominated by populated systems.

#### Why before component-level encoding

Two paths considered:

1. **Coarse tier flag** *(this phase)* — derive `barebone` / `partial` /
   `configured` from title regex, append to product key. ~50 lines of
   code, no new attributes to extract, baselines warm up fast (we have
   ~339 server listings; even split three ways each tier still has
   100+).
2. **Component-level encoding** — parse actual CPU model, RAM total, HDD
   count from titles; either embed in the product key (fragments
   baselines into combinations that never warm up) or do composite-price
   math (subtract typical CPU/RAM/HDD prices to get chassis-only price,
   score residual against chassis-only baseline). Much more involved and
   needs its own DESIGN doc.

The coarse tier is the 80/20 fix. We start there, observe whether
alert noise drops further, and only escalate to component-level
encoding if barebone-vs-configured segmentation alone isn't enough.

#### Tasks

- [ ] Add tier detection in a new `pkg/extract/server_tier.go`:
  - `barebonePatterns` — `\bbar(e)?bone\b`,
    `\b(no|w/o|without)\s+(cpu|ram|memory|hdd?s?|drives?)\b`,
    `\bcto\b` (configure-to-order). Multiple matches strengthen
    confidence but a single match suffices.
  - `cpuPresentPatterns` — `\b(xeon|epyc|opteron|threadripper)\b`,
    `\b(gold|silver|platinum|bronze)\s+\d{4}\b` (Xeon model SKUs).
  - `ramPresentPatterns` — `\b\d+gb\b` (paired with RAM context — at
    least 8GB to filter out drive-capacity false positives), or
    `\bddr[2345]\b`.
  - `DetectServerTier(title string) string` — returns `barebone`,
    `partial`, or `configured`:
    - `barebone` — any barebone pattern matches
    - `configured` — both CPU AND RAM present
    - `partial` — exactly one of CPU/RAM present
    - default to `unknown` when nothing matches (treated as `partial`
      downstream — neither cleanly barebone nor cleanly configured)
- [ ] Wire into `pkg/extract/normalize.go::NormalizeExtraction`:
  ```go
  if componentType == domain.ComponentServer {
      attrs["tier"] = DetectServerTier(title)
  }
  ```
  Run before validation so the tier is available when building the
  product key.
- [ ] Update `pkg/extract/productkey.go` server case to append tier:
  ```go
  case "server":
      return fmt.Sprintf("server:%s:%s:%s:%s",
          normalizeStr(attrs["manufacturer"]),
          normalizeStr(attrs["model"]),
          normalizeStr(attrs["drive_form_factor"]),
          normalizeStr(attrs["tier"]),
      )
  ```
- [ ] Add `tier` to the server extraction prompt schema in
  `pkg/extract/prompts.go` and `pkg/extract/validate.go` (optional
  enum: `barebone | partial | configured`). Even though we derive it
  from title regex, declaring it in the schema lets the LLM emit a
  hint that NormalizeExtraction can accept or override.
- [ ] Tests:
  - `pkg/extract/server_tier_test.go` — table-driven cases covering
    explicit "Barebone Server", "No CPU/RAM/HDDs", "CTO Server",
    fully-configured listings ("R740xd 2x Xeon Gold 5118 64GB 2x SSD"),
    partial ("R640 with Xeon Silver 4110 No RAM"), and ambiguous
    titles ("Dell R740xd Server").
  - Extend `pkg/extract/productkey_test.go` server cases to assert the
    tier suffix is included.
  - Ensure `NormalizeExtraction` server-tier wiring is exercised in
    `pkg/extract/normalize_test.go`.
- [ ] Migration after deploy:
  - Re-extract or recompute `product_key` for all active server
    listings — easiest path is a one-shot SQL UPDATE that runs the
    same product-key formula in Postgres. Add to
    `docs/SQL_HELPERS.md`. Alternative: hit `/api/v1/reextract` per
    CLAUDE.md memory (handles `component_type IS NOT NULL`).
  - Trigger `POST /api/v1/baselines/refresh` so per-tier baselines
    populate from the freshly bucketed listings.
  - Trigger `POST /api/v1/rescore`.
  - Capture pre/post distribution split by tier:
    ```sql
    SELECT
      CASE
        WHEN product_key LIKE '%:barebone' THEN 'barebone'
        WHEN product_key LIKE '%:configured' THEN 'configured'
        WHEN product_key LIKE '%:partial' THEN 'partial'
        ELSE 'unknown'
      END AS tier,
      percentile_cont(0.50) WITHIN GROUP (ORDER BY score) AS p50,
      percentile_cont(0.90) WITHIN GROUP (ORDER BY score) AS p90,
      COUNT(*) AS n
    FROM listings
    WHERE active = true AND component_type = 'server'
    GROUP BY tier ORDER BY tier;
    ```
- [ ] Run `make lint`, `make fmt`, `make test-coverage`. Coverage
  target ≥90% on new file.
- [ ] Commit with `feat(extract): add server config tier to product
  key for baseline segmentation`.

#### Success Criteria

- `DetectServerTier` table-driven tests cover the three tiers plus
  ambiguous fallback, with ≥90% coverage on `server_tier.go`.
- Post-deploy + post-baseline-refresh, the per-tier P50 query shows
  separation: barebone P50 should land 20+ points lower than
  configured P50 (barebones are systematically cheaper, so their own
  baseline reflects that, and barebones stop scoring 100 against the
  configured baseline).
- The original failure case ("R740xd Barebone Server with 2x Heatsink
  2x 1100W PSU") at $400 stops firing as a deal alert because its
  `product_key` ends `:barebone` and its $400 price is now near the
  median of the barebone bucket, not the P10 of the configured bucket.

#### Risks / Tradeoffs

- **Cold start per-tier baselines**: splitting the existing 339 server
  listings three ways gives ~110 per tier — above the
  `MinBaselineSamples=10` threshold but smaller buckets are noisier.
  If baselines stay cold (`<10 samples`) for any tier+chassis combo,
  `priceScore` falls back to neutral 50, which is fine.
- **Tier detection false positives** (regex says `barebone` for a
  configured listing) inflate the barebone bucket's P10/P25 → real
  barebones score lower → fewer barebone alerts than they deserve.
  Probably acceptable.
- **Tier detection false negatives** (regex misses a barebone) leave
  the listing in `partial` or `unknown` → status quo problem
  perpetuates for those rows. Better to err toward false negatives
  than false positives — keeps the barebone bucket's percentiles
  honest.
- **Ambiguous "unknown" tier** — if regex finds neither CPU nor RAM
  signal and no barebone marker, the title may be too short to tell.
  Default to `partial` so it doesn't pollute either extreme. Monitor
  the `:unknown` row count over time; if it's large, refine the
  regexes.

---

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `pkg/extract/preclassify.go` | Create | Accessory regex tables + `IsAccessoryOnly` |
| `pkg/extract/preclassify_test.go` | Create | Table-driven tests for the regex matcher |
| `pkg/extract/extractor.go` | Modify | Short-circuit in `ClassifyAndExtract` |
| `pkg/extract/extractor_test.go` | Modify | Sub-test asserting zero LLM calls on short-circuit |
| `pkg/scorer/scorer.go` | Modify | New boundary values in `priceScore` |
| `pkg/scorer/scorer_test.go` | Modify | Update existing assertions; add 3 new composite tests |
| `internal/metrics/metrics.go` | Modify | Register `spt_alerts_fired_total` CounterVec |
| `internal/metrics/metrics_test.go` | Modify | Seed + assert on rendered metric output |
| `internal/engine/alert.go` | Modify | Increment counter on alert insert |
| `internal/engine/alert_test.go` | Modify | Test counter increment on insert |
| `tools/dashgen/panels/alerts.go` | Modify | Add `AlertsFired` panel |
| `tools/dashgen/dashboards/overview.go` | Modify | Wire panel into alerts row |
| `tools/dashgen/config.go` | Modify | Add `spt_alerts_fired_total` to `KnownMetrics` |
| `tools/dashgen/dashgen_test.go` | Modify | Bump `totalPanels` |
| `deploy/grafana/data/spt-overview.json` | Modify | Regenerated via `make dashboards` |
| `docs/SQL_HELPERS.md` | Modify | New "Backfill misclassified accessories" section |
| `docs/impl/0016-...md` | Modify | Check off tasks as they complete |
| `docs/design/0011-...md` | Modify | Status: `Draft` → `Implemented` at end of Phase 5 |

No migrations. No CLI changes. No API surface changes. No Helm chart
changes.

## Testing Plan

- [ ] Unit tests for `IsAccessoryOnly` (target ≥90% line coverage on
  `pkg/extract/preclassify.go`).
- [ ] Unit tests for the new `priceScore` curve and the three composite
  scenarios (typical / good deal / bad listing).
- [ ] Existing `extractor_test.go::TestClassifyAndExtract` extended for
  the short-circuit path; verify mock backend records zero `Generate`
  calls.
- [ ] Unit test for `AlertsFiredTotal` increment behavior in
  `internal/engine/alert_test.go`.
- [ ] `tools/dashgen/dashgen_test.go::TestBuildOverviewDashboard` and
  `TestStaleness` pass after the new panel + regenerated JSON.
- [ ] CI must stay green on all 11 jobs (Lint, Test Go, Build, Security
  Scan, Docker Build, Helm Chart Test, Helm Unit Tests, Lint Repo, Label
  PR, Check Required Labels, Check Dependency Licenses).
- [ ] No new integration tests required — the rescore endpoint already
  has integration coverage; only the input function changed.
- [ ] Manual validation in dev (Phase 4) and prod (Phase 5) via the
  pre/post-rescore SQL distribution query and the new metric.

## Open Questions

All resolved 2026-04-30:

1. ~~How do we quantitatively measure "alert volume dropped"?~~
   **Resolved: bundle the metric into this PR.** New Phase 3 adds
   `spt_alerts_fired_total{component_type}` counter + Grafana panel.
   Useful long-term beyond this fix.
2. ~~Backfill: re-classify existing accessory-titled listings?~~
   **Resolved: yes, both layers.** The pre-classifier fixes future
   ingestions; the SQL UPDATE in `docs/SQL_HELPERS.md` fixes historical
   data. Run in dev (Phase 4) and prod (Phase 5).
3. ~~`extraction_confidence` value for the short-circuit path?~~
   **Resolved: 0.95.** A regex match isn't an LLM result; 1.0 would
   overstate certainty.
4. ~~What if Phase 4's distribution check shows no meaningful P50 drop?~~
   **Resolved: stop and revisit.** No need to pre-stage diagnostic
   queries — if the rescore doesn't move P50 by ≥20 points on
   `n > 50` types, halt the rollout and file a follow-up to re-tune.

## Dependencies

- DESIGN-0011 (parent design doc, already merged on this branch).
- IMPL-0015 deployed in prod (provides the `/alerts` UI we'll spot-check).
- `/api/v1/rescore` endpoint operational — already shipped in MVP.
- Dev cluster has enough active listings post-IMPL-0015 deploy to make
  the distribution check meaningful (≥50 per primary component type).

## References

- DESIGN-0011 — parent design doc with the math and rationale
- IMPL-0015 — alert review UI rollout that surfaced these symptoms
- DESIGN-0001 — original scoring algorithm definition
- `pkg/scorer/scorer.go` — current scoring implementation
- `pkg/extract/extractor.go::ClassifyAndExtract` — short-circuit hook
  point
- `pkg/extract/normalize.go` — sibling pattern for deterministic
  LLM-output repair
- `internal/engine/alert.go` — alert insert path (metric hook point)
- `internal/metrics/metrics.go` — Prometheus metric registration
- `tools/dashgen/` — Grafana dashboard generator (5-step add-panel
  workflow per CLAUDE.md memory)
- `docs/SQL_HELPERS.md` — backfill query home
- CLAUDE.md memory: "Classifier accessory routing", "score_breakdown" /
  rescore semantics, "Promauto vec metrics need at least one observed
  series before HELP/TYPE renders", "Dashgen workflow"
