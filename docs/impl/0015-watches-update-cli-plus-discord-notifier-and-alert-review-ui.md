---
id: IMPL-0015
title: "Watches update CLI plus Discord notifier and alert review UI"
status: Draft
author: Donald Gifford
created: 2026-04-26
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0015: Watches update CLI plus Discord notifier and alert review UI

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-04-26

<!--toc:start-->
- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: Discord notifier off-by-one fix (DESIGN-0009 Phase 1)](#phase-1-discord-notifier-off-by-one-fix-design-0009-phase-1)
  - [Phase 2: spt watches update CLI command (DESIGN-0008)](#phase-2-spt-watches-update-cli-command-design-0008)
  - [Phase 3: Alerts schema + dismiss store API (DESIGN-0010 prerequisites)](#phase-3-alerts-schema--dismiss-store-api-design-0010-prerequisites)
  - [Phase 4: Alert review UI handlers, list page, detail page (DESIGN-0010 core)](#phase-4-alert-review-ui-handlers-list-page-detail-page-design-0010-core)
  - [Phase 5: Discord chunked sends with rate-limit tracking (DESIGN-0009 Phase 2)](#phase-5-discord-chunked-sends-with-rate-limit-tracking-design-0009-phase-2)
  - [Phase 6: Discord summary mode (DESIGN-0010 + 0009 join)](#phase-6-discord-summary-mode-design-0010--0009-join)
  - [Phase 7: Validation, docs, closeout](#phase-7-validation-docs-closeout)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
- [Follow-up Work](#follow-up-work)
- [References](#references)
<!--toc:end-->

## Objective

Implement three related designs as a single coordinated effort:

- **DESIGN-0008** — `spt watches update <id>` CLI subcommand so operators
  can change watch fields (especially filters and threshold) without
  hand-crafting SQL.
- **DESIGN-0009** — Fix the Discord notifier's off-by-one (10 alerts +
  1 summary = 11 embeds → HTTP 400) and add chunked sends with
  `X-RateLimit-*` header tracking, 429 handling, and a partial-success
  contract for `MarkAlertsNotified`.
- **DESIGN-0010** — Server-rendered `/alerts` UI with paginated table,
  per-alert detail page (`/alerts/{id}`), ILIKE title search, dismiss /
  restore / retry actions, and an optional Discord summary-only mode
  that links to the page instead of blasting the channel. Built with
  templ + HTMX + Alpine, gated by a `web.enabled` feature flag.

The three are bundled because they all branch off the same incident
(the PR #44 rescore that produced ~2,100 unsendable alerts) and the
fixes are most useful together: the CLI fixes the *cause* (over-broad
watch filters), the notifier fix unblocks *delivery*, and the UI gives
us a *triage* surface so volume becomes manageable instead of merely
deliverable.

**Implements:** DESIGN-0008, DESIGN-0009, DESIGN-0010

## Scope

### In Scope

**CLI (DESIGN-0008):**

- New CLI subcommand `spt watches update <id>` with replace / merge /
  clear filter semantics, GET-then-PUT against existing
  `PUT /api/v1/watches/{id}`.

**Discord notifier (DESIGN-0009):**

- Off-by-one fix in `(*DiscordNotifier).SendBatchAlert` so a >10-alert
  batch produces a valid 10-embed payload (9 alerts + 1 summary)
  instead of the current 11-embed 400.
- Chunked Discord sends that deliver every alert (no truncation) by
  splitting into `ceil(N/10)` POSTs.
- In-process Discord rate-limit tracker that parses `X-RateLimit-*`
  headers and waits before the next chunk when `Remaining == 0`.
- 429 handling with `Retry-After` parse, single retry, and `global=true`
  short-circuit.
- Notifier interface change: `SendBatchAlert` returns
  `(sent int, err error)`; engine marks only delivered alerts.
- Optional `notify.discord.inter_chunk_delay` config field
  (default `0s`) for defensive throttling beyond what headers require.

**Alert review UI (DESIGN-0010):**

- New `dismissed_at TIMESTAMPTZ NULL` column on `alerts` plus partial
  index and a composite `(score DESC, created_at DESC)` index, behind
  migration `009_alerts_dismissed_at.sql`.
- Store interface additions: `ListAlertsForReview`, `DismissAlerts`,
  `RestoreAlerts`, `GetAlertDetail` (joins listing + watch +
  notification history).
- **templ + HTMX + Alpine** stack. Templates live as `*.templ` files
  generated to `*_templ.go` via the `templ` CLI; HTMX and Alpine
  served as static files from an `embed.FS`.
- Echo route group `/alerts` serving:
  - `GET /alerts` — HTML list page (paginated table, search, filter
    chips, bulk dismiss).
  - `GET /alerts.json` — JSON mirror.
  - `GET /alerts/{id}` — HTML detail page (full listing card, score
    breakdown, watch info, notification history, action buttons).
  - `POST /alerts/{id}/dismiss` — single dismiss.
  - `POST /alerts/dismiss` — bulk dismiss (form POST with
    `name="ids"` checkboxes).
  - `POST /alerts/{id}/restore` — undismiss.
  - `POST /alerts/{id}/retry` — re-send via Discord (rich embed,
    bypasses idempotency, always sends per-alert regardless of
    summary mode).
- HTMX-driven search with `hx-trigger="keyup changed delay:300ms"
  hx-target="#alerts-table" hx-swap="innerHTML" hx-push-url="true"`.
- Bulk dismiss via `hx-boost="true"` form (graceful no-JS fallback) +
  Alpine `x-data` for select-all / shift-click range / "N selected"
  counter.
- `web.enabled` feature flag (binary config + Helm values toggle,
  default true) gating the entire `/alerts` route group.
- New config `web.alerts_url_base` (string, default empty) — when set,
  Discord summary embeds and any other deep-links use it as the URL
  base.

**Engine changes:**

- Engine-level summary-mode branch that aggregates pending alerts into
  a single rollup embed when `notify.discord.summary_only` is true.
- Manual retry path (`POST /alerts/{id}/retry`) calls
  `Notifier.SendAlert` directly, bypasses
  `HasSuccessfulNotification`, records a `notification_attempts`
  row, leaves `notified_at` and `dismissed_at` unchanged.

**Status enum:**

- `parseAlertsListQuery` accepts an explicit `Status: "undismissed"`
  value alongside `active`, `dismissed`, `notified`, `all`. URL stays
  honest regardless of summary-mode configuration.

**Metrics:**

- `spt_discord_rate_limit_remaining` (Gauge)
- `spt_discord_rate_limit_waits_total` (Counter)
- `spt_discord_429_total{global}` (CounterVec)
- `spt_discord_chunks_sent_total` (Counter)
- `spt_alerts_dismissed_total` (Counter)
- `spt_alerts_query_duration_seconds{query="list|count|detail|dismiss|restore"}` (HistogramVec)
- `spt_alerts_table_rows` (Gauge, updated on each list query)
- `spt_notification_attempts_inserted_total{result="success|failure"}`
  (CounterVec)
- One dashgen panel surfacing alert query latency p50/p95/p99 + total
  row count on the existing Grafana dashboard.

**Closeout:**

- Status updates on DESIGN-0008/0009/0010 from `Draft` to `Implemented`
  on completion.

### Out of Scope

- Authentication on `/alerts`. Stays consistent with the current
  unauthenticated `/docs` and `/metrics` posture; document the gap and
  defer auth to a deployment-layer concern (basic-auth sidecar via
  Cilium HTTPRoute or oauth2-proxy).
- A separate Bun/React SPA. Considered and deferred; templ stack
  scales for the expected trajectory. Revisit if the UI grows past
  list + detail + the small set of write actions in this IMPL.
- Persisted (DB-backed) Discord rate-limit state. Single-replica
  today, in-memory tracker is sufficient. DESIGN-0009 Phase 3 stays
  deferred.
- Batched `InsertNotificationAttempts(ctx, attempts)` method. Per-alert
  inserts are fine at our scale; batched insert tracked under
  Follow-up Work with explicit trigger criteria.
- CNPG-side `pg_stat_statements` exposition through
  `monitoring.customQueriesConfigMap`. Deserves its own design;
  tracked under Follow-up Work.
- `pg_trgm` GIN index on `listings.title`. ILIKE is fine at current
  volume (~10K rows); revisit if search latency becomes a complaint.
- Alert TTL / archival.
- Bulk `spt watches update --where component=ram` style operations.
  Single-watch update only, per DESIGN-0008 non-goals.
- Renaming watch IDs.
- Schema changes to `watches.filters` JSONB shape.
- Multi-webhook routing (per-watch Discord destinations).
- Touching `extraction_*` metrics or any non-notifier Prometheus
  surface.
- A `triggered_by` audit column on `notification_attempts` to
  distinguish scheduled vs manual retries — relying on row insertion
  order and timestamps is sufficient.

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all
its tasks are checked off and its success criteria are met. Phases 1,
2, and 3 are independent of each other and could ship in any order or
in parallel; phase 4 depends on phase 3; phase 5 depends on phase 1
(same notifier code) but is independent of 2/3/4; phase 6 depends on
4 and 5; phase 7 depends on everything.

The intended ship sequence is **1 → 2 → 3 → 4 → 5 → 6 → 7**. Phase 1
lands as the first commit on the existing
`fix/post-score-alert-evaluation` branch (per resolved Q1) and the
rest follow on the same branch — single PR, reviewer can approve
incrementally.

---

### Phase 1: Discord notifier off-by-one fix (DESIGN-0009 Phase 1)

The smallest, highest-value change. One file, one constant, ~5 lines
of code, plus a unit test that asserts `len(payload.Embeds) <= 10` for
batches of 10, 11, 25, and 100. This is the only task that must ship
before doing anything else — every batch send today produces a 400.

#### Tasks

- [ ] In `internal/notify/discord.go`, add `const maxEmbedsPerMessage = 10`
      near the existing `colorGreen`/`colorYellow`/`colorOrange` block.
- [ ] In `(*DiscordNotifier).SendBatchAlert`, compute `alertCap` so
      that when `len(alerts) > maxEmbedsPerMessage` we reserve a slot
      for the summary embed:
  ```go
  alertCap := len(alerts)
  if alertCap > maxEmbedsPerMessage {
      alertCap = maxEmbedsPerMessage - 1
  }
  ```
- [ ] Replace the `min(len(alerts), 10)` literal with `alertCap`.
- [ ] Update the summary-embed `Title` to use `len(alerts) - alertCap`
      so the count is correct (currently `len(alerts) - 10`, which is
      off by one when we cap at 9).
- [ ] In `internal/notify/discord_test.go`, add a table-driven test
      `TestSendBatchAlert_EmbedLimit` with cases for `n = 1, 9, 10,
      11, 25, 100`. Use `httptest.NewServer` to capture the posted
      payload; assert `len(decoded.Embeds) <= 10` in every case and
      that the summary embed text reflects the right "and N more"
      count for `n > 10`.
- [ ] Run `make lint` and `make fmt`.
- [ ] Run `make test ./internal/notify/...` and verify the new test
      passes.

#### Success Criteria

- For any input slice size `n`, `SendBatchAlert` posts a payload with
  `len(Embeds) <= 10`.
- For `n > 10`, the payload contains exactly 9 alert embeds plus 1
  summary embed (10 total) and the summary text reads
  `... and (n-9) more alerts for <watchName>`.
- Existing notifier unit tests continue to pass.
- `make lint` clean on the modified files.

---

### Phase 2: spt watches update CLI command (DESIGN-0008)

Add the missing `update` subcommand to the watches CLI. The HTTP
client method `UpdateWatch` already exists
(`internal/api/client/watches.go:58`), the server endpoint
`PUT /api/v1/watches/{id}` already exists
(`internal/api/handlers/watches.go:156`), and the filter parser
`handlers.ParseFilters` already exists
(`internal/api/handlers/filters.go:23`). This phase is pure glue: a
new Cobra command that uses Cobra's `Changed()` to detect
explicitly-set flags, fetches current state via `client.GetWatch`,
applies the deltas, and PUTs the updated body.

#### Tasks

- [ ] Create `cmd/spt/cmd/watches_update.go` with a new
      `watchesUpdateCmd` `*cobra.Command` and `init()` that registers
      it under `watchesCmd` (the existing parent in
      `cmd/spt/cmd/watches.go`).
- [ ] Define flags on `watchesUpdateCmd`:
  - `--name string`
  - `--query string`
  - `--category string`
  - `--component string` (with validation against the
    `ram|drive|server|cpu|nic|other` enum on `RunE`)
  - `--threshold int`
  - `--enabled bool`
  - `--filter stringSlice` (replace semantics)
  - `--add-filter stringSlice` (merge semantics)
  - `--clear-filters bool`
- [ ] In `RunE`:
  - [ ] Validate exactly one of `{--clear-filters, --filter, --add-filter}`
        was passed (or none — leave filters untouched). Two-of-the-three
        is a usage error.
  - [ ] Construct an API client via `newAPIClient(cmd)` (existing
        helper used in other CLI commands).
  - [ ] Call `client.GetWatch(ctx, args[0])` to fetch current state.
  - [ ] For each scalar flag, copy its value into the fetched watch
        only if `cmd.Flags().Changed("<name>")` returned `true`.
  - [ ] For filters, branch:
    - `--clear-filters`: set `Filters = domain.WatchFilters{}` (the
      zero value).
    - `--filter` set: parse with `handlers.ParseFilters`, replace the
      entire `Filters` block.
    - `--add-filter` set: parse, then merge the parsed
      `AttributeFilters` into the existing map (key-by-key overwrite).
    - None of the three: leave `Filters` as fetched.
  - [ ] Call `client.UpdateWatch(ctx, updated)`.
  - [ ] Print the updated watch to stdout via the existing
        `printWatch(updated, output)` helper (mirrors
        `watchesCreateCmd` behavior).
- [ ] Create `cmd/spt/cmd/watches_update_test.go` with table-driven
      tests for the merge/replace logic — extract the merge logic into
      a small pure helper `applyFilterUpdates(current, filterFlag,
      addFlag []string, clear bool) (domain.WatchFilters, error)` so
      it's testable without spinning up a Cobra command.
  - [ ] Test cases: no flags (preserved), `--filter k=v` (replaced),
        `--add-filter k=v` (merged into existing map),
        `--add-filter k=v` overwrites same key, `--clear-filters`
        empties, mutually-exclusive flags return error.
- [ ] In `internal/api/client/client_test.go`, add (or extend) one
      round-trip test that asserts a partial update PUTs the full
      updated body (since the existing `TestClient_UpdateWatch` only
      covers the happy path).
- [ ] Update `docs/OPERATIONS.md` "Watch Management" section with one
      `spt watches update` example (changing a threshold and adding a
      filter).
- [ ] Update `docs/USAGE.md` to reference the new command in its
      command summary.
- [ ] Run `make lint`, `make fmt`,
      `make test ./cmd/spt/... ./internal/api/client/...`.

#### Success Criteria

- Running `spt watches update <id> --threshold 80` against a live
  server fetches the current watch, sets `score_threshold = 80`, and
  PUTs the updated body. Other fields are preserved.
- Running `spt watches update <id> --filter "capacity_gb=eq:32"`
  replaces the entire `attribute_filters` map with the parsed value.
- Running `spt watches update <id> --add-filter "capacity_gb=eq:32"`
  merges the new key into the existing map without dropping other
  keys.
- Running `spt watches update <id> --clear-filters` empties the filter
  block.
- Running with two of `{--filter, --add-filter, --clear-filters}`
  exits non-zero with a clear error.
- Unit tests for `applyFilterUpdates` cover all five flag
  combinations.
- `docs/OPERATIONS.md` and `docs/USAGE.md` reference the new command.

---

### Phase 3: Alerts schema + dismiss store API (DESIGN-0010 prerequisites)

Adds the `dismissed_at` column, two indexes, and the store-layer
methods that the UI handlers in Phase 4 will consume. No HTTP surface
yet — purely the data plumbing. Splitting it out lets us land
migration + store changes early so the schema is settled before
template work begins.

#### Tasks

- [ ] Create `migrations/009_alerts_dismissed_at.sql` with:
  ```sql
  ALTER TABLE alerts
      ADD COLUMN dismissed_at TIMESTAMPTZ NULL;
  CREATE INDEX idx_alerts_dismissed_at ON alerts(dismissed_at)
      WHERE dismissed_at IS NULL;
  CREATE INDEX idx_alerts_score_created ON alerts(score DESC, created_at DESC);
  ```
  Both indexes per resolved Q9 — partial dismissed_at index for the
  dismiss workflow, composite for the default page sort.
- [ ] Copy `009_alerts_dismissed_at.sql` to
      `internal/store/migrations/` (the embed.FS source).
- [ ] In `pkg/types/alert.go` (or wherever `domain.Alert` lives), add
      `DismissedAt *time.Time` field with JSON tag
      `json:"dismissed_at,omitempty"`.
- [ ] In `internal/store/queries.go`, update every `SELECT` against
      the `alerts` table to include `dismissed_at` in the column list.
      Match column order in the existing `scanAlert` / `scanAlertRow`
      helpers — see the `MEMORY.md` note that scan functions must
      mirror SELECT order.
- [ ] In `internal/store/queries.go`, add new query constants:
  - `qListAlertsForReview` — parameterized SELECT that joins
    `alerts → listings → watches`, filters by status / type / watch /
    `min_score`, optional `ILIKE` on title, with `LIMIT $N OFFSET $M`.
  - `qCountAlertsForReview` — same WHERE, `count(*)` only.
  - `qGetAlertDetail` — single-row SELECT joining `alerts → listings
    → watches` plus a sub-select (or follow-up query) for
    `notification_attempts` rows ordered DESC.
  - `qDismissAlerts` — `UPDATE alerts SET dismissed_at = now()
    WHERE id = ANY($1) AND dismissed_at IS NULL RETURNING id`.
  - `qRestoreAlerts` — `UPDATE alerts SET dismissed_at = NULL
    WHERE id = ANY($1) RETURNING id`.
- [ ] In `internal/store/store.go`, extend the `Store` interface:
  ```go
  ListAlertsForReview(ctx context.Context, q AlertReviewQuery) (AlertReviewResult, error)
  GetAlertDetail(ctx context.Context, id string) (*AlertDetail, error)
  DismissAlerts(ctx context.Context, ids []string) (int, error)
  RestoreAlerts(ctx context.Context, ids []string) (int, error)
  ```
  where `AlertReviewQuery` contains `Search string`,
  `ComponentType string`, `WatchID string`, `MinScore int`,
  `Status string` (active / dismissed / notified / undismissed /
  all), `Sort string`, `Page int`, `PerPage int`; and
  `AlertReviewResult` contains `Items []domain.AlertWithListing`,
  `Total int`, `Page int`, `PerPage int`. `AlertDetail` carries the
  joined listing/watch/score-breakdown plus
  `NotificationHistory []domain.NotificationAttempt`.
  Note Status enum includes the explicit `undismissed` value per
  resolved Q7.
- [ ] Implement the four new methods in `internal/store/postgres.go`
      with safe parameter binding (no string concatenation for the
      `ILIKE` clause — use `'%' || $1 || '%'`).
- [ ] Wrap each store method's body with the
      `metrics.AlertsQueryDuration.WithLabelValues("<op>").Observe(...)`
      timing pattern (registered in Phase 4).
- [ ] Run `make mocks` to regenerate `MockStore` with the new
      methods.
- [ ] Add `internal/store/postgres_test.go` cases (use the existing
      `pgtestdb` setup pattern) for:
  - `ListAlertsForReview` with no filters (returns all).
  - `ListAlertsForReview` with a search substring (matches via ILIKE).
  - `ListAlertsForReview` with `Status: "dismissed"` after a dismiss.
  - `ListAlertsForReview` with `Status: "undismissed"` returns rows
    regardless of `notified_at`.
  - `LIMIT/OFFSET` correctness (insert 30, request `per_page=10,
    page=2`, assert items 11-20).
  - `GetAlertDetail` returns nested listing + watch + notification
    history.
  - `DismissAlerts` returns the row count and is idempotent (calling
    it twice on the same IDs only updates once).
  - `RestoreAlerts` clears `dismissed_at`.
- [ ] Run `make lint`, `make fmt`, `make test ./internal/store/...`.

#### Success Criteria

- Migration 009 applies cleanly on a populated `alerts` table
  (verified via `pgtestdb` integration test).
- Both indexes (`idx_alerts_dismissed_at`, `idx_alerts_score_created`)
  are created.
- `Store` interface exposes `ListAlertsForReview`, `GetAlertDetail`,
  `DismissAlerts`, `RestoreAlerts` and `MockStore` mocks them.
- `domain.Alert` (and any wrapping result type) carries
  `DismissedAt`.
- All existing alert-related tests continue to pass after the schema
  scan-order change.
- New store tests cover at least one happy path per new method plus
  the search, pagination, and `undismissed` status cases.

---

### Phase 4: Alert review UI handlers, list page, detail page (DESIGN-0010 core)

The user-visible piece of DESIGN-0010. Templ-based components, HTMX
for interactivity, Alpine for small reactive bits. New Echo route
group, embedded HTMX/Alpine static assets, JSON mirror, dismiss /
restore / retry endpoints, and the per-alert detail page. Continues
to ship with Discord behavior unchanged from Phase 1 — flipping
summary mode is Phase 6.

#### Tasks

**Tooling setup:**

- [ ] Pin `templ` in `mise.toml` (latest stable, e.g.
      `go:github.com/a-h/templ/cmd/templ 0.2.x`). Run `mise install`
      to confirm.
- [ ] Add `make templ-generate` target to
      `scripts/makefiles/go.mk` (or similar):
  ```makefile
  templ-generate:
  	templ generate
  ```
- [ ] Add `templ generate` to `make build` (and CI's `make ci`) so
      generated `*_templ.go` is always fresh. Either add it as a
      dependency of `build`, or wire `//go:generate templ generate`
      directives in the `internal/api/web/` package and run
      `go generate ./...` as part of `make build`.
- [ ] Decide commit-vs-gitignore for `*_templ.go`. Recommendation:
      gitignore generated files; CI / make build always regenerates.
      Keeps PR diffs clean. Document the decision in CLAUDE.md.
- [ ] Add `internal/api/web/static/htmx.min.js` (~14KB) and
      `internal/api/web/static/alpine.min.js` (~15KB) — committed
      static files served from the embed. Pin specific versions and
      record them in the file comments / commit message.

**Template components (templ):**

- [ ] Create `internal/api/web/embed.go` declaring
      `//go:embed templates/*.templ static/*` `var FS embed.FS` (the
      static files are embedded at build time; `*.templ` files are
      embedded too for source reference but compiled via codegen).
- [ ] Create `internal/api/web/components/layout.templ` with
      `templ Layout(title string)` that includes htmx.min.js and
      alpine.min.js + the static CSS link.
- [ ] Create `internal/api/web/components/alerts_page.templ` with
      `templ AlertsPage(data AlertsPageData)` rendering the search
      input, filter chips, and `@AlertsTable(data.Result)`. The
      search input carries:
      `hx-get="/alerts" hx-trigger="keyup changed delay:300ms"
       hx-target="#alerts-table" hx-swap="innerHTML"
       hx-push-url="true"`.
- [ ] Create `internal/api/web/components/alerts_table.templ` with
      `templ AlertsTable(result AlertReviewResult)` rendering the
      table body + pagination controls. ID `#alerts-table` on the
      outer wrapper so HTMX swaps it cleanly. Bulk dismiss form uses
      `hx-boost="true" action="/alerts/dismiss" method="post"`.
- [ ] Create `internal/api/web/components/alert_row.templ` with
      `templ AlertRow(a domain.AlertWithListing)` — shared between
      the page render and any HTMX swap responses (compile-time
      guarantee they match).
- [ ] Create `internal/api/web/components/alert_detail.templ` with
      `templ AlertDetailPage(d AlertDetail)` rendering the full
      listing card, score-breakdown table, watch info, notification
      history list, and action buttons (Retry, Dismiss/Restore,
      External link to eBay).
- [ ] Create `internal/api/web/components/score_badge.templ` with
      `templ ScoreBadge(score int)` for consistent score coloring
      matching Discord embeds.
- [ ] Create `internal/api/web/components/notification_history.templ`
      with `templ NotificationHistory(attempts []domain.NotificationAttempt)`.
- [ ] Add Alpine `x-data` to the bulk-dismiss control on the table
      template:
  ```html
  <div x-data="{ selected: [], allSelected: false }">
    <input type="checkbox" x-model="allSelected"
           @change="document.querySelectorAll('input[name=ids]').forEach(c => c.checked = allSelected)">
    <span x-text="`${selected.length} selected`"></span>
    <button type="submit" :disabled="selected.length === 0">Dismiss selected</button>
  </div>
  ```
  Plus shift-click range select on row checkboxes (~15 lines).
- [ ] Create `internal/api/web/static/spt.css` (~5KB) with score
      color variables and minimal layout styling.

**Template renderer:**

- [ ] Create `internal/api/web/renderer.go` with a small `echo.Renderer`
      adapter that delegates to `templ`'s `Render(ctx, w)` method, so
      handler code can call
      `c.Render(http.StatusOK, components.AlertsPage(data))` or
      similar. (Templ doesn't *need* `echo.Renderer` — handlers can
      call `Render(c.Request().Context(), c.Response().Writer)`
      directly. Whichever is cleaner per the templ examples.)

**Handlers:**

- [ ] Create `internal/api/handlers/alerts_ui.go` with
      `AlertsUIHandler` accepting `store.Store`, `notify.Notifier`,
      and a config struct (`AlertsURLBase string`). Methods:
  - [ ] `(h *AlertsUIHandler) ListPage(c echo.Context) error` —
        parses query via `parseAlertsListQuery`, calls
        `s.ListAlertsForReview`, renders `AlertsPage` for normal
        requests, `AlertsTable` partial for HTMX requests
        (inspect `c.Request().Header.Get("HX-Request") == "true"`).
  - [ ] `(h *AlertsUIHandler) ListJSON(c echo.Context) error` — same
        query, returns `AlertReviewResult` as JSON.
  - [ ] `(h *AlertsUIHandler) DetailPage(c echo.Context) error` —
        calls `s.GetAlertDetail`, renders `AlertDetailPage`. 404 if
        not found.
  - [ ] `(h *AlertsUIHandler) DismissOne(c echo.Context) error` —
        path param `id`, calls `s.DismissAlerts(ctx, []string{id})`,
        returns the updated row partial via `AlertRow` for HTMX
        requests, redirect to `/alerts` for non-HTMX.
  - [ ] `(h *AlertsUIHandler) DismissBulk(c echo.Context) error` —
        form values `ids` (or JSON body `{"ids": [...]}` for the
        JSON mirror), calls `s.DismissAlerts(ctx, ids)`, returns
        the updated table partial via `AlertsTable` for HTMX,
        redirect for non-HTMX.
  - [ ] `(h *AlertsUIHandler) Restore(c echo.Context) error` —
        path param `id`, calls `s.RestoreAlerts(ctx,
        []string{id})`.
  - [ ] `(h *AlertsUIHandler) Retry(c echo.Context) error` — path
        param `id`, fetches the alert detail, builds an
        `AlertPayload`, calls `n.SendAlert(ctx, payload)` (single
        embed, bypasses `HasSuccessfulNotification`), records a
        `notification_attempts` row regardless of outcome, returns
        the updated `NotificationHistory` partial for HTMX, redirect
        to `/alerts/{id}` for non-HTMX. Per resolved Q3 expansion,
        retry always sends the rich per-alert embed regardless of
        `summary_only`.
  - [ ] `parseAlertsListQuery(c echo.Context) AlertReviewQuery` with
        defaults: `min_score=75`, `status=active`, `page=1`,
        `per_page=25` (max 100), `sort=score`. Invalid values fall
        back to defaults rather than 400ing. Recognizes the explicit
        `undismissed` status value (resolved Q7).

**Wiring:**

- [ ] In `cmd/server-price-tracker/cmd/serve.go` (or wherever Echo
      routes are registered), gate the `/alerts` group on
      `cfg.Web.Enabled`:
  ```go
  if cfg.Web.Enabled {
      h := handlers.NewAlertsUIHandler(store, notifier, cfg.Web)
      e.GET("/alerts", h.ListPage)
      e.GET("/alerts.json", h.ListJSON)
      e.GET("/alerts/:id", h.DetailPage)
      e.POST("/alerts/:id/dismiss", h.DismissOne)
      e.POST("/alerts/dismiss", h.DismissBulk)
      e.POST("/alerts/:id/restore", h.Restore)
      e.POST("/alerts/:id/retry", h.Retry)
      e.GET("/static/*", echo.WrapHandler(
          http.StripPrefix("/static/", http.FileServer(http.FS(web.StaticFS))),
      ))
  }
  ```
- [ ] In `internal/config/config.go`, add:
  - `Web.Enabled bool` (default `true`)
  - `Web.AlertsURLBase string` (default `""`)
- [ ] Update `configs/config.example.yaml` and
      `configs/config.dev.yaml` with the new keys and comments.

**Helm chart:**

- [ ] In `charts/server-price-tracker/values.yaml`, add:
  ```yaml
  web:
    enabled: true
    alertsUrlBase: ""  # absolute URL base; empty = links omitted
  ```
- [ ] In `charts/server-price-tracker/templates/configmap.yaml` (or
      whichever template renders the runtime config), wire
      `web.enabled` and `web.alertsUrlBase` into the container
      config. Keep generated config valid when both are unset
      (defaults bake into the binary).
- [ ] In `charts/server-price-tracker/tests/`, add a helm-unittest
      case asserting the `web.enabled` toggle round-trips into the
      config.

**Metrics:**

- [ ] In `internal/metrics/metrics.go`, register:
  - `AlertsDismissedTotal` (Counter)
  - `AlertsQueryDuration` (HistogramVec, label `query`,
    buckets `[0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5]`)
  - `AlertsTableRows` (Gauge)
  - `NotificationAttemptsInsertedTotal` (CounterVec, label `result`)
- [ ] Increment `AlertsDismissedTotal` from the dismiss handlers.
- [ ] Update `AlertsTableRows` from `ListAlertsForReview` after the
      `COUNT(*)` query.
- [ ] Increment `NotificationAttemptsInsertedTotal` everywhere
      `InsertNotificationAttempt` is called (engine + retry handler).
      Single-source helper would be cleanest:
      `func recordNotificationAttempt(...) { ...; metric.Inc() }`.

**Dashgen:**

- [ ] In `tools/dashgen/`, add a panel definition surfacing:
  - `histogram_quantile(0.50, rate(spt_alerts_query_duration_seconds_bucket[5m]))`
    p50 line per query type
  - p95 and p99 stacked
  - `spt_alerts_table_rows` as a single-stat
- [ ] Run `make dashboards` (or whatever the dashgen entrypoint is)
      and verify the JSON updates cleanly.

**Tests:**

- [ ] `internal/api/handlers/alerts_ui_test.go`:
  - Table-driven `parseAlertsListQuery` (defaults, `per_page` clamp
    to 100, invalid `sort` → fallback, invalid `min_score` →
    fallback, `undismissed` accepted).
  - Render smoke tests using `httptest.NewRecorder` and a
    `MockStore`: empty state, 1 row, 25 rows + pagination links.
  - HTMX vs non-HTMX branch: same handler, `HX-Request: true` header
    returns table partial; absent header returns full page.
  - `DismissBulk` round-trip: form POST `ids=a&ids=b` → mock store
    asserts `DismissAlerts` called with same slice.
  - `Retry` round-trip: mock store + mock notifier; assert
    `SendAlert` called once, `InsertNotificationAttempt` called
    with the outcome, `notified_at` and `dismissed_at` unchanged on
    the alert row.
  - `Web.Enabled = false` → routes return 404 (test by registering
    the gated routes and asserting absence).
- [ ] Run `make lint`, `make fmt`,
      `make test ./internal/api/...`.
- [ ] Manual smoke test on local dev: `make dev-setup`, `make run`,
      navigate to `http://localhost:8080/alerts`, exercise
      search-as-you-type, pagination, dismiss-and-row-disappears,
      restore, click into detail page, click Retry button, observe
      Discord receives a fresh embed and the notification history
      list refreshes in place.

#### Success Criteria

- `GET /alerts` returns a 200 HTML response built from templ
  components, with a paginated table, search input, filter chips, and
  bulk-dismiss form.
- `GET /alerts.json?min_score=80&type=ram` returns the same data
  filtered, with `total` / `page` / `per_page` metadata.
- `GET /alerts/{id}` returns the full detail page with listing card,
  score breakdown, watch info, notification history, and action
  buttons.
- HTMX-driven search updates the table in place with debounce; URL
  bar reflects the search via `hx-push-url`.
- HTMX-driven dismiss / restore / retry swap the affected DOM in
  place without a full reload; HTML form submit (no JS) still works
  via `hx-boost` fallback.
- `POST /alerts/{id}/dismiss` sets `dismissed_at`; row disappears
  from default `status=active` view.
- `POST /alerts/dismiss` with form `ids` checkbox values dismisses N
  alerts in one round-trip.
- `POST /alerts/{id}/restore` un-dismisses.
- `POST /alerts/{id}/retry` calls `Notifier.SendAlert`, records a
  `notification_attempts` row, leaves `notified_at` and
  `dismissed_at` unchanged.
- Templates and static files (HTMX, Alpine, CSS) are served from the
  embedded `embed.FS` — the binary still ships single-file.
- `web.enabled = false` removes the entire `/alerts` surface (404).
- Helm chart `values.yaml` exposes `web.enabled` and
  `web.alertsUrlBase`; helm-unittest passes.
- All five new metric series (`spt_alerts_dismissed_total`,
  `spt_alerts_query_duration_seconds`, `spt_alerts_table_rows`,
  `spt_notification_attempts_inserted_total`) appear at `/metrics`.
- Dashgen produces a panel surfacing alert query latency and row
  count.
- Manual smoke test passes on local dev.

---

### Phase 5: Discord chunked sends with rate-limit tracking (DESIGN-0009 Phase 2)

Replace truncation with chunking. After this phase, a 142-alert batch
sends 15 Discord POSTs (14 × 10 alerts + 1 × 2 alerts, no summary
needed in the chunked path) instead of one truncated 9+1 message.
Adds header parsing, in-process bucket state, 429 retry, optional
inter-chunk delay config, and changes the `Notifier` interface to
return `(sent int, err error)`.

#### Tasks

- [ ] In `internal/notify/notifier.go`, change the `Notifier`
      interface signature (per resolved Q2 — break, don't wrap):
  ```go
  SendBatchAlert(ctx context.Context, alerts []AlertPayload, watchName string) (sent int, err error)
  ```
- [ ] Update `internal/notify/noop.go` and any other implementations
      (none expected beyond `Discord` and `NoOp`) to match.
- [ ] In `internal/notify/discord.go`, add the `rateLimitState`
      struct:
  ```go
  type rateLimitState struct {
      mu        sync.Mutex
      bucket    string
      remaining int
      resetAt   time.Time
  }
  func (r *rateLimitState) update(resp *http.Response) { ... }
  func (r *rateLimitState) waitForBucket(ctx context.Context) (waited time.Duration, err error) { ... }
  func (r *rateLimitState) snapshotRemaining() int { ... }
  ```
  Initialize a single `*rateLimitState` per `DiscordNotifier` instance
  (matches the webhook-as-bucket assumption in DESIGN-0009).
- [ ] Add `chunkAlerts(alerts []AlertPayload, n int) [][]AlertPayload`
      pure helper. Easy to table-test.
- [ ] Rewrite `(*DiscordNotifier).SendBatchAlert` to:
  - Compute chunks via `chunkAlerts(alerts, maxEmbedsPerMessage)`.
  - For each chunk:
    - Call `d.rateLimit.waitForBucket(ctx)`.
    - Optionally `time.Sleep(d.interChunkDelay)` if configured (per
      resolved Q10, default `0s`).
    - Build embeds (no summary in the chunked path — every embed is
      an alert embed, so partial-success accounting maps 1-to-1).
    - Call `d.post(ctx, payload)`.
  - Track `sent` cumulatively; on first error, return
    `(sent, fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), err))`.
  - On full success return `(len(alerts), nil)`.
- [ ] Wire `interChunkDelay time.Duration` field on
      `DiscordNotifier`; expose via a new `WithInterChunkDelay(d)`
      option. Read from `cfg.Notify.Discord.InterChunkDelay` in
      `serve.go`.
- [ ] Rewrite `(*DiscordNotifier).post` to:
  - Always call `d.rateLimit.update(resp)` after `d.client.Do` and
    before any branching, including non-2xx paths.
  - Update `metrics.DiscordRateLimitRemaining.Set(float64(snapshot))`.
  - On 429: read `Retry-After`, check `X-RateLimit-Global`. If global
    or this is the second attempt, return error. Otherwise sleep
    `Retry-After` (respecting `ctx.Done()`), retry once.
  - On other non-2xx: existing read-body, return-error behavior.
  - Loop counter caps at 2 attempts.
- [ ] Add Prometheus metrics in `internal/metrics/metrics.go`:
  - `DiscordRateLimitRemaining` (Gauge)
  - `DiscordRateLimitWaitsTotal` (Counter)
  - `Discord429Total` (CounterVec, label `global="true"|"false"`)
  - `DiscordChunksSentTotal` (Counter)
- [ ] In `internal/engine/alert.go`, update `sendBatch`:
  - Replace the single `n.SendBatchAlert(ctx, payloads, watch.Name)`
    call site with the new `(sentCount, sendErr)` signature.
  - When `sentCount > 0`: slice `alertIDs[:sentCount]`, call
    `s.MarkAlertsNotified(ctx, delivered)`, and emit one
    `InsertNotificationAttempt(true, ...)` per delivered ID
    (per-alert inserts per resolved Q8).
  - When `sendErr != nil`: emit
    `InsertNotificationAttempt(false, ...)` for each ID in
    `alertIDs[sentCount:]` with the error text.
  - Return `sendErr` so the outer loop in `ProcessAlerts` keeps its
    failure path.
- [ ] In `internal/config/config.go`, add
      `Notify.Discord.InterChunkDelay time.Duration` (default `0s`).
- [ ] Update `configs/config.example.yaml` with the new key + a
      comment explaining when to set it (defensive throttling beyond
      header-driven waits).
- [ ] Run `make mocks` to regenerate `MockNotifier` with the new
      signature.
- [ ] Update `internal/engine/alert_test.go`:
  - All `mn.EXPECT().SendBatchAlert(...).Return(nil)` become
    `Return(<n>, nil)` where `<n>` is the count of input payloads.
  - Add new test `TestProcessAlerts_BatchPartialFailure`:
    `SendBatchAlert` returns `(20, errors.New("chunk 3 failed"))` on
    a 30-alert batch; assert `MarkAlertsNotified` is called with the
    first 20 IDs only and `InsertNotificationAttempt(false, ...)`
    for the last 10.
- [ ] Update `internal/notify/discord_test.go`:
  - Table-driven `TestChunkAlerts` over
    `n = {0, 1, 9, 10, 11, 21, 100}` with assertions on chunk count
    and last-chunk size.
  - `TestSendBatchAlert_Chunking_15` — `httptest.NewServer` records
    payloads; assert exactly 2 POSTs, both `len(embeds) <= 10`,
    return value `(15, nil)`.
  - `TestSendBatchAlert_RateLimitWait` — first response carries
    `X-RateLimit-Remaining: 0`, `X-RateLimit-Reset-After: 0.05`;
    assert second POST occurs at least 50ms after the first.
  - `TestSendBatchAlert_429Retry` — first response is 429 with
    `Retry-After: 0.02`, second is 200; assert one retry, return
    success.
  - `TestSendBatchAlert_429Global` — 429 with
    `X-RateLimit-Global: true`; assert no retry, error returned,
    `sent == 0` for that chunk.
  - `TestSendBatchAlert_InterChunkDelay` — wire a 25ms delay; assert
    second POST is at least 25ms after first even when headers say
    capacity is available.
- [ ] Run `make lint`, `make fmt`,
      `make test ./internal/notify/... ./internal/engine/...`.

#### Success Criteria

- A 100-alert batch sends 10 POSTs to Discord, each with ≤10 embeds,
  with no truncation.
- When a response carries `X-RateLimit-Remaining: 0`, the next POST
  waits until at least `X-RateLimit-Reset-After` has elapsed.
- A single 429 with non-global rate-limit header is retried once
  after `Retry-After` elapses; a 429 with `X-RateLimit-Global: true`
  aborts immediately without retry.
- `sent` reflects the number of alert embeds (not chunks)
  successfully delivered. Engine marks only those IDs notified.
- All four new metric series are registered and increment under the
  conditions named.
- `notify.discord.inter_chunk_delay` config is honored when set.
- Existing `TestProcessAlerts_*` tests pass with updated return
  signature.

---

### Phase 6: Discord summary mode (DESIGN-0010 + 0009 join)

Wire the `notify.discord.summary_only` flag. When enabled, one
scheduler tick produces one Discord embed regardless of pending alert
count, and every pending alert gets `MarkAlertNotified` so the page
becomes the work surface. Operators can flip the page's status filter
to `undismissed` (the explicit enum value from Phase 3) to see the
full queue.

#### Tasks

- [ ] Add config field `notify.discord.summary_only` (bool, default
      `false`) in `internal/config/config.go` and the example config
      `configs/config.example.yaml`.
- [ ] Add `BuildSummaryPayload(alerts []domain.Alert,
      alertsURLBase string) *notify.AlertPayload` helper in
      `internal/engine/alert.go`. Synthesizes a single payload with:
  - `WatchName: "Summary"`
  - `ListingTitle: fmt.Sprintf("%d new alerts (top score %d)", len(alerts), topScore)`
  - `EbayURL: alertsURLBase + "/alerts"` (when non-empty)
  - One field per component-type with the count
  - Color from the top alert's score
- [ ] In `internal/engine/alert.go`'s `ProcessAlerts`, branch on
      `cfg.SummaryOnly`:
  - **False**: existing per-watch grouped path (with Phase 5
    chunking behavior).
  - **True**: skip the per-watch group loop. Build one payload via
    `BuildSummaryPayload`, call `n.SendAlert(ctx, payload)`. On
    success, `MarkAlertsNotified(ctx, allIDs)` and emit one
    `InsertNotificationAttempt(true, ...)` per ID. On failure,
    record failed attempt for each ID and do NOT mark any notified.
- [ ] Update `docs/OPERATIONS.md` "Alert Review UI" section to:
  - Document the `summary_only` flag and what changes when it flips
    (Discord becomes one embed/tick, page becomes the queue).
  - Recommend defaulting the bookmarked `/alerts` URL to
    `?status=undismissed` once summary mode is on, so the operator's
    queue view persists.
  - Document the `web.enabled` flag and the page's lack of built-in
    auth — recommend a Cilium HTTPRoute filter or oauth2-proxy
    sidecar for production.
- [ ] Tests:
  - [ ] `TestProcessAlerts_SummaryMode_SingleEmbed` — 50 pending
        alerts, `cfg.SummaryOnly = true`; assert one `SendAlert`
        call with a synthesized payload, `MarkAlertsNotified` called
        with all 50 IDs.
  - [ ] `TestProcessAlerts_SummaryMode_NoNewAlerts` — 0 pending;
        assert no `SendAlert` call.
  - [ ] `TestProcessAlerts_SummaryMode_SendFailure` — `SendAlert`
        returns error; assert no IDs marked notified, all attempts
        recorded as failures.
  - [ ] `TestBuildSummaryPayload` — table-driven: counts by type,
        top-score selection, URL construction with and without
        `alertsURLBase`.
- [ ] Run `make lint`, `make fmt`,
      `make test ./internal/engine/...`.

#### Success Criteria

- With `notify.discord.summary_only = true`, a scheduler tick that
  finds 142 pending alerts produces exactly 1 Discord POST (the
  summary embed) and marks all 142 alerts notified.
- The summary embed carries a hyperlink to
  `<alerts_url_base>/alerts` when `web.alerts_url_base` is configured,
  otherwise omits the link (graceful empty state).
- Operators can navigate to `/alerts?status=undismissed` to see the
  queue regardless of summary mode (URL stays honest per resolved
  Q7).
- Tests cover empty-pending and send-failure cases without
  regressing the existing per-watch path.

---

### Phase 7: Validation, docs, closeout

End-to-end smoke against a live deployment, status updates, and final
linter pass.

#### Tasks

**User-side runbook (operational, run against a live deployment):**

1. [ ] Apply migration 009 against prod (verified via `helm upgrade`
       runs the migration init container, or manually via
       `./build/bin/server-price-tracker migrate`).
2. [ ] Run `spt watches update <id> --threshold 80
       --add-filter capacity_gb=eq:32` against prod, confirm via
       `spt watches get <id>` that the change took.
3. [ ] Trigger `spt rescore` to surface a backlog of alerts.
4. [ ] Open `https://spt.fartlab.dev/alerts`, confirm pagination,
       search-as-you-type, filter chips work; dismiss a few rows
       (HTMX swap-in-place); click into a detail page; confirm Retry
       Discord sends a fresh embed and the notification history list
       refreshes in place.
5. [ ] With `summary_only = false` (default), confirm Discord
       receives chunked notifications without 400 errors and
       `spt_discord_chunks_sent_total` increments.
6. [ ] Flip `summary_only = true` in dev config, restart, trigger
       another rescore, confirm one summary embed lands and links
       back to `/alerts`.
7. [ ] Confirm Prometheus exposes:
       - `spt_discord_rate_limit_remaining`
       - `spt_discord_rate_limit_waits_total`
       - `spt_discord_429_total{global}`
       - `spt_discord_chunks_sent_total`
       - `spt_alerts_dismissed_total`
       - `spt_alerts_query_duration_seconds`
       - `spt_alerts_table_rows`
       - `spt_notification_attempts_inserted_total{result}`
8. [ ] Verify Grafana dashboard shows the new alert query latency
       panel populated with real data after a few page loads.
9. [ ] Verify `web.enabled = false` removes `/alerts` (returns 404)
       by flipping the flag, restarting, and confirming.

**Implementation-side closeout:**

- [ ] Verify `make build` produces working `server-price-tracker`
      and `spt` binaries (templ generates first via the `make build`
      dependency chain).
- [ ] Run `make test` (full unit suite), `make lint`, `make lint-md`,
      `make lint-yaml`, `make helm-test`.
- [ ] Run `make mocks` and confirm no diff (any drift means a
      regenerate was missed in an earlier phase).
- [ ] Update
      `docs/design/0008-add-spt-watches-update-cli-command.md` status
      from `Draft` to `Implemented`. Run `docz update design`.
- [ ] Update
      `docs/design/0009-discord-notifier-rate-limiting-and-embed-chunking.md`
      status from `Draft` to `Implemented`. Run `docz update design`.
- [ ] Update
      `docs/design/0010-alert-review-ui-with-pagination-and-search.md`
      status from `Draft` to `Implemented`. Run `docz update design`.
- [ ] Update this IMPL's status from `Draft` to `Completed`. Run
      `docz update impl`.
- [ ] Update `CLAUDE.md` and `MEMORY.md` with patterns surfaced
      during implementation: notifier interface change, summary-mode
      semantics, alerts schema scan order, templ build step,
      `web.enabled` feature flag, HTMX swap pattern.
- [ ] Update PR description on the merge PR to reference all three
      designs and the IMPL.

#### Success Criteria

- Live smoke checks 1-9 all pass.
- `make build`, `make test`, `make lint`, `make lint-md`,
  `make helm-test` clean.
- All three DESIGN docs are `Implemented`; IMPL-0015 is `Completed`.
- Changelog entry on the next release covers the four behaviors:
  CLI update, Discord chunking, alert review UI (list + detail), and
  summary mode.

---

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `internal/notify/discord.go` | Modify | Phase 1: off-by-one fix; Phase 5: rateLimitState, chunking, post() retry loop, interChunkDelay |
| `internal/notify/discord_test.go` | Modify | Phases 1 + 5: embed-limit, chunking, rate-limit-wait, 429, inter-chunk-delay tests |
| `internal/notify/notifier.go` | Modify | Phase 5: `SendBatchAlert` signature `(int, error)` |
| `internal/notify/noop.go` | Modify | Phase 5: signature parity |
| `internal/notify/mocks/notifier.go` | Generate | Phase 5: regenerated via `make mocks` |
| `cmd/spt/cmd/watches_update.go` | Create | Phase 2: new `update` subcommand |
| `cmd/spt/cmd/watches_update_test.go` | Create | Phase 2: filter merge/replace logic tests |
| `internal/api/client/client_test.go` | Modify | Phase 2: extend `TestClient_UpdateWatch` for partial update |
| `migrations/009_alerts_dismissed_at.sql` | Create | Phase 3: schema for dismiss + indexes |
| `internal/store/migrations/009_alerts_dismissed_at.sql` | Create | Phase 3: embedded copy |
| `pkg/types/alert.go` | Modify | Phase 3: `DismissedAt *time.Time` field |
| `internal/store/queries.go` | Modify | Phase 3: SELECT updates + 5 new queries |
| `internal/store/store.go` | Modify | Phase 3: 4 new interface methods |
| `internal/store/postgres.go` | Modify | Phase 3: implementations of new methods + query duration timing |
| `internal/store/postgres_test.go` | Modify | Phase 3: tests for the 4 new methods |
| `internal/store/mocks/store.go` | Generate | Phase 3: regenerated via `make mocks` |
| `mise.toml` | Modify | Phase 4: pin `templ` CLI |
| `scripts/makefiles/go.mk` | Modify | Phase 4: `make templ-generate` target + wire into `make build` |
| `internal/api/web/embed.go` | Create | Phase 4: `embed.FS` declarations |
| `internal/api/web/renderer.go` | Create | Phase 4: echo.Renderer adapter for templ |
| `internal/api/web/components/layout.templ` | Create | Phase 4: HTML shell with HTMX + Alpine |
| `internal/api/web/components/alerts_page.templ` | Create | Phase 4: list page |
| `internal/api/web/components/alerts_table.templ` | Create | Phase 4: table partial (HTMX swap target) |
| `internal/api/web/components/alert_row.templ` | Create | Phase 4: row partial (shared list/detail/swap) |
| `internal/api/web/components/alert_detail.templ` | Create | Phase 4: detail page |
| `internal/api/web/components/score_badge.templ` | Create | Phase 4: shared score-coloring component |
| `internal/api/web/components/notification_history.templ` | Create | Phase 4: history list |
| `internal/api/web/static/spt.css` | Create | Phase 4: minimal CSS |
| `internal/api/web/static/htmx.min.js` | Create | Phase 4: HTMX bundle (~14KB, version-pinned) |
| `internal/api/web/static/alpine.min.js` | Create | Phase 4: Alpine bundle (~15KB, version-pinned) |
| `internal/api/handlers/alerts_ui.go` | Create | Phase 4: 7 handlers + parseAlertsListQuery + retry path |
| `internal/api/handlers/alerts_ui_test.go` | Create | Phase 4: handler tests (incl. HTMX vs non-HTMX branch) |
| `cmd/server-price-tracker/cmd/serve.go` | Modify | Phase 4: register `/alerts` Echo routes gated by `web.enabled` |
| `internal/config/config.go` | Modify | Phases 4, 5, 6: `web.enabled`, `web.alerts_url_base`, `notify.discord.inter_chunk_delay`, `notify.discord.summary_only` |
| `configs/config.example.yaml` | Modify | Phases 4, 5, 6: example values for new keys |
| `configs/config.dev.yaml` | Modify | Phase 4: dev defaults for new keys |
| `internal/engine/alert.go` | Modify | Phase 5: partial-success contract; Phase 6: `BuildSummaryPayload` + branch |
| `internal/engine/alert_test.go` | Modify | Phases 5 + 6: signature update + summary-mode tests |
| `internal/metrics/metrics.go` | Modify | Phases 4 + 5: 8 new metrics |
| `tools/dashgen/...` | Modify | Phase 4: alert query latency + table rows panel |
| `charts/server-price-tracker/values.yaml` | Modify | Phase 4: `web.enabled`, `web.alertsUrlBase` toggles |
| `charts/server-price-tracker/templates/configmap.yaml` | Modify | Phase 4: render new keys into runtime config |
| `charts/server-price-tracker/tests/` | Modify | Phase 4: helm-unittest case for `web.enabled` |
| `docs/design/0008-*.md` | Modify | Phase 7: status → Implemented |
| `docs/design/0009-*.md` | Modify | Phase 7: status → Implemented |
| `docs/design/0010-*.md` | Modify | Phase 7: status → Implemented |
| `docs/impl/0015-*.md` | Modify | Phase 7: status → Completed |
| `docs/OPERATIONS.md` | Modify | Phases 2 + 6: new sections |
| `docs/USAGE.md` | Modify | Phase 2: reference `spt watches update` |
| `CLAUDE.md` | Modify | Phase 7: any new patterns worth recording |
| `.gitignore` | Modify | Phase 4: ignore `*_templ.go` (per resolved Q on commit-vs-gitignore) |

## Testing Plan

- [ ] Phase 1: `make test ./internal/notify/...` covers embed-limit
      table.
- [ ] Phase 2: `make test ./cmd/spt/... ./internal/api/client/...`
      covers `applyFilterUpdates` table and the partial-update
      round-trip.
- [ ] Phase 3: `make test ./internal/store/...` covers the 4 new
      methods + scan-order regression on existing alert reads +
      `undismissed` status enum.
- [ ] Phase 4: `make test ./internal/api/...` covers handler render
      smoke tests, `parseAlertsListQuery` table, HTMX vs non-HTMX
      branch, retry round-trip, `web.enabled = false` route absence,
      JSON mirror.
- [ ] Phase 4: `make helm-test` covers the new `web.enabled` toggle.
- [ ] Phase 5: chunking unit tests + 429 retry tests via
      `httptest.NewServer`; engine partial-failure test asserts
      `MarkAlertsNotified` slice.
- [ ] Phase 6: engine summary-mode tests cover empty / populated /
      send-failure branches.
- [ ] Full unit suite: `make test`.
- [ ] Lint: `make lint`, `make lint-md`, `make lint-yaml`.
- [ ] Mocks: `make mocks` clean (no diff after run).
- [ ] Manual smoke per Phase 7 runbook.

## Dependencies

- New Go dependencies:
  - `github.com/a-h/templ` (runtime). Tracked in `go.mod`.
  - `templ` CLI (build-time). Tracked in `mise.toml`.
- HTMX (~14KB) and Alpine (~15KB) static JS bundles committed under
  `internal/api/web/static/`. Version-pinned in commit messages /
  file headers.
- Migration `009_alerts_dismissed_at.sql` is additive (NULL column +
  partial index + composite index) — no lock concerns at current
  `alerts` table size.
- Helm chart: `web.enabled` and `web.alertsUrlBase` toggles in
  `values.yaml`, plus configmap wiring. The migration init container
  already runs migrations on chart upgrade. CI owns appVersion /
  version bumps so no chart bump in this IMPL.
- DESIGN-0009 Phase 3 (persisted bucket state) stays deferred —
  depends on multi-replica deployment which is not on the roadmap.
- The notifier interface change is internal (only consumed by the
  engine and the new retry handler) so no external API consumers
  break.

## Open Questions

All open questions resolved during planning. Recorded here for future
reference.

1. ~~**Phase ordering — split Phase 1 into a hot-fix PR?**~~
   **Resolved: (a) same branch.** Phase 1 lands as the first commit
   on the existing `fix/post-score-alert-evaluation` branch; the
   rest follow on the same branch. Reviewer approves incrementally.
   No separate hot-fix PR needed.

2. ~~**Notifier interface change — break or wrap?**~~
   **Resolved: (a) break.** `SendBatchAlert` signature changes
   directly to `(sent int, err error)`. Internal interface, only two
   real implementations + the generated mock; carrying a deprecated
   method to avoid the diff isn't worth it.

3. ~~**`alerts_url_base` config — required, optional, or auto-derive?**~~
   **Resolved: (b) optional.** Config is empty by default; when set,
   used for both the list-page link in summary embeds AND any
   per-alert deep links. Auto-derive from `Host` header was rejected
   as fragile (request-order dependent, header trust behind proxies).

   Sub-decision: **scope expansion to per-alert detail page.** New
   endpoint `GET /alerts/{id}` renders full listing card + score
   breakdown + watch info + notification history + action buttons.
   New endpoint `POST /alerts/{id}/retry` re-sends via the
   single-alert Discord path, bypasses
   `HasSuccessfulNotification`, always sends the rich embed
   regardless of `summary_only`, leaves `notified_at` /
   `dismissed_at` unchanged. No `triggered_by` audit column on
   `notification_attempts` — row-insertion order + timestamps are
   sufficient. Lands in Phase 4 alongside the list page.

   Sub-decision: **stack — templ + HTMX + Alpine + `web.enabled`
   feature flag.** Compile-time-checked components via templ;
   server-side-driven interactivity via HTMX (search debounce,
   dismiss/retry swap-in-place, `hx-boost` form for graceful no-JS
   fallback); Alpine for small reactive bits (select-all, shift-
   click, "N selected" counter). `web.enabled` toggle in binary
   config + Helm values gates the entire `/alerts` route group. Bun
   /React SPA was considered and deferred — reconsider if the UI
   grows past list + detail + a small set of write actions (retry,
   rescan, re-alert, clear).

4. ~~**Page authentication.**~~ **Resolved: (a) punt.** Document
   the gap in `docs/OPERATIONS.md` and recommend a basic-auth
   sidecar (Cilium HTTPRoute filter or oauth2-proxy) for
   production. Matches existing `/docs` and `/metrics` posture.
   Revisit if `/alerts` becomes publicly reachable or operator
   feedback says otherwise.

5. ~~**Search input — full reload or HTMX swap?**~~
   **Resolved: (b) HTMX-driven search.**
   `hx-get="/alerts" hx-trigger="keyup changed delay:300ms"
   hx-target="#alerts-table" hx-swap="innerHTML"
   hx-push-url="true"`. Handler inspects `HX-Request` header and
   returns either the full page or just the table partial. Form
   POST still works as no-JS fallback.

6. ~~**Bulk dismiss UI affordance.**~~ **Resolved: (c) hx-boost
   form + Alpine.** `<form hx-boost="true" action="/alerts/dismiss"
   method="post">` with checkboxes; HTMX intercepts the submit and
   server returns the updated table partial. Falls back to plain
   form POST + redirect when HTMX is unavailable. Alpine handles
   select-all checkbox state, shift-click range selection, and
   "N selected" counter (~15 lines of `x-data`).

7. ~~**Status enum — add `undismissed` value or compute it?**~~
   **Resolved: (a) explicit enum value.** `parseAlertsListQuery`
   accepts `undismissed` as a real value alongside `active`,
   `dismissed`, `notified`, `all`. URL stays honest regardless of
   summary-mode configuration; sharing `?status=undismissed` shows
   the same data on every server.

8. ~~**`InsertNotificationAttempt` per-alert — N inserts or
   batched?**~~ **Resolved: (a) per-alert inserts.** At our scale
   (30-50 extractions/day, scheduler ticks every 30 min), even a
   2k-alert worst case is ~2s of background INSERTs — Postgres
   handles 1000+/sec easily. Add
   `spt_notification_attempts_inserted_total{result}` counter so
   we can monitor volume in PromQL and know when to refactor.
   Batched insert refactor tracked under Follow-up Work with
   trigger criteria.

9. ~~**Migration 009 indexes — both, or just `dismissed_at`?**~~
   **Resolved: (a) both.** Index cost at thousands of rows is
   negligible (a few MB disk, microseconds per insert). The
   composite `(score DESC, created_at DESC)` index supports the
   default sort that every page load uses. Migration is additive;
   pre-paying the cost now avoids a follow-up migration when row
   count grows.

   Sub-decision: **app-side query observability.** Add
   `spt_alerts_query_duration_seconds{query}` histogram and
   `spt_alerts_table_rows` gauge in Phase 4; surface them in a
   dashgen panel on the existing Grafana dashboard. CNPG-side
   `pg_stat_statements` exposition is its own design — tracked
   under Follow-up Work.

10. ~~**Phase 5 inter-chunk delay default.**~~ **Resolved: (a)
    wire the config field now, default `0s`.** Header-driven waits
    are the protocol-correct path, but exposing the knob lets
    operators throttle defensively (e.g., to make a Discord
    channel stream over 30 seconds for human-readability) without
    a code change. ~5 lines plumbing.

## Follow-up Work

These items were considered during open-question review and deferred
so this IMPL stays scoped. Tracked here so we don't lose them.

- **Batched `InsertNotificationAttempts` (DESIGN + IMPL).** Replace
  the per-alert insert pattern with
  `InsertNotificationAttempts(ctx, attempts []NotificationAttempt) error`
  using a single multi-row INSERT per chunk. Trigger criteria:
  - Sustained per-tick alert volume above ~500 (would mean watch
    filters need tightening first; if both are needed, the watch fix
    comes first).
  - Multi-replica deployment putting pressure on the connection pool.
  - Per-attempt retry logic that further multiplies the row count.
  - PromQL rate of `spt_notification_attempts_inserted_total`
    sustained above ~10/sec for an hour or more.

- **CNPG `pg_stat_statements` exposition (DESIGN + IMPL).** Enable
  `pg_stat_statements` in the CNPG `Cluster` CR
  (`postgresql.parameters`), surface query stats through
  `monitoring.customQueriesConfigMap` to Prometheus, add a Grafana
  dashboard for query-level latency and call counts. Trigger
  criteria:
  - `spt_alerts_query_duration_seconds` p95 above 500ms for the list
    query, or above 200ms for dismiss/restore.
  - Investigation needed for a slow-query alert that the app-side
    histograms can't fully diagnose.
  - Migration to a multi-tenant Postgres instance where query-level
    visibility is needed for noisy-neighbor analysis.

- **Page authentication.** Revisit Q4. Trigger criteria:
  - `/alerts` becomes publicly reachable without a fronting sidecar
    (no HTTPRoute filter, no oauth2-proxy).
  - Operator feedback or actual abuse incident.
  - Multi-operator deployment where action attribution matters.
  Likely lightweight option when needed: optional
  `web.basic_auth` config (`user:bcrypt(pass)`) gating the route
  group via Echo middleware. ~15 lines.

- **Discord persisted bucket state (DESIGN-0009 Phase 3).** When we
  run multiple notifier replicas, in-process rate-limit state stops
  working. Either persist bucket state in a `discord_rate_limits`
  table or move the notifier behind the existing scheduler leader
  lock. Trigger: Helm `replicaCount > 1`.

- **Bun/React SPA frontend.** Reconsider the templ stack if the UI
  grows past:
  - Read-list (alerts, listings, watches, jobs)
  - Per-resource detail pages
  - The small set of write actions in this IMPL (retry, dismiss,
    restore) plus 1-2 more (e.g., rescan, re-extract, manual
    re-alert, clear-all)
  Beyond that, the type-checked-components-and-htmx-swaps
  ergonomics start to plateau and a real SPA becomes worth the
  build pipeline investment.

- **`pg_trgm` GIN index on `listings.title`.** Add when ILIKE search
  becomes a usability complaint (current expectation: not before
  ~100K listings). Includes adding the extension to CNPG cluster
  spec and the migration. Migration is small (one index); the cost
  is operational (one more extension to manage).

## References

- [DESIGN-0008: Add spt watches update CLI command](../design/0008-add-spt-watches-update-cli-command.md)
- [DESIGN-0009: Discord notifier rate limiting and embed chunking](../design/0009-discord-notifier-rate-limiting-and-embed-chunking.md)
- [DESIGN-0010: Alert review UI with pagination and search](../design/0010-alert-review-ui-with-pagination-and-search.md)
- Triggering incident: PR #44 (`fix/post-score-alert-evaluation`)
  rescore on 2026-04-25 produced 2,129 pending alerts, all of which
  failed Discord delivery with `400 Must be 10 or fewer in length`.
- Bug surface: `internal/notify/discord.go:82-107`
  (`SendBatchAlert`).
- Server endpoint reused by Phase 2:
  `internal/api/handlers/watches.go:156` (`UpdateWatch`).
- Filter parser reused by Phase 2:
  `internal/api/handlers/filters.go:23` (`ParseFilters`).
- HTTP client method reused by Phase 2:
  `internal/api/client/watches.go:58` (`UpdateWatch`).
- Engine batch path modified by Phases 5/6:
  `internal/engine/alert.go:127-180` (`sendBatch`,
  `MarkAlertsNotified`).
- Discord rate-limits external reference:
  <https://discord.com/developers/docs/topics/rate-limits>
- templ docs: <https://templ.guide/>
- HTMX docs: <https://htmx.org/docs/>
- Alpine docs: <https://alpinejs.dev/>
- Prior IMPL formatting reference:
  [IMPL-0014: LLM Token Metrics](0014-llm-token-metrics.md).
- Migrations directory: `migrations/` (next free version is `009`).
