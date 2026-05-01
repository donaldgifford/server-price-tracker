---
id: DESIGN-0014
title: "SPA frontend with Bun, React, shadcn/ui, and Meilisearch"
status: Draft
author: Donald Gifford
created: 2026-05-01
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0014: SPA frontend with Bun, React, shadcn/ui, and Meilisearch

**Status:** Draft
**Author:** Donald Gifford
**Date:** 2026-05-01

<!--toc:start-->
- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
- [Background](#background)
- [Architecture](#architecture)
- [Frontend Stack](#frontend-stack)
- [Backend Changes](#backend-changes)
- [Meilisearch Integration](#meilisearch-integration)
- [Authentication](#authentication)
- [Deployment Topology](#deployment-topology)
- [Migration Plan](#migration-plan)
- [Testing Strategy](#testing-strategy)
- [Open Questions](#open-questions)
- [References](#references)
<!--toc:end-->

## Overview

Replace the current embedded `templ`+HTMX+Alpine UI at `/alerts` with a
standalone SPA built on Bun + React + TypeScript + shadcn/ui, deployed
as a separate container in the existing Helm chart. Add Meilisearch as
an indexing layer (Postgres remains the source of truth) so the UI
gets fast typeahead and faceted filtering. Wrap the API in OIDC
authentication via Keycloak so the SPA can be exposed publicly.

This is a foundational refactor that decouples UI from the Go monolith,
opens the door to data/ML surfaces that Grafana can't render well, and
gives operators a richer triage workflow than the current alert review
page.

## Goals and Non-Goals

### Goals

- **Replace** the existing `/alerts` UI. Templ components, embedded
  HTMX/Alpine assets, and the route handler are removed once feature
  parity is reached.
- **Operational dashboard** — top-N alerts per ComponentType filterable
  by time window (6/12/24h, custom), pulling current state of work that
  Discord channels alone don't make searchable.
- **General search** across listings via Meilisearch — fast typeahead,
  field-level filters (price range, component, score, condition, etc.).
- **Watch management UI** — create/edit/disable/delete watches without
  shelling out to `spt`.
- **Alert review** — same surface the current `/alerts` provides
  (review, dismiss, mark notified) but with better filters and bulk
  actions.
- **Auth via Keycloak OIDC** — SPA uses PKCE; API verifies JWTs against
  JWKS; CLI continues to work via service-account or bearer-token.
- **Helm-native deployment** — new SPA container ships in the existing
  `charts/server-price-tracker/` chart, gated by a top-level toggle.
- **API-first** — every SPA capability is reachable via `/api/v1/*`;
  nothing is SPA-only.

### Non-Goals

- **No new business logic.** Scoring, baselines, ingestion, extraction,
  Discord all stay where they are. SPA is a presentation/control plane.
- **No mobile app.** Responsive web is fine; React Native is out of
  scope.
- **No multi-tenancy.** Single Keycloak realm, role-based authz. Not
  designing for organisations / projects / scoped data.
- **No replacing Grafana.** Grafana stays for SRE/ops dashboards. The
  SPA absorbs only product-level views (alerts, listings, watches,
  search) where Grafana fits poorly.
- **No SSR.** Pure client-rendered SPA. Bun builds static assets;
  Go-side serves them (or a separate static container does — see
  Deployment).
- **No real-time push** for v1. SWR polling is sufficient. WebSockets/
  SSE are a future enhancement once a use case demands them.

## Background

The current UI surface is described in DESIGN-0010 / IMPL-0015 Phase 6:
templ components in `internal/api/web/components/`, HTMX 1.9 and
Alpine.js 3.14 served via `go:embed`, gated by `config.web.enabled`. It
works but has hit the ceiling for what it can express:

- Listings/alerts queries hit Postgres `LIKE` and JSONB filters; UX is
  slow once filtering compounds.
- Adding a "top-10 over 6h" view requires hand-rolling templ + an
  endpoint per shape.
- No watch CRUD UI — operators run `spt watches create/update/delete`.
- No way to do exploratory data work without exporting to CSV.
- Auth is "trust the network" (no public exposure today).

Discord channel routing (DESIGN-0013) helps short-term triage. The SPA
is the longer arc.

## Architecture

```
                          ┌──────────────────────┐
                          │   Keycloak (OIDC)    │
                          └──────┬───────────────┘
                                 │  PKCE / JWKS
                                 ▼
        ┌──────────────────────────────────────────────────┐
        │   spa container  (Bun-built static + nginx)      │
        │   served at: spt.fartlab.dev/                    │
        │   talks to: /api/v1/* and /search/*              │
        └──────┬─────────────────────────────────┬─────────┘
               │ JWT bearer                      │ JWT bearer
               ▼                                 ▼
        ┌──────────────────┐             ┌──────────────────┐
        │  api container   │             │  Meilisearch     │
        │  (Go monolith)   │── indexes ─▶│  (managed K8s    │
        │  /api/v1/*       │             │   StatefulSet)   │
        └──────┬───────────┘             └──────────────────┘
               │
               ▼
        ┌──────────────────┐
        │  PostgreSQL      │  ◀── source of truth
        │  (CNPG)          │
        └──────────────────┘
```

Two containers, one deploy: the existing `server-price-tracker`
(Go API + scheduler + extractor) and a new `spa` container serving
the built React bundle. Both routed through the existing Cilium
Gateway / HTTPRoute. Meilisearch is a third workload (StatefulSet)
indexed asynchronously by the API.

## Frontend Stack

### Build toolchain

- **Bun** as the package manager and bundler. Faster install, native
  TypeScript, easier dev loop than Node + Vite.
- **React 19** + TypeScript (strict mode).
- **shadcn/ui** as the component library — copies code into the repo
  rather than importing a black box, easier to customise. Tailwind v4
  for styling.
- **TanStack Router** for typed routing.
- **TanStack Query** for server state (caching, background refetch,
  optimistic updates on watch CRUD).
- **react-hook-form + zod** for forms (watch create/edit) with
  validation that mirrors the OpenAPI spec.
- **MeiliSearch JS client** for search/typeahead.
- **oidc-client-ts** for Keycloak PKCE.

### Top-level routes

```
/                       Dashboard — top-N per type, time-window filter
/listings               Listing search (Meili-powered)
/listings/:id           Listing detail (full JSONB attrs, score breakdown, raw eBay link)
/alerts                 Alert review (replaces current /alerts)
/watches                Watch list + bulk enable/disable
/watches/new            Watch creation form
/watches/:id            Watch detail / edit
/settings               User settings (token rotation, Discord channel test)
/login                  OIDC redirect handler
```

### Dashboard view (the "top-N over time window")

Single-screen panel layout, one card per ComponentType. Each card:

- Time window selector at top (6h / 12h / 24h / custom).
- Top 10 alerts in window, sorted by score desc.
- Click → listing detail. Bulk-dismiss button at card footer.
- Empty state when no alerts in window for that type.

Backed by `/api/v1/alerts/top?component_type=ram&since=PT6H&limit=10`
(new endpoint — see Backend Changes).

### Project layout

```
web/                          (new top-level directory in repo)
├── package.json              # bun
├── tsconfig.json
├── tailwind.config.ts
├── vite.config.ts            # bun-vite bridge for dev
├── index.html
├── src/
│   ├── main.tsx
│   ├── routes/
│   ├── components/
│   │   └── ui/               # shadcn copy-paste targets
│   ├── api/                  # generated from OpenAPI
│   ├── search/               # Meili client wrapper
│   ├── auth/                 # OIDC PKCE
│   └── lib/
└── Dockerfile                # multi-stage: bun build → nginx static
```

### Generated API client

Use **openapi-typescript-codegen** (or `openapi-fetch`) against the
existing `/openapi.json` Huma spec. Generated client + types live in
`web/src/api/generated/`, regenerated by a Makefile target:

```bash
make web-gen   # bun run openapi-typescript ../openapi.json -o src/api/types.ts
```

CI fails if generated client is out of date, mirroring the dashgen
staleness check.

## Backend Changes

The API is API-first today, but the SPA exposes gaps. New endpoints
and modifications:

### New endpoints

- `GET /api/v1/alerts/top?component_type=&since=&limit=` — top-N alerts
  in a time window. Server-side sort/limit, JSONB attrs included.
- `GET /api/v1/search/listings?q=&filter=&facets=` — pass-through to
  Meilisearch with auth + index name resolution server-side.
- `GET /api/v1/search/alerts?q=&filter=` — same shape, alert index.
- `GET /api/v1/dashboard/summary?since=` — counts per type, ratio of
  alerts:unique_keys, recent ingestion stats. Powers the dashboard
  header.
- `GET /api/v1/me` — current user (claims, roles) for the SPA topbar.

### Modifications

- All `/api/v1/*` routes become auth-required (see Authentication).
  CLI is updated to send a bearer token from a config file or
  environment variable.
- `GET /api/v1/listings` and `/alerts` gain proper cursor pagination
  (currently offset-based). Cursor is opaque, base64-encoded.
- `/api/v1/watches/{id}` PATCH gains a `disabled_reason` field for
  audit ("disabled because supplier left market 2026-04").
- CORS middleware respects `WEB_ORIGIN` env var; `*` in dev,
  `https://spt.fartlab.dev` in prod.

### Removed endpoints

- `GET /alerts` (the templ HTML page) and the `/static/*` embedded
  asset routes are removed once SPA reaches parity. The OpenAPI surface
  doesn't change since these never lived under `/api/v1/`.

## Meilisearch Integration

### Indexing pipeline

Meilisearch isn't the source of truth — Postgres is. The API
maintains two Meili indexes (`listings`, `alerts`), kept in sync via:

- **Initial backfill**: a one-shot CLI command `spt meili reindex`
  reads all rows and bulk-loads.
- **Live updates**: every `INSERT`/`UPDATE` on `listings` and `alerts`
  calls into a Meili client after the DB commit. Best-effort; failures
  are logged and a periodic reconciler (every 1h) catches drift.
- **No DB triggers / no logical replication.** Application-level
  indexing keeps the dependency optional and fail-soft.

### Index schema

`listings` index document:

```json
{
  "id": "uuid",
  "title": "string",                 // searchable, primary
  "component_type": "ram",           // facet
  "manufacturer": "string",          // facet, searchable
  "model": "string",                 // facet, searchable
  "price": 199.99,                   // sortable, filterable range
  "score": 87,                       // sortable, filterable range
  "active": true,                    // facet
  "created_at_ts": 1714521600,       // sortable
  "attributes_text": "..."           // flattened JSONB for full-text
}
```

`alerts` index document:

```json
{
  "id": "uuid",
  "watch_name": "DDR4 ECC 32GB",     // searchable
  "listing_title": "string",         // searchable
  "component_type": "ram",           // facet
  "score": 87,                       // sortable
  "notified": true,                  // facet
  "dismissed": false,                // facet
  "created_at_ts": 1714521600        // sortable
}
```

### Why Meili (not Postgres FTS)

- Typeahead UX without writing custom debounced LIKE queries.
- Facets are first-class — drilldown UI is one query.
- Typo-tolerant matching out of the box (relevant for noisy eBay titles).
- Postgres remains authoritative; the index is rebuildable from scratch
  in minutes, so corruption is recoverable.

## Authentication

### Flow

- SPA performs OIDC PKCE against Keycloak realm `spt` (or whatever
  realm the operator uses).
- Access token is a short-lived JWT (5–15min). Refresh token used
  silently.
- Every API request carries `Authorization: Bearer <jwt>`.
- API middleware verifies signature against JWKS (cached with TTL),
  validates `iss`, `aud`, `exp`, `nbf`. Extracts `sub`, `preferred_username`,
  and `realm_access.roles` into request context.

### Roles

- `spt-viewer` — read-only access to all GETs.
- `spt-operator` — full access (CRUD watches, dismiss alerts).
- `spt-admin` — operator + can trigger ingest/rescore/reextract.

Roles map to endpoint permissions via a small middleware — not RBAC-
heavy, just an enum check.

### CLI auth

`spt` CLI gains:

- `spt login` — runs OIDC device-code flow against Keycloak, stores
  refresh token in `~/.config/spt/credentials`.
- Existing commands grow `--token` flag; default reads from credentials
  file.

### Local dev

- Dev mode env (`SPT_AUTH_DISABLED=true`) bypasses auth — keeps the
  current "trust localhost" loop working.
- CI tests use a stubbed JWT signed with a test key.

## Deployment Topology

### Helm chart additions

```yaml
# values.yaml
web:
  enabled: false              # opt-in for now
  image:
    repository: ghcr.io/donaldgifford/server-price-tracker-web
    tag: ""                   # defaults to chart appVersion
  replicas: 2
  resources: { ... }
  ingress:
    host: spt.fartlab.dev     # SPA at /, API at /api/*

meilisearch:
  enabled: false
  image: getmeili/meilisearch:v1.10
  persistence:
    size: 5Gi
  masterKey:
    existingSecret: spt-meili-master-key
    key: master-key

auth:
  oidc:
    enabled: false
    issuerURL: https://keycloak.example.com/realms/spt
    clientID: server-price-tracker
    audience: spt-api
```

Three new templates:

- `templates/web-deployment.yaml`, `templates/web-service.yaml` — SPA
  container.
- `templates/meilisearch-statefulset.yaml`, `templates/meilisearch-pvc.yaml`,
  `templates/meilisearch-service.yaml` — Meili.
- HTTPRoute updated: `/` → web service, `/api/*` and `/openapi.json`
  → api service. `/healthz`, `/metrics` stay on api.

### Container layout

- `server-price-tracker` (existing) — Go binary, unchanged.
- `server-price-tracker-web` (new) — multi-stage Dockerfile:
  - `bun install && bun run build` produces `dist/`.
  - `nginx:alpine-slim` serves `dist/` with a config that fall-throughs
    to `index.html` for SPA routing.
- Both share the same Helm chart; CI builds both images per release.

### Cluster prereqs

- Meilisearch StatefulSet needs RWO PVC (5Gi default).
- Keycloak realm must exist; chart values reference issuer/client.
- Operator creates a Keycloak service account for the API to validate
  JWKS (no client secret needed for asymmetric flow).

## Migration Plan

### Phase 1 — Foundation (no UI yet)

1. Add Meilisearch StatefulSet template to chart, gated by
   `meilisearch.enabled=false`.
2. Add OIDC validation middleware to API, gated by
   `auth.oidc.enabled=false`. New CLI auth commands.
3. New endpoints: `/api/v1/alerts/top`, `/api/v1/search/*`,
   `/api/v1/dashboard/summary`, `/api/v1/me`. Cursor pagination.
4. Indexing pipeline + reconciler. `spt meili reindex` command.
5. CI: build + push web image (empty stub for now).

### Phase 2 — SPA at parity with `/alerts`

6. SPA project scaffold (`web/` directory, Bun, shadcn/ui).
7. OIDC PKCE login.
8. Alert review page (full parity with current `/alerts`).
9. Watch list (read-only).
10. Deploy alongside templ UI (new Helm value `web.enabled=true`,
    templ stays at `/alerts` until parity confirmed).

### Phase 3 — Beyond parity

11. Dashboard view (top-N per type, time window).
12. Listing search (Meili-powered).
13. Watch CRUD UI.
14. Settings page.
15. Remove templ UI: delete `internal/api/web/`, route `/alerts`
    handler, embedded asset routes. Bump chart minor version.

### Rollback

- At any point before Phase 3 step 15, `web.enabled=false` reverts to
  templ-only UI. After step 15, rollback requires reverting the
  chart minor version.

## Testing Strategy

### Frontend

- **Vitest + React Testing Library** for component tests.
- **Playwright** for e2e — mocked API + real Meili in a docker-compose
  smoke test.
- **MSW** (mock service worker) for component-level API mocking.

### Backend

- New endpoints get table-driven tests in
  `internal/api/handlers/*_test.go`.
- Auth middleware tested with stub JWTs and real JWKS fixtures.
- Meilisearch indexing tested with `meilisearch:v1.10` in a docker test
  container (build tag `integration`).

### Helm

- New `meilisearch-*.yaml` templates get helm-unittest assertions.
- chart-testing-action installs the full chart with all toggles on
  in the kind CI cluster and checks for green pods.

## Open Questions

1. **Where does the SPA bundle live in the API container vs separate?**
   Two viable shapes: (a) build SPA inside the API container and serve
   from Go via `go:embed` (one container, simpler ops) or (b) separate
   nginx container as drafted (better separation, slightly more
   moving parts). User chose (b) — locked in.

2. **Meilisearch persistence backup story** — do we ever need to back
   up the Meili volume, or always reindex from Postgres on disaster?
   Proposal: never back up. PVC is reproducible; faster to rebuild
   from Postgres. CNPG already handles the source-of-truth backups.

3. **OIDC client type** — _resolved: public client + PKCE, no secret._
   Keycloak realm is configured that way. Chart values reference issuer
   and client ID; no client-secret config field needed.

4. **Role granularity** — _resolved: three roles
   (viewer/operator/admin)._ Endpoint-level permissions deferred until
   a real need surfaces.

5. **What happens to existing Grafana dashboards?** If the SPA absorbs
   the alerts/listings views, do we delete the equivalent Grafana
   panels or leave them? Proposal: leave Grafana for SRE-shaped
   metrics (rate limiter, ingestion duration, error budgets); the SPA
   takes the product-shaped views. They serve different audiences.

6. **Bundle size budget** — React + shadcn + TanStack stack lands at
   ~150–250KB gzipped before our code. Acceptable for a personal/
   small-team tool; if it ever needs to load on slow links, code-split
   per route.

7. **Component library copy-in maintenance** — shadcn/ui copies
   components into the repo. Updating later is manual (re-run the CLI
   per component). Pin a shadcn version in `web/components.json` and
   document the upgrade path in `docs/OPERATIONS.md`.

8. **Search for free-text JSONB attributes** — Meili needs a flat
   document. Strategy: at index time, flatten `listings.attributes`
   into `attributes_text` (concatenated values, separator-joined).
   Lossy for type-specific search (e.g., "drives with capacity > 4TB"
   stays in Postgres-via-API). Acceptable trade-off.

9. **CI cost** — adding a Bun build step + Playwright in CI ~doubles
   CI time. Acceptable, but consider parallelising frontend and Go
   jobs to keep wall-clock similar.

10. **Watch CRUD form schema** — _resolved: generated zod schema as
    floor, override per-field for UX._ Generated client stays in sync
    with the OpenAPI spec; per-field overrides for places where the
    auto-generated validation is too coarse.

## References

- DESIGN-0010 — Alert review UI with pagination and search (the
  surface this design replaces)
- IMPL-0015 Phase 6 — current templ + HTMX + Alpine implementation
- DESIGN-0013 — Discord channel routing (interim noise solution
  before SPA dashboard ships)
- `/api/v1/*` Huma OpenAPI spec — runtime-generated, source of truth
  for the SPA's typed client
- Keycloak OIDC docs — Authorization Code with PKCE flow
- Meilisearch docs — indexing model, faceted search, ranking rules
- shadcn/ui — component library philosophy
- TanStack Query — server state management
- TanStack Router — typed routing
