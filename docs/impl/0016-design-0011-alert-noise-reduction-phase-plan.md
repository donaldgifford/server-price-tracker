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
- [ ] Commit with `feat(scorer): recalibrate priceScore curve to spread
  the composite distribution`.

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

- [ ] Add `AlertsFiredTotal` to `internal/metrics/metrics.go`:
  - `*prometheus.CounterVec`, name `spt_alerts_fired_total`,
    labels `component_type`.
  - Help text: "Number of alerts fired by the engine, labeled by
    component type."
  - Register via `promauto.NewCounterVec` (matches existing pattern).
  - Note (per CLAUDE.md memory): the metric will not render HELP/TYPE at
    `/metrics` until first observed — ensure the test in
    `internal/metrics/metrics_test.go` seeds a zero-valued series if it
    asserts on rendered output.
- [ ] Increment the counter in `internal/engine/alert.go`:
  - In whichever function inserts into the `alerts` table (check
    `engine.ProcessAlerts` and its descendants), call
    `metrics.AlertsFiredTotal.WithLabelValues(string(componentType)).Inc()`
    for each newly-inserted alert.
  - The increment fires on alert *creation*, not notification — tracking
    the engine's decision-to-alert independent of Discord delivery
    success.
- [ ] Add a unit test in `internal/engine/alert_test.go` covering:
  - When `ProcessAlerts` inserts N alerts for component type X, the
    counter for `{component_type="X"}` increases by N.
  - Use `testutil.ToFloat64` against `metrics.AlertsFiredTotal.With(...)`
    with a unique label per test name (per CLAUDE.md memory pattern for
    parallel-safe metric tests).
- [ ] Add a Grafana panel via the dashgen workflow:
  - Add panel func `AlertsFired` in `tools/dashgen/panels/alerts.go`.
  - Wire it into the alerts row in `tools/dashgen/dashboards/overview.go`.
  - Register `spt_alerts_fired_total` in `KnownMetrics` in
    `tools/dashgen/config.go`.
  - Bump `totalPanels` in `tools/dashgen/dashgen_test.go`.
  - Run `make dashboards` to regenerate
    `deploy/grafana/data/spt-overview.json` and Prometheus rules YAML.
  - Per CLAUDE.md memory: skipping the regeneration step turns CI red.
- [ ] Add a "Backfill misclassified accessories" section to
  `docs/SQL_HELPERS.md` containing the canonical UPDATE query:
  ```sql
  -- Re-classify accessory-titled listings to 'other'.
  -- Mirrors pkg/extract/preclassify.go:accessoryPatterns. Postgres uses
  -- \y for word boundaries (POSIX), not \b.
  UPDATE listings
  SET component_type = 'other',
      extraction_confidence = 0.95,
      updated_at = now()
  WHERE active = true
    AND (
      title ~* '\ybackplane\y'
      OR title ~* '\y(drive\s+)?(caddy|caddies|tray|trays|sled|sleds)\y'
      OR title ~* '\yrails?\y'
      OR title ~* '\ybezels?\y'
      OR title ~* '\y(mounting\s+)?brackets?\y'
      OR title ~* '\yrisers?\y'
      OR title ~* '\yheat[\s-]?sinks?\y'
      OR title ~* '\yfan\s+(assembly|kit|tray|module)\y'
      OR title ~* '\ycable\y'
      OR title ~* '\ygpu\s+riser\y'
    )
    AND title !~* '\y(ddr[345]?|nvme|sas|sata|xeon|epyc|intel|amd)\y'
  RETURNING id, component_type, title;
  ```
  Document that it should be run **after** the new image is deployed
  (so the rescore picks up the corrected component types) and that the
  RETURNING clause lets the operator audit the affected rows.
- [ ] Run `make lint`, `make fmt`, `make test-coverage`,
  `go test ./tools/dashgen/...` → ensure passing.
- [ ] Commit with `feat(metrics): add alerts-fired counter; doc(sql):
  add accessory backfill helper`.

#### Success Criteria

- `spt_alerts_fired_total` is registered, increments correctly in tests,
  and renders at `/metrics` with HELP/TYPE.
- `tools/dashgen` tests pass; the regenerated dashboard JSON has the new
  panel.
- `docs/SQL_HELPERS.md` contains the backfill query with the
  word-boundary syntax (`\y`) note.
- `make lint`, `make test-coverage`, and `go test ./tools/dashgen/...`
  all green.

---

### Phase 4: PR + dev deploy + rescore + backfill validation

Open the PR, ride CI green, deploy to dev, run the rescore and backfill,
verify the distribution shift matches DESIGN-0011's prediction before any
prod move.

#### Tasks

- [ ] Push the branch and open PR with `patch` label. Title:
  `fix(scoring): reduce alert noise via accessory pre-classifier and
  priceScore recalibration`.
- [ ] PR description references DESIGN-0011 + IMPL-0016, includes the
  migration plan bullets and the pre/post `psql` distribution query so a
  reviewer can run the validation themselves.
- [ ] Confirm all 11 CI checks green; merge to main.
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
