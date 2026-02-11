# Server Price Tracker — Design Document

## Overview

An API-first Go service that monitors eBay listings for server hardware,
extracts structured attributes via LLM (Ollama, Anthropic Claude, or compatible
backends), scores listings against historical baselines, and alerts on deals via
Discord webhooks. The CLI acts as a client to the API, and the API design
supports future integrations (Discord bot, web UI, Grafana dashboards).

## Architecture

```
                                    ┌──────────────────────────────────────────────┐
                                    │           server-price-tracker API           │
                                    │               (Echo HTTP)                    │
                                    │                                              │
┌──────────────┐    ┌───────────────┤  ┌──────────────┐      ┌──────────────┐      │
│  eBay Browse │───▶│   Ingestion   │  │  LLM Extract │      │   Scorer /   │      │
│     API      │    │    Loop       │─▶│  (Ollama /   │───┐  │  Alert Loop  │      │
└──────────────┘    │               │  │   Claude)    │   │  │              │      │
                    └───────────────┤  └──────────────┘   │  └──────┬───────┘      │
                                    │                     │         │              │
                                    │  ┌──────────────┐   │  ┌──────┴───────┐      │
                                    │  │   Postgres   │◀──┘  │   Discord    │      │
                                    │  │              │◀─────│   Webhook    │      │
                                    │  └──────┬───────┘      └──────────────┘      │
                                    │         │                                    │
                                    │  ┌──────┴───────┐     ┌──────────────┐       │
                                    │  │  Prometheus  │────▶│   Grafana    │       │
                                    │  │  /metrics    │     │              │       │
                                    │  └──────────────┘     └──────────────┘       │
                                    └──────────────────────────────────────────────┘
                                                      ▲
                                    ┌─────────────────┤
                                    │    CLI Client   │
                                    │  (cobra, HTTP)  │
                                    └─────────────────┘

External tool (separate binary):
┌─────────────────────────┐
│  tools/baseline-seeder  │──── imports server-price-tracker/pkg/*
│  (cold-start helper)    │──── eBay Finding API → extract → seed baselines
└─────────────────────────┘
```

### Design Principles

- **API-first**: All functionality exposed via HTTP endpoints. The CLI is a thin
  client that calls the API. This enables future Discord bot, web UI, and
  external integrations to use the same interface.
- **Interface-first / TDD**: Every external dependency (eBay API, LLM backends,
  Postgres, Discord) is abstracted behind a Go interface. Mockery generates mock
  implementations for all interfaces, enabling comprehensive table-driven tests
  with testify before any external service is connected. Tests are written
  alongside code in every phase, not deferred. Code that cannot be tested via
  TDD is annotated with `// TODO(test): <reason>` using the todo-comments
  convention.
- **Exported packages**: Core logic lives in `pkg/` so external tools (like the
  baseline seeder) can import and reuse it without coupling to the main binary.
- **Datastore abstraction**: The `Store` interface in `internal/store/` defines
  all data access. Postgres implements it, but any backend could. All business
  logic depends on the interface, never on `pgx` directly. This enables
  mock-based testing of handlers, engine, and CLI without a running database.
- **LLM backend abstraction**: A common interface for LLM calls, with
  implementations for Ollama (default), Anthropic Claude API (Haiku or other
  models), and OpenAI-compatible endpoints. This keeps extraction logic
  decoupled from the provider.

## Key Interfaces

Every boundary in the system is defined by a Go interface. Mockery generates
mocks for all of these, enabling full TDD without external dependencies.

| Interface       | Package           | Purpose                                     | Implementations                                                              |
| --------------- | ----------------- | ------------------------------------------- | ---------------------------------------------------------------------------- |
| `Store`         | `internal/store`  | All data access (CRUD, queries, migrations) | `PostgresStore`, `MockStore` (mockery)                                       |
| `LLMBackend`    | `pkg/extract`     | LLM text generation                         | `OllamaBackend`, `AnthropicBackend`, `OpenAICompatBackend`, `MockLLMBackend` |
| `Extractor`     | `pkg/extract`     | Classification + attribute extraction       | `LLMExtractor`, `MockExtractor`                                              |
| `EbayClient`    | `internal/ebay`   | eBay Browse API search + item details       | `BrowseClient`, `MockEbayClient`                                             |
| `TokenProvider` | `internal/ebay`   | OAuth2 token management                     | `OAuthTokenProvider`, `MockTokenProvider`                                    |
| `Notifier`      | `internal/notify` | Alert delivery                              | `DiscordNotifier`, `MockNotifier`                                            |
| `Scorer`        | `pkg/scorer`      | Composite deal scoring                      | `DefaultScorer`, `MockScorer`                                                |

### Mockery Configuration

Mocks are generated via `mockery` and stored in each package's `mocks/`
subdirectory. The `.mockery.yaml` config at the repo root defines generation
rules. Run `make mocks` to regenerate after interface changes.

## Testing Strategy

- **Table-driven tests**: All tests use the table-driven pattern with
  `testify/assert` and `testify/require`. Each test case is a struct with name,
  inputs, expected outputs, and optional error expectations.
- **TDD per phase**: Every implementation phase includes writing tests before or
  alongside code. Tests for a phase must pass before the phase is considered
  complete.
- **Mock-based unit tests**: Handlers, engine, and CLI are tested entirely
  against mocked interfaces. No external service is needed to run
  `go test ./...`.
- **Integration tests**: Tagged with `//go:build integration` so they only run
  when explicitly requested (e.g., `go test -tags integration ./...`). These hit
  real Postgres (via testcontainers), real Ollama, etc.
- **Untestable code annotation**: Any code that cannot be practically tested via
  TDD must be annotated with `// TODO(test): <explanation>` per the
  todo-comments convention. This is tracked and should trend toward zero.

## Scoring System

Each listing receives a composite score (0–100) derived from weighted
sub-scores:

| Factor           | Weight | Description                                      |
| ---------------- | ------ | ------------------------------------------------ |
| Price Percentile | 40%    | Where this price falls vs. rolling baseline      |
| Seller Trust     | 20%    | Feedback score, %, top-rated status              |
| Condition        | 15%    | New/Like New/Used-Working/For Parts              |
| Quantity Value   | 10%    | Per-unit price for lots                          |
| Listing Quality  | 10%    | Has photos, specifics filled, description length |
| Time Pressure    | 5%     | Auction ending soon, newly listed BIN            |

### Price Percentile Scoring

- p10 or below → 100 points (exceptional deal)
- p25 → 85
- p50 (median) → 50
- p75 → 25
- p90+ → 0

Baselines computed per **normalized product key** (e.g.,
`ram:ddr4:ecc_reg:32gb:2666`) from the last 90 days of sold listings.

**Cold-start behavior:** Before enough sold data exists for a product key (<
min_baseline_samples), the price factor defaults to a neutral score of 50. Use
the external `tools/baseline-seeder` to bootstrap baselines from eBay
completed/sold listings for key categories.

### Seller Trust Scoring

- Feedback score: 0–100 → 0 pts, 100–500 → 40 pts, 500–5000 → 70 pts, 5000+ →
  100 pts
- Feedback percentage: <95% → 0, 95–98% → 50, 98–99.5% → 80, 99.5%+ → 100
- Top-rated seller bonus: +20 pts (capped at 100)
- Final = avg of components, capped at 100

## Data Model

### watches (saved searches / alert configs)

```sql
CREATE TABLE watches (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,               -- "Cheap R640 RAM"
    search_query    TEXT NOT NULL,               -- eBay search string
    category_id     TEXT,                        -- eBay category filter
    component_type  TEXT NOT NULL,               -- ram | drive | server | cpu | nic | other
    filters         JSONB NOT NULL DEFAULT '{}', -- structured attribute filters
    score_threshold INTEGER NOT NULL DEFAULT 75, -- alert when score >= this
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Example filters:

```json
{
  "capacity_gb": { "min": 32, "max": 32 },
  "memory_type": "DDR4",
  "ecc": true,
  "registered": true,
  "seller_min_feedback": 500,
  "seller_min_feedback_pct": 98.0,
  "price_max": 30.0,
  "condition": ["new", "used_working"]
}
```

### listings

```sql
CREATE TABLE listings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ebay_item_id    TEXT UNIQUE NOT NULL,
    title           TEXT NOT NULL,
    price           NUMERIC(10,2) NOT NULL,
    currency        TEXT NOT NULL DEFAULT 'USD',
    listing_type    TEXT NOT NULL,                -- auction | buy_it_now | best_offer
    condition_raw   TEXT,
    condition_norm  TEXT,                         -- new | like_new | used_working | for_parts
    seller_name     TEXT,
    seller_feedback_score   INTEGER,
    seller_feedback_pct     NUMERIC(5,2),
    seller_top_rated        BOOLEAN DEFAULT false,
    image_url       TEXT,
    item_url        TEXT NOT NULL,
    ebay_category   TEXT,
    shipping_cost   NUMERIC(10,2),
    quantity        INTEGER NOT NULL DEFAULT 1,
    -- LLM-extracted structured attributes
    component_type  TEXT,                         -- ram | drive | server | cpu | nic
    attributes      JSONB NOT NULL DEFAULT '{}',  -- extracted structured data
    extraction_confidence NUMERIC(3,2),           -- 0.00–1.00
    -- scoring
    score           INTEGER,                      -- 0–100 composite score
    score_breakdown JSONB,                        -- per-factor scores
    -- metadata
    listed_at       TIMESTAMPTZ,
    sold_at         TIMESTAMPTZ,
    sold_price      NUMERIC(10,2),
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_listings_component ON listings(component_type);
CREATE INDEX idx_listings_score ON listings(score DESC);
CREATE INDEX idx_listings_attrs ON listings USING GIN(attributes);
CREATE INDEX idx_listings_first_seen ON listings(first_seen_at);
```

### price_baselines

```sql
CREATE TABLE price_baselines (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_key     TEXT UNIQUE NOT NULL,         -- e.g. "ram:ddr4:ecc_reg:32gb:2666"
    sample_count    INTEGER NOT NULL,
    p10             NUMERIC(10,2),
    p25             NUMERIC(10,2),
    p50             NUMERIC(10,2),
    p75             NUMERIC(10,2),
    p90             NUMERIC(10,2),
    mean            NUMERIC(10,2),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### alerts

```sql
CREATE TABLE alerts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    watch_id        UUID NOT NULL REFERENCES watches(id),
    listing_id      UUID NOT NULL REFERENCES listings(id),
    score           INTEGER NOT NULL,
    notified        BOOLEAN NOT NULL DEFAULT false,
    notified_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## Component Types

The MVP supports all five component types:

| Type   | Description       | Product Key Format                           | Example                      |
| ------ | ----------------- | -------------------------------------------- | ---------------------------- |
| RAM    | Server memory     | `ram:{gen}:{type}:{capacity}:{speed}`        | `ram:ddr4:ecc_reg:32gb:2666` |
| Drive  | Storage (SSD/HDD) | `drive:{interface}:{form}:{capacity}:{type}` | `drive:sas:2.5:1.2tb:10k`    |
| Server | Complete servers  | `server:{mfg}:{model}:{form}`                | `server:dell:r740xd:sff`     |
| CPU    | Server processors | `cpu:{mfg}:{family}:{model}`                 | `cpu:intel:xeon:gold_6130`   |
| NIC    | Network adapters  | `nic:{speed}:{ports}:{type}`                 | `nic:10gbe:2port:sfp+`       |

## LLM Extraction

### Backend Abstraction

```go
type LLMBackend interface {
    Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
    Name() string
}
```

Implementations:

- **Ollama** (default) — local, free, good with Mistral 7B (Q4/Q5) or
  llama3.1:8b
- **Anthropic Claude API** — Claude Haiku for fast/cheap extraction,
  configurable model
- **OpenAI-compatible** — any endpoint that speaks the OpenAI chat completions
  API

### Two-Pass Extraction

**Pass 1 — Classification (title-only):** Determine component type. Fast (~100ms
local).

**Pass 2 — Attribute Extraction (component-specific):** Full structured JSON
extraction using the component-specific prompt and schema.

See `docs/EXTRACTION.md` for all prompts, grammars, and validation rules.

### Grammar-Constrained Output

When using Ollama or llama.cpp backends, GBNF grammar support forces valid JSON
matching the schema. This eliminates parse failures and most hallucination for
structured extraction. Claude API uses structured tool_use or JSON mode instead.

## Alert Flow

1. New listing ingested → LLM extracts attributes → stored
2. Product key generated from attributes
3. Price compared against `price_baselines` for that key
4. Composite score computed
5. Score checked against all matching `watches`
6. If score >= threshold AND filters match → create alert → notify

## Notification Targets

- **Discord Webhooks** — primary notification channel. Rich embeds with listing
  image, price, score breakdown, and direct eBay link. Future path: Discord bot
  that accepts commands to query watches/listings.
- **Webhook (generic)** — for custom integrations

## API Endpoints

The Echo HTTP server exposes RESTful endpoints. The CLI is a client to this API.

```
# Service
GET  /healthz                          # health check
GET  /readyz                           # readiness (DB connected, LLM reachable)
GET  /metrics                          # Prometheus metrics

# Watches
POST   /api/v1/watches                 # create watch
GET    /api/v1/watches                 # list watches
GET    /api/v1/watches/:id             # get watch
PUT    /api/v1/watches/:id             # update watch
DELETE /api/v1/watches/:id             # delete watch
POST   /api/v1/watches/:id/enable      # enable watch
POST   /api/v1/watches/:id/disable     # disable watch

# Listings
GET    /api/v1/listings                # list with filters (component, score, price, etc.)
GET    /api/v1/listings/:id            # full detail + score breakdown

# Baselines
GET    /api/v1/baselines               # list all baselines
GET    /api/v1/baselines/:product_key  # baseline detail
POST   /api/v1/baselines/refresh       # trigger recomputation

# Operations
POST   /api/v1/ingest                  # trigger manual ingestion cycle
POST   /api/v1/rescore                 # force re-score all listings

# Utilities
POST   /api/v1/extract                 # one-off LLM extraction test
POST   /api/v1/search                  # one-off eBay search (no persistence)
```

## CLI Commands

The CLI calls the API server. It can target a local or remote instance via
`--api-url` flag.

```
server-price-tracker
├── serve                               # start the API server + scheduler
├── migrate                             # apply DB migrations
├── watch
│   ├── add                             # create a new watch
│   ├── list                            # show all watches
│   ├── show <id>                       # watch details + recent alerts
│   ├── edit <id>                       # modify watch params
│   ├── enable <id>                     # enable a watch
│   ├── disable <id>                    # disable a watch
│   └── remove <id>                     # delete a watch
├── listings
│   ├── list                            # browse listings with filters
│   ├── show <id|ebay_id>              # full listing detail + score breakdown
│   └── rescore                         # force re-score all listings
├── baselines
│   ├── list                            # show all baselines
│   ├── show <product_key>             # baseline detail
│   └── refresh                         # force baseline recomputation
├── search <query>                      # one-off eBay search (no persistence)
├── extract <title>                     # one-off LLM extraction test
└── version
```

## Prometheus Metrics

Exposed at `/metrics` for Grafana dashboards:

- `spt_ingestion_listings_total` — counter of listings ingested, labeled by
  component_type
- `spt_ingestion_cycle_duration_seconds` — histogram of ingestion cycle duration
- `spt_extraction_duration_seconds` — histogram of LLM extraction time
- `spt_extraction_failures_total` — counter of extraction failures
- `spt_scoring_distribution` — histogram of listing scores
- `spt_alerts_fired_total` — counter of alerts, labeled by watch_name
- `spt_ebay_api_calls_total` — counter of eBay API calls
- `spt_ebay_api_errors_total` — counter by error type
- `spt_baseline_sample_count` — gauge per product_key
- `spt_active_listings_total` — gauge by component_type

## Scheduling

- Ingestion: every 15 min per watch (staggered)
- Baseline refresh: every 6 hours
- Alert evaluation: triggered post-ingestion (inline, not separate schedule)

## External Tooling

### Baseline Seeder (`tools/baseline-seeder`)

A separate Go binary that imports `server-price-tracker/pkg/*` packages. Used to
bootstrap price baselines from eBay completed/sold listings before the main
tracker has accumulated enough data.

- Uses eBay Finding API (different auth from Browse API) to fetch completed
  items
- Runs LLM extraction on sold listings using the same extraction pipeline
- Inserts listings with `sold_at` and `sold_price` populated
- Triggers baseline recomputation

This tool lives outside the main binary because sold-listing ingestion requires
different API credentials and is a one-time/periodic bootstrapping operation,
not a continuous pipeline.

## Deployment

### Target Environment

- **Platform**: Talos Linux Kubernetes cluster
- **GitOps**: ArgoCD for continuous deployment
- **Manifests**: Kustomize (base + overlays for dev/prod)
- **Ingress**: Cilium API Gateway (Gateway API) for traffic routing to the API
  server
- **Observability**: Prometheus (in-cluster scraping) + Grafana (dashboards)

### Container Strategy

- Multi-stage Dockerfile producing a minimal Alpine-based image
- Single binary, no runtime dependencies beyond `ca-certificates`
- Container image built via CI (GoReleaser or GitHub Actions)
- Image pushed to container registry, referenced by ArgoCD Application manifests

### Kustomize Structure

```
deploy/
├── base/
│   ├── kustomization.yaml
│   ├── deployment.yaml          # server-price-tracker Deployment
│   ├── service.yaml             # ClusterIP Service on port 8080
│   ├── configmap.yaml           # config.yaml (non-secret values)
│   ├── httproute.yaml           # Cilium Gateway API HTTPRoute
│   ├── serviceaccount.yaml
│   └── servicemonitor.yaml      # Prometheus ServiceMonitor for /metrics
├── overlays/
│   ├── dev/
│   │   ├── kustomization.yaml
│   │   └── patches/             # dev-specific overrides (replicas, resources, log level)
│   └── prod/
│       ├── kustomization.yaml
│       └── patches/             # prod-specific overrides
└── argocd/
    └── application.yaml         # ArgoCD Application pointing to this repo
```

Secrets (DB password, eBay credentials, Discord webhook URL, Anthropic API key)
are managed outside Kustomize via Kubernetes Secrets (sealed-secrets or
external-secrets-operator).

### Dependencies (external to the app, managed separately in k8s)

- **PostgreSQL** — deployed separately in the cluster (CloudNativePG, or
  external managed DB)
- **Ollama** — deployed as a separate workload with GPU scheduling, or external
  endpoint
- **Prometheus + Grafana** — existing cluster observability stack, app exposes
  `/metrics` via ServiceMonitor
