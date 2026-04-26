---
id: DESIGN-0010
title: "Alert review UI with pagination and search"
status: Draft
author: Donald Gifford
created: 2026-04-26
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0010: Alert review UI with pagination and search

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-04-26

<!--toc:start-->
- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
- [Detailed Design](#detailed-design)
  - [Page layout](#page-layout)
  - [Endpoints](#endpoints)
  - [Discord summary mode](#discord-summary-mode)
  - [Implementation notes](#implementation-notes)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Add a server-rendered HTML page at `/alerts` that lists alerts in a
paginated table with simple full-text search across listing titles.
Make Discord the *summary* surface — one embed per tick saying
"N new alerts since T, see <link>" — and treat the dashboard as the
primary place to review and dismiss alerts. This eliminates the
"2,000+ pending alerts blast Discord" failure mode at the source
instead of relying solely on the chunking work in DESIGN-0009.

## Goals and Non-Goals

### Goals

- Operator can open a single URL (`https://spt.fartlab.dev/alerts`)
  and see every undismissed alert in a sortable, paginated table.
- Per-alert detail page at `/alerts/{id}` showing the full listing
  info, score breakdown, watch info, and notification history — the
  triage surface for "why did this alert fire and what do I do with
  it?"
- Search box that filters by listing title (PostgreSQL `ILIKE` against
  `listings.title`). Good enough for "find that 32GB ECC alert" —
  ranked search is out of scope.
- Filter chips: component type, watch name, score range,
  dismissed/active.
- Per-row actions: open eBay listing (link), dismiss alert (POST,
  hides from default view), view detail page.
- Per-alert detail-page actions: **retry Discord** (re-send the rich
  embed bypassing idempotency), dismiss, restore.
- Bulk dismiss: select N rows + dismiss button → mark notified +
  dismissed in one round-trip.
- Discord summary embed gains a hyperlink to the page so an alerted
  operator can pivot to triage in one click.
- Hide unsorted, unfiltered noise (PR #44 surfaced ~2,100 alerts —
  the page must be usable with that volume).
- Whole `/alerts` route group is gated by a `web.enabled` config flag
  (default true) so paranoid prod deploys can disable the UI without
  recompiling.

### Non-Goals

- Multi-user auth / RBAC. Single-tenant deployment, Cilium API
  Gateway already gates ingress. The page is unauthenticated for now
  (matches `/docs`, `/metrics` posture in this deployment).
- A SPA or component framework. `html/template` + minimal CSS, no
  JS framework, fetch-only progressive enhancement at most.
- Replacing Grafana for ops metrics — `/alerts` is the *workflow*
  surface (act on a deal), Grafana stays the *health* surface
  (notification success rate, scheduler lag).
- A general "admin UI" for watches, listings, jobs. Out of scope;
  this design is alert-shaped only.
- Re-architecting alert delivery into a queue. Same `ProcessAlerts`
  scheduler tick; the dashboard is read-side, alert state mutations
  are write-side via a small set of POST endpoints.

## Background

PR #44 wired alert evaluation into the rescore path. The first rescore
created ~2,100 pending alerts (most from RAM watches with empty
`attribute_filters` matching every capacity). Even after fixing the
filters, ~140 legitimate alerts remained per tick — too many for
Discord to be a useful queue. The notifier blast also hit Discord's
rate limits and embed cap (DESIGN-0009).

Today the only ways to inspect alerts are:

1. SQL: `SELECT * FROM alerts WHERE notified_at IS NULL ORDER BY
   score DESC LIMIT 50;` — requires DB shell, unreadable for triage.
2. Discord webhook — fire-and-forget, no history once a message
   scrolls off, and the channel becomes unusable above ~50 messages
   per day.
3. `spt alerts list` (if it existed; it doesn't yet) — would still be
   text-only.

The alert review UI fills the gap. It also makes Discord's job much
smaller: one summary embed per tick instead of `ceil(N/10)` chunks.

## Detailed Design

### Page layout

```
┌─────────────────────────────────────────────────────────────────────┐
│ SPT Alerts                                          [Settings] [↗] │
├─────────────────────────────────────────────────────────────────────┤
│ Search: [_____________ ] [Search] [Clear]                           │
│ Type: [all ▾]  Watch: [all ▾]  Score: [≥ 75]  Status: [active ▾]    │
├─────────────────────────────────────────────────────────────────────┤
│ ☐ │ Score │ Title                          │ Watch       │ Created  │
│ ☐ │  92   │ DDR4 ECC REG 32GB 2666 1Rx4    │ DDR4 32G    │ 2h ago   │
│ ☐ │  89   │ Lot of 4 Samsung 32GB DDR4 ECC │ DDR4 32G    │ 2h ago   │
│ ☐ │  87   │ Intel S2600WT 2U Server        │ Servers     │ 4h ago   │
│ ...                                                                 │
│ [Dismiss selected]   [< Prev]  Page 1 of 14  [Next >]               │
└─────────────────────────────────────────────────────────────────────┘
```

- Default view: `notified_at IS NULL OR dismissed_at IS NULL`,
  score ≥ 75, sorted by score DESC, 25 per page.
- Toggling Status to "all" includes notified+dismissed for audit.
- Row click opens the eBay listing in a new tab; the title is the
  hyperlink, the score column is a colored badge matching Discord
  embed colors (green ≥90, yellow 80–89, orange 75–79).
- The thumbnail image (if present on `listings.image_url`) renders
  in a tooltip / expandable detail row — not in the default row to
  keep density high.

### Endpoints

| Method | Path                          | Purpose                                       |
|--------|-------------------------------|-----------------------------------------------|
| `GET`  | `/alerts`                     | HTML page (paginated table)                   |
| `GET`  | `/alerts.json`                | Same data as JSON (debugging, scripts)        |
| `GET`  | `/alerts/{id}`                | HTML detail page for a single alert           |
| `POST` | `/alerts/{id}/dismiss`        | Mark single alert dismissed                   |
| `POST` | `/alerts/dismiss`             | Body: `{ids: [...]}` — bulk dismiss           |
| `POST` | `/alerts/{id}/restore`        | Undismiss (clears `dismissed_at`)             |
| `POST` | `/alerts/{id}/retry`          | Re-send via Discord (rich embed, bypasses idempotency) |

The detail page renders:

- Full listing card: title, image, price, unit price, eBay URL, seller,
  condition, component type, attribute extraction.
- Score breakdown: each factor (price 40%, seller 20%, condition 15%,
  quantity 10%, quality 10%, time 5%) with its computed value.
- Watch info: name, threshold, filter snapshot at the time the alert
  fired.
- Notification history: rows from `notification_attempts` for this
  alert (timestamp, succeeded, http status, error text if any).
- Action buttons: **Retry Discord**, **Dismiss** (or **Restore** if
  already dismissed), **Open eBay listing** (external link).

The retry endpoint:

- Calls the existing single-alert `Notifier.SendAlert` path (not the
  batched chunked path).
- Always sends the rich per-alert embed regardless of `summary_only`
  config — manual retry is the user explicitly asking for the full
  detail in Discord.
- Bypasses the `HasSuccessfulNotification` idempotency guard (the
  whole point of "retry").
- Records a `notification_attempts` row regardless of outcome.
- Does NOT change `notified_at` or `dismissed_at` — those are
  separate operator actions.
- Returns the updated detail-page partial (HTMX swap-in-place) so the
  notification history list reflects the new attempt without a
  reload.

Query parameters for `GET /alerts` and `/alerts.json`:

- `q` — substring (used as `ILIKE '%' || q || '%'` against title)
- `type` — `ram | drive | server | cpu | nic | other`
- `watch` — watch ID (slug or ID, exact match)
- `min_score` — int, default 75
- `status` — `active` (default) | `dismissed` | `notified` | `all`
- `page` — 1-indexed, default 1
- `per_page` — default 25, max 100
- `sort` — `score | created | watch`, default `score` (always DESC)

The `/alerts.json` mirror is intentional: lets us script bulk
operations and gives the existing OpenAPI consumers a programmatic
view without inventing a separate API tier.

### Discord summary mode

A new config switch `notify.discord.summary_only` (default `false`,
flip to `true` once the page ships):

- When `true`, `ProcessAlerts` aggregates all pending alerts in a
  tick and sends *one* Discord embed:

  ```text
  Title: 142 new alerts (top score 94)
  Description: Highest: "DDR4 ECC REG 32GB 2666 1Rx4" — 94/100
  Field: By type — ram: 88, drive: 31, server: 23
  URL: https://spt.fartlab.dev/alerts
  ```

  Every alert is `MarkAlertNotified`'d in the same transaction so
  they don't re-trigger next tick. The page surfaces them via the
  default `active` filter (`notified_at IS NULL OR dismissed_at IS
  NULL` becomes `notified_at IS NOT NULL AND dismissed_at IS NULL`
  in summary mode — see Open Question 2).

- When `false`, current per-watch batch behavior (with DESIGN-0009
  fixes) continues. Useful for low-volume installs that prefer
  Discord-as-feed.

### Stack

The UI uses **templ + HTMX + Alpine** rather than `html/template` or a
separate React/Bun SPA. Rationale:

- **templ** for compile-time-checked components. The dismiss/retry
  HTMX swap pattern returns the same row partial that the list-page
  renders — templ guarantees they share data shape at compile time
  rather than at request time. Score breakdown (nested struct) and
  notification history (slice of structs) render with type-safe field
  references and IDE autocomplete instead of `interface{}`-via-reflection.
- **HTMX** for server-side-driven interactivity. Search uses
  `hx-get="/alerts" hx-trigger="keyup changed delay:300ms"
  hx-target="#alerts-table" hx-push-url="true"`. Dismiss/retry/restore
  use `hx-post` with a row partial swap-in-place. Forms get
  `hx-boost="true"` for graceful no-JS fallback.
- **Alpine** for small reactive bits (select-all checkbox state,
  shift-click range select, "N selected" counter on the bulk-dismiss
  control). Not a framework; ~15 lines of inline `x-data` per page.

A separate Bun/React SPA was considered and deferred. For an internal
admin tool with a single operator, the costs (separate build pipeline,
second deployable, OpenAPI client generation step, CORS handling, npm
toolchain in CI) outweigh the benefits. The templ stack scales
comfortably to a top-10 dashboard, watch editor, and similar
extensions; if the UI grows past read-list + detail + a small set of
write actions (retry, rescan, re-alert, clear), revisit the SPA option.

### Implementation notes

- **Templates.** `*.templ` files in `internal/api/web/`. Generated
  Go via `templ generate` (CLI pinned in `mise.toml`, run via `make
  templ-generate`). Embedded indirectly — the generated `*_templ.go`
  compiles into the binary, so the server still ships single-file.
  Components: `Layout`, `AlertsPage`, `AlertsTable`, `AlertRow`
  (shared between page render and HTMX swap), `AlertDetailPage`,
  `RetryButton`, `DismissButton`, `ScoreBadge`, `NotificationHistory`.
- **CSS.** One static file `static/spt.css` ~5KB, served via Echo's
  `e.Static` from an `embed.FS`. No CSS framework. Variables for the
  score colors so they match Discord embeds exactly.
- **JS.** HTMX (~14KB minified) and Alpine (~15KB minified) served
  from the same embed. No framework, no build step, no npm.
- **Feature flag.** `web.enabled` (bool, default true). When false,
  the entire `/alerts` route group is not registered — `/alerts`
  returns 404. Helm chart exposes the same toggle under `web.enabled`
  in `values.yaml`.
- **Pagination.** `LIMIT $1 OFFSET $2` keyset is overkill for
  expected volumes (≤10K alerts). Add `created_at DESC, id DESC`
  tiebreakers in the ORDER BY so OFFSET stays stable across pages
  even if rows arrive between requests.
- **Search.** `WHERE listings.title ILIKE '%' || $1 || '%'`. With
  ~10K listings and a B-tree on `listings.title` this is a seq
  scan but well under 50ms. Add a `pg_trgm` GIN index in a follow-up
  if it gets slow.
- **Dismiss semantics.** `dismissed_at TIMESTAMPTZ NULL`. Dismiss
  sets `now()`; restore sets NULL. Independent of `notified_at`
  (an alert can be both notified and dismissed; the operator dismissed
  it after seeing the Discord embed).
- **No new auth.** Page lives behind the same Cilium HTTPRoute as
  the API. Consistency with `/docs` and `/metrics`. Document this in
  OPERATIONS.md so deployers know to put a basic auth proxy in front
  if they want one.

## API / Interface Changes

- **HTTP API:** new endpoints listed above. They are deliberately
  outside `/api/v1/*` because they serve HTML and operate on a
  view-model, not the canonical JSON API. The `.json` mirror is the
  exception — same path tree, content-type negotiated by suffix.
- **Echo routing:** the existing Echo server gets a new route group
  rooted at `/alerts` with both HTML handlers and a JSON handler.
  Templates and static assets registered via `e.Renderer` and
  `e.Static`.
- **Notifier:** no interface change. `Notifier.SendAlert` and
  `SendBatchAlert` continue to exist; summary-mode is a new internal
  branch in `engine.ProcessAlerts` that calls `n.SendAlert` with a
  single synthesized `AlertPayload` representing the rollup.
- **Config:** `notify.discord.summary_only` (bool, default false).
  `web.alerts_url_base` (string, default empty — when set, included
  in Discord embed URLs and in the page's "share link" header).

## Data Model

Migration `009_add_alerts_dismissed_at.sql`:

```sql
ALTER TABLE alerts
    ADD COLUMN dismissed_at TIMESTAMPTZ NULL;

CREATE INDEX idx_alerts_dismissed_at ON alerts(dismissed_at)
    WHERE dismissed_at IS NULL;

CREATE INDEX idx_alerts_score_created ON alerts(score DESC, created_at DESC);
```

- `dismissed_at` lets us hide alerts without losing them (audit
  trail). The partial index is small (only undismissed rows).
- The composite index supports the default sort. The existing PK +
  watch FK indexes already cover most other queries.

No changes to `listings`. Search joins `alerts → listings ON
alerts.listing_id = listings.id` and uses `listings.title`. If
`listings.title` lookups become slow at scale, follow up with:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX idx_listings_title_trgm ON listings USING gin (title gin_trgm_ops);
```

Held back from the initial migration to avoid pulling in the
extension dependency until it's needed.

`MEMORY.md` reminder: scan functions must match SELECT order. Adding
`dismissed_at` to alert SELECTs requires updating
`scanAlert`/`scanAlertRow` and the domain `Alert` struct in
`pkg/types`.

## Testing Strategy

- **Unit (handler):**
  - Table-driven for `parseAlertsListQuery` (defaults, max
    `per_page` clamp, invalid sort fallback, invalid `min_score`
    fallback).
  - HTML template smoke: render with 0 / 1 / 25 / 100 alerts;
    assert `<table>` row count and that pagination links carry
    query params forward.
- **Unit (store):**
  - `ListAlertsForReview(ctx, AlertReviewQuery)` covers each filter
    permutation against an in-memory test schema (we already have a
    `pgtestdb` setup for migration tests). Assert `LIMIT/OFFSET`
    correctness and `ILIKE` substring match.
  - `DismissAlerts(ctx, ids)` and `RestoreAlerts(ctx, ids)` round
    trip.
- **Unit (engine):**
  - New test for summary-mode `ProcessAlerts`: mock notifier
    receives a single `SendAlert` call with rollup payload; mock
    store is asked to mark all input alert IDs notified.
  - Confirm summary mode does **not** re-notify alerts that were
    already notified (idempotency boundary unchanged).
- **Integration:**
  - One end-to-end `httptest`-backed test: insert 30 alerts, GET
    `/alerts?per_page=10`, assert pagination metadata and that
    `dismiss` POST removes the row from the next GET.
- **Manual:** open the page on local dev with the seed data from
  the rescore-incident dump; visually confirm density, scroll
  performance, and that the dismiss action feels instant.

## Migration / Rollout Plan

Two-step, both behind small flags so we can roll back independently:

1. **Ship the page** with summary mode `false` by default. Existing
   Discord behavior unchanged. Operators can navigate to `/alerts`
   to triage — useful even before flipping summary mode.
2. **Flip summary mode** to `true` in `configs/config.dev.yaml`,
   exercise for a week, then change the prod default. Provides a
   clean rollback (config flip) if Discord becomes too quiet for
   anyone's taste.

Migration 009 is additive (NULL column, new indexes). Safe to apply
on a populated `alerts` table without locking concerns at our
volume.

Doc updates: `docs/OPERATIONS.md` gains an "Alert Review UI"
section; README screenshot; `docs/USAGE.md` mentions the page in
the alert workflow.

## Open Questions

1. **Auth.** Truly nothing in front of `/alerts`? `/docs` and
   `/metrics` are open, so consistent — but `/alerts` exposes
   listing titles + watch names + scores. Not secret, but also not
   public-facing content. Defer auth to a deployment-layer concern
   (basic-auth sidecar) and document it; revisit if the project
   ever ships a multi-tenant story.
2. **What does "active" mean in summary mode?** When the notifier
   marks every alert notified each tick, the default `active`
   filter (`notified_at IS NULL`) goes empty and the page looks
   useless. Proposed fix: in summary mode, default filter is
   `dismissed_at IS NULL` (notification status is not the signal
   anymore — the page is the queue). Document the semantic shift
   and surface it in the UI ("Showing undismissed").
3. **Alert TTL / archival.** Right now alerts persist forever.
   With the page as the primary surface, do we age out alerts
   older than N days into a separate `alerts_archive` table to
   keep the default view fast? Probably premature — defer until
   `LIMIT 25 OFFSET 0` queries cross 100ms.
4. **Coordination with DESIGN-0009.** If we go straight to summary
   mode, do we still need the chunking work? Yes — the summary
   embed itself is one POST, but `SendAlert` still needs the
   rate-limit-aware `post()` from DESIGN-0009 because every
   notifier path shares it. Phase 1 of DESIGN-0009 (off-by-one)
   is technically unnecessary in summary mode, but ship it anyway
   so per-watch batching remains correct for installs that don't
   flip the summary flag.
5. **Search ranking.** `ILIKE '%q%'` is the dumbest thing that
   could work and is fine for 10K rows. If we want phrase
   matching ("samsung 32gb ddr4") move to `tsvector` /
   `plainto_tsquery`. Out of scope for v1; revisit when search
   becomes a usability complaint, not a theoretical one.

## References

- Triggering incident: PR #44 (`fix/post-score-alert-evaluation`)
  rescore at 2026-04-25 produced ~2,100 alerts; Discord became
  unusable as the alert surface.
- Companion design: DESIGN-0009 (Discord notifier rate limiting +
  chunking) — fixes the *delivery* path; DESIGN-0010 fixes the
  *triage* surface. Either can ship first; together they remove
  the "Discord blast" failure mode entirely.
- Schema: `migrations/` (next free version is `009`).
- Existing query patterns: `internal/store/queries.go` (raw SQL,
  no ORM).
- Existing handler/route patterns: `internal/api/handlers/` (Huma
  v2). Note: alert-review handlers are *not* Huma — they serve
  HTML — so they register against the underlying Echo instance,
  not the Huma API.
- PostgreSQL `pg_trgm` reference (held for follow-up):
  <https://www.postgresql.org/docs/current/pgtrgm.html>
