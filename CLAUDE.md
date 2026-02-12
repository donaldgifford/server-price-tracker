# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Server Price Tracker is an API-first Go service that monitors eBay listings for server hardware deals (RAM, drives, servers, CPUs, NICs). It extracts structured attributes via LLM (Ollama default, Anthropic Claude API optional), scores listings against historical price baselines, and sends deal alerts via Discord webhooks. The CLI acts as a thin client to the HTTP API.

**Current state:** MVP implementation is complete (Phases 0-9). All domain types, scoring engine, LLM extraction, eBay integration, API handlers, middleware, and deployment manifests are implemented. See `docs/IMPLEMENTATION.md` for the full build plan.

## Build & Development Commands

Tool versions are managed via `mise.toml` — run `mise install` to set up the toolchain (Go 1.25.7, golangci-lint 2.8.0, etc.).

The build system uses a modular Makefile structure: root `Makefile` includes domain-specific files from `scripts/makefiles/` (common.mk, go.mk, docker.mk, db.mk).

```bash
# Build
make build                    # or: go build -o build/bin/server-price-tracker ./cmd/server-price-tracker

# Run the API server + scheduler
make run                      # or: go run ./cmd/server-price-tracker serve --config configs/config.dev.yaml

# Lint (Uber-style Go via golangci-lint)
make lint                     # or: golangci-lint run ./...

# Test (unit tests only, no external services needed)
make test                     # or: go test ./...

# Test with integration tests (requires eBay sandbox credentials, Postgres, etc.)
make test-integration         # or: go test -tags integration ./...

# Test with e2e tests
make test-e2e                 # or: go test -tags e2e ./test/e2e/...

# Test with coverage (used in CI)
make test-coverage            # or: go test -race -coverprofile=coverage.out -covermode=atomic ./...

# Run a single test
go test -run TestFunctionName ./pkg/scorer/...

# Format
make fmt                      # or: goimports -w . && golines -w .

# Generate mocks (run after changing any interface)
make mocks                    # or: mockery

# Apply database migrations
make migrate                  # or: go run ./cmd/server-price-tracker migrate

# Docker (not yet wired — targets in docker.mk are placeholder references)
# make docker-up              # Start PostgreSQL
# make docker-down            # Stop PostgreSQL

# Build cross-platform (via GoReleaser)
goreleaser build --snapshot --clean
```

## Architecture

### API-First Design

All functionality is exposed via an Echo HTTP API (`/api/v1/*`). The CLI is a client to this API. This pattern enables future Discord bot integration, web UI, and external tool access.

```
eBay Browse API → Ingestion Loop → LLM Extract (Ollama/Claude) → PostgreSQL → Scorer → Alert → Discord Webhook
                                                                                        ↓
                                                                    Echo HTTP API ← CLI Client
                                                                        ↓
                                                                    Prometheus → Grafana
```

### Interface-First Design

Every external dependency is abstracted behind a Go interface. Mockery generates mocks for all interfaces. This enables full TDD without any external services running.

| Interface | Package | Implementations |
|---|---|---|
| `Store` | `internal/store` | `PostgresStore`, `MockStore` |
| `LLMBackend` | `pkg/extract` | `OllamaBackend`, `AnthropicBackend`, `OpenAICompatBackend`, `MockLLMBackend` |
| `Extractor` | `pkg/extract` | `LLMExtractor`, `MockExtractor` |
| `EbayClient` | `internal/ebay` | `BrowseClient`, `MockEbayClient` |
| `TokenProvider` | `internal/ebay` | `OAuthTokenProvider`, `MockTokenProvider` |
| `Notifier` | `internal/notify` | `DiscordNotifier`, `MockNotifier` |

### Pipeline

1. **Watches** define saved searches with component type, filters, and score threshold
2. **Ingestion** polls eBay per watch on a 15-min schedule (staggered)
3. **LLM Extraction** (two-pass): classify component type from title, then extract component-specific attributes using the configured backend
4. **Product Key** generation normalizes attributes for baseline grouping (e.g., `ram:ddr4:ecc_reg:32gb:2666`)
5. **Scoring** computes a weighted 0–100 composite score (price 40%, seller 20%, condition 15%, quantity 10%, quality 10%, time 5%). Price factor defaults to neutral 50 when baseline has insufficient samples (cold start).
6. **Alerts** fire when score >= watch threshold and filters match; sent as Discord webhook rich embeds

### Project Layout

```
cmd/server-price-tracker/     Cobra CLI entry point (serve, migrate commands)
configs/                      YAML configuration files
  config.example.yaml         Template with env var placeholders
  config.dev.yaml             Local development configuration
migrations/                   PostgreSQL schema migrations (source of truth)
internal/store/migrations/    Embedded copy for Go embed.FS
scripts/makefiles/            Modular Makefile includes (common, go, docker, db)
test/                         Top-level test directories
  e2e/                        End-to-end tests (//go:build e2e)
  integration/                Cross-cutting integration tests (//go:build integration)
  utils/                      Shared test utilities
deploy/                       Kubernetes manifests (Kustomize base + overlays)
  argocd/                     ArgoCD Application manifest
  base/                       Base Kustomize resources
  overlays/dev/               Dev overlay (debug logging, lower resources)
  overlays/prod/              Prod overlay (info logging, higher resources)
docs/                         Design and implementation documentation
```

**Exported (`pkg/`)** — importable by external tools:
- `pkg/types/` (package `domain`) — Core domain types: `Listing`, `Watch`, `WatchFilters`, `PriceBaseline`, `Alert`, `ScoreBreakdown`. Enums: `ComponentType`, `Condition`, `ListingType`. Contains `WatchFilters.Match()`.
- `pkg/scorer/` (package `score`) — Composite scoring with `Score(ListingData, *Baseline, Weights) Breakdown`. Decoupled from DB models via `ListingData` input struct.
- `pkg/extract/` — `LLMBackend` and `Extractor` interfaces, implementations (Ollama, Anthropic, OpenAI-compatible), extraction orchestrator, prompt templates, response validation.

**Internal (`internal/`)** — application-specific, not importable:
- `internal/api/` — Echo HTTP server, route handlers, middleware (Prometheus metrics, request logging, panic recovery)
- `internal/store/` — `Store` interface (datastore abstraction) + `PostgresStore` implementation (raw SQL with pgx, no ORM)
- `internal/engine/` — Ingestion loop, baseline recomputation, alert evaluation, scheduler. Takes all dependencies as interfaces.
- `internal/ebay/` — `EbayClient` and `TokenProvider` interfaces + implementations
- `internal/notify/` — `Notifier` interface + `DiscordNotifier` implementation
- `internal/config/` — YAML config loader with `os.ExpandEnv()` for secrets

### Configuration

YAML config files live in `configs/`. The config loader (`internal/config`) reads YAML with `os.ExpandEnv()` for secret injection from environment variables. The `.env` file (gitignored) holds local credentials.

Key env vars: `EBAY_APP_ID`, `EBAY_DEV_ID`, `EBAY_CERT_ID`, `EBAY_TOKEN_URL`, `EBAY_BROWSE_URL`, `DB_PASSWORD`, `DISCORD_WEBHOOK_URL`, `ANTHROPIC_API_KEY`.

eBay API URLs default to production (`api.ebay.com`) when `EBAY_TOKEN_URL`/`EBAY_BROWSE_URL` are empty. Set them to `api.sandbox.ebay.com` equivalents for sandbox testing.

### Key API Endpoints

- `GET /healthz`, `GET /readyz`, `GET /metrics` — operational
- `POST/GET /api/v1/watches` — watch CRUD
- `GET /api/v1/listings` — query with filters (component, score, price)
- `POST /api/v1/ingest` — trigger manual ingestion
- `POST /api/v1/extract` — one-off LLM extraction test
- `POST /api/v1/baselines/refresh` — recompute baselines

## Testing Strategy

- **TDD**: Tests are written alongside code in every phase, not deferred. Each phase defines its own test requirements and coverage targets.
- **Table-driven tests**: All tests use the table-driven pattern with `testify/assert` and `testify/require`. Each test case is a struct with name, inputs, expected outputs, and error expectations.
- **Mockery**: All interfaces have generated mocks (run `make mocks` after interface changes). Configured via `.mockery.yaml`. Mocks live in `<package>/mocks/` subdirectories.
- **Test organization**: Unit tests live next to code (`*_test.go`). Package-level integration tests use `//go:build integration` in their source package. Cross-cutting integration and e2e tests live in `test/integration/` and `test/e2e/` respectively.
- **Unit tests run without external services**: `make test` requires no database, no LLM, no eBay API, no Discord. Everything is mocked.
- **Untestable code annotation**: Code that cannot be practically tested via TDD must be annotated with `// TODO(test): <explanation>` using the todo-comments convention. These are tracked and should trend toward zero.
- **Coverage targets**: >= 90% on `pkg/`, >= 80% on `internal/`, >= 85% per package as a working target.

## Code Conventions

- **Linting:** Uber Go Style Guide enforced via `.golangci.yml`. Key limits: cyclomatic complexity ≤15, function length ≤100 lines, cognitive complexity ≤30, nested-if depth ≤4.
- **Imports:** Ordered as standard → third-party → `github.com/donaldgifford/` (enforced by gci).
- **Formatting:** gofumpt + golines (max 150 chars).
- **Errors:** Always check errors (`errcheck`), wrap with `%w` (`errorlint`), error type names end with `Error`.
- **Context:** First parameter, proper propagation enforced by `contextcheck`/`noctx`.
- **Resources:** HTTP bodies and SQL rows must be closed (`bodyclose`, `sqlclosecheck`).
- **Testing:** Table-driven with testify. `t.Helper()` in helpers, `t.Parallel()` where safe.
- **Interfaces:** Every external boundary has an interface. Business logic depends on interfaces, never concrete implementations.
- **Commits:** Conventional Commits format (`feat:`, `fix:`, `chore:`, `docs:`). GoReleaser changelog groups by type.
- **No ORM** — raw SQL with pgx. All SQL lives in `internal/store/queries.go` as constants.

## Deployment

- **Target:** Talos Linux Kubernetes cluster
- **GitOps:** ArgoCD with auto-sync
- **Manifests:** Kustomize (base + overlays for dev/prod) in `deploy/`
- **Ingress:** Cilium API Gateway (Gateway API HTTPRoute)
- **Observability:** Prometheus ServiceMonitor scraping `/metrics`, Grafana dashboards
- **Secrets:** Kubernetes Secrets (sealed-secrets or external-secrets-operator), not in manifests

## Design Documentation

- `docs/DESIGN.md` — Full architecture, interface catalog, testing strategy, API endpoints, data model, scoring formula, product key strategy, deployment architecture
- `docs/IMPLEMENTATION.md` — 10-phase MVP plan with TDD workflow, detailed task checkboxes, table-driven test specifications, and success criteria per phase
- `docs/EXTRACTION.md` — LLM backend options (Ollama, Claude API, OpenAI-compat), prompts for all 5 component types, GBNF grammars, validation rules, product key generation
