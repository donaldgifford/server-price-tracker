# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Server Price Tracker is an API-first Go service that monitors eBay listings for server hardware deals (RAM, drives, servers, CPUs, NICs). It extracts structured attributes via LLM (Ollama default, Anthropic Claude API optional), scores listings against historical price baselines, and sends deal alerts via Discord webhooks. Two binaries: `server-price-tracker` (API server) and `spt` (CLI client).

**Current state:** MVP implementation is complete. All handlers use Huma v2 typed input/output structs with runtime OpenAPI spec generation. The `spt` CLI client consumes the HTTP API via Cobra + Viper. Scheduler state, extraction queue, and rate limiter state are DB-backed (migrations 002-006). Baselines use active listing prices as a proxy since the eBay Browse API only returns active listings (migration 007). Stale unextracted listings are soft-deactivated via an `active` flag (migration 008). Alerts gained a `dismissed_at` column for the alert review UI (migration 009). Documentation is managed via `docz` CLI — see `docs/design/`, `docs/impl/`, `docs/rfc/`.

## Git Workflow

**Never commit directly to `main`.** Always create a feature branch for changes (e.g., `fix/condition-normalization`, `feat/new-endpoint`, `chore/update-deps`). Push the branch and open a PR for review.

## Build & Development Commands

Tool versions are managed via `mise.toml` — run `mise install` to set up the toolchain (Go 1.25.7, golangci-lint 2.8.0, Helm 3.19.0, helm-ct, helm-cr, helm-diff, helm-docs, yamllint, yamlfmt, markdownlint-cli2, actionlint, etc.). The `helm-unittest` plugin is installed separately via `helm plugin install https://github.com/helm-unittest/helm-unittest.git`.

The build system uses a modular Makefile structure: root `Makefile` includes domain-specific files from `scripts/makefiles/` (common.mk, go.mk, docker.mk, db.mk, helm.mk, docs.mk).

```bash
# Build
make build                    # builds both server-price-tracker and spt binaries
make build-core               # or: go build -o build/bin/server-price-tracker ./cmd/server-price-tracker
make build-spt                # or: go build -o build/bin/spt ./cmd/spt

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

# Generate Postman collection with contract tests (requires running server)
make postman                  # portman fetches /openapi.json from running server

# Run Postman tests via Newman (requires running server)
make postman-test             # runs postman first, then newman

# Apply database migrations
make migrate                  # or: go run ./cmd/server-price-tracker migrate

# Docker local dev (PostgreSQL + Ollama)
make docker-up                # Start PostgreSQL and Ollama containers
make docker-down              # Stop all containers
make docker-clean             # Stop and remove containers, volumes, images
make ollama-pull              # Pull Ollama model (default: qwen2.5:3b)
make dev-setup                # Full local dev setup: docker-up + migrate + ollama-pull

# Mock eBay server (for local testing without real eBay credentials)
make mock-server              # Start mock eBay server on port 8089

# Docker image builds (via Docker Bake)
make docker-build             # Build local dev image (single-arch)
make docker-build-multiarch   # Validate multi-arch build (no push)
make docker-bake-print        # Print resolved bake config (debug)
make docker-push              # Build and push multi-arch image to registry

# Build cross-platform (via GoReleaser)
goreleaser build --snapshot --clean

# Helm chart development (see scripts/makefiles/helm.mk)
make helm-lint                # Lint the Helm chart
make helm-template            # Render chart templates with default values
make helm-template-ci         # Render with CI values (nginx stub, no probes/migration)
make helm-package             # Package chart into .tgz archive

# Helm testing
make helm-unittest            # Run helm-unittest plugin tests
make helm-test                # Run all Helm tests (lint + unit tests)

# Helm chart-testing (ct) — requires kind cluster for install
make helm-ct-lint             # ct lint (yamllint + helm lint via ct.yaml)
make helm-ct-list-changed     # List charts changed since target branch
make helm-ct-install          # Install and test charts in kind cluster

# Helm tools
make helm-docs                # Generate chart docs with helm-docs
make helm-diff-check RELEASE=spt  # Diff installed release vs local chart
make helm-cr-package          # Package chart with chart-releaser

# Repo-wide linting (see scripts/makefiles/docs.mk)
make lint-yaml                # yamllint repo YAML (excludes charts/)
make lint-yaml-charts         # yamllint chart YAML (relaxed rules)
make lint-yaml-fmt            # yamlfmt formatting check (no modify)
make lint-md                  # markdownlint-cli2
make lint-actions             # actionlint on GitHub Actions workflows
make lint-all                 # All linters: Go + YAML + Markdown + Actions + Helm
```

## Architecture

### API-First Design

All functionality is exposed via an Echo HTTP API with Huma v2 typed handlers (`/api/v1/*`). The `spt` CLI is a remote client to this API. Huma generates the OpenAPI 3.1 spec at runtime from Go structs — no annotation-based code generation step.

```
eBay Browse API → Ingestion Loop → LLM Extract (Ollama/Claude) → PostgreSQL → Scorer → Alert → Discord Webhook
                                                                                        ↓
                                                                Echo + Huma API ← spt CLI Client
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
| `Notifier` | `internal/notify` | `DiscordNotifier`, `NoOpNotifier`, `MockNotifier` |

### Pipeline

1. **Watches** define saved searches with component type, filters, and score threshold
2. **Ingestion** polls eBay per watch on a 15-min schedule (staggered), with rate limiting (token bucket + rolling 24-hour daily quota) and per-cycle budget enforcement
3. **LLM Extraction** (two-pass + normalize): classify component type from title (server accessories like caddies/rails/bezels route to `other`), then extract component-specific attributes using the configured backend. A pre-validation **`NormalizeExtraction`** pass (`pkg/extract/normalize.go`) repairs common LLM mistakes — capacity unit confusion (`32GB` returned as `32768`), placeholder enum values (`"N/A"`, `"unknown"`), out-of-range `speed_mhz` recovered from PC4 markers, missing `confidence` defaulted to 0.5. Anthropic responses are de-fenced (markdown ```` ``` ```` code blocks stripped) before JSON parse. See `docs/EXTRACTION.md`.
4. **Product Key** generation normalizes attributes for baseline grouping (e.g., `ram:ddr4:ecc_reg:32gb:2666`)
5. **Scoring** computes a weighted 0–100 composite score (price 40%, seller 20%, condition 15%, quantity 10%, quality 10%, time 5%). Baselines are computed from active listing prices within a 90-day window (`updated_at`-based). Price factor defaults to neutral 50 when baseline has insufficient samples (cold start).
6. **Alerts** fire when score >= watch threshold and filters match; sent as Discord webhook rich embeds

### Project Layout

```
cmd/server-price-tracker/     humacli entry point (serve, migrate, version)
cmd/spt/                      Cobra + Viper CLI client (watches, listings, search, etc.)
configs/                      YAML configuration files
  config.example.yaml         Template with env var placeholders
  config.dev.yaml             Local development configuration
portman/                      Portman config for Postman collection generation
  portman-config.json         Contract test configuration
  environments/dev.json       Dev environment variables
migrations/                   PostgreSQL schema migrations (source of truth)
internal/store/migrations/    Embedded copy for Go embed.FS
scripts/makefiles/            Modular Makefile includes (common, go, docker, db, helm, docs)
tools/mock-server/            Mock eBay API server for local dev (JSON fixtures)
test/                         Top-level test directories
  e2e/                        End-to-end tests (//go:build e2e)
  integration/                Cross-cutting integration tests (//go:build integration)
  utils/                      Shared test utilities
scripts/docker/               Docker Compose for local dev (PostgreSQL + Ollama)
deploy/                       Kubernetes manifests (Kustomize base + overlays)
  argocd/                     ArgoCD Application manifest
  base/                       Base Kustomize resources
  overlays/dev/               Dev overlay (debug logging, lower resources)
  overlays/prod/              Prod overlay (info logging, higher resources)
charts/server-price-tracker/  Helm chart (alternative to Kustomize deploy/)
docker-bake.hcl               Docker Bake build definitions (dev, ci, release targets)
docs/                         Design and implementation documentation (managed via docz CLI)
  design/                     Architecture and design docs (DESIGN-NNNN)
  impl/                       Implementation plans with tasks (IMPL-NNNN)
  rfc/                        Proposals and plans (RFC-NNNN)
  adr/                        Architecture decision records (ADR-NNNN)
  plans/                      Legacy migration and feature plans (pre-docz)
```

**Exported (`pkg/`)** — importable by external tools:
- `pkg/types/` (package `domain`) — Core domain types: `Listing`, `Watch`, `WatchFilters`, `PriceBaseline`, `Alert`, `ScoreBreakdown`. Enums: `ComponentType`, `Condition`, `ListingType`. Contains `WatchFilters.Match()`.
- `pkg/scorer/` (package `score`) — Composite scoring with `Score(ListingData, *Baseline, Weights) Breakdown`. Decoupled from DB models via `ListingData` input struct.
- `pkg/extract/` — `LLMBackend` and `Extractor` interfaces, implementations (Ollama, Anthropic, OpenAI-compatible), extraction orchestrator, prompt templates, response validation.

**Internal (`internal/`)** — application-specific, not importable:
- `internal/api/` — Echo HTTP server with Huma v2 typed handlers, middleware (Prometheus metrics, request logging, panic recovery), API client for CLI
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

- `GET /openapi.json` — OpenAPI 3.1 spec (generated at runtime by Huma)
- `GET /docs` — Huma interactive API docs UI
- `GET /healthz`, `GET /readyz`, `GET /metrics` — operational
- `GET/POST /api/v1/watches` — watch CRUD (6 endpoints)
- `GET /api/v1/listings` — query with filters (component, score, price)
- `POST /api/v1/search` — eBay search
- `POST /api/v1/extract` — one-off LLM extraction test
- `POST /api/v1/ingest` — trigger manual ingestion
- `POST /api/v1/baselines/refresh` — recompute baselines
- `POST /api/v1/rescore` — rescore all listings
- `POST /api/v1/reextract` — re-extract listings with incomplete data (optional type/limit)
- `GET /api/v1/extraction/stats` — extraction quality statistics (incomplete counts by type)
- `GET /api/v1/quota` — eBay API quota status (daily usage, remaining, reset time)
- `GET /api/v1/system/state` — system health metrics (listing/baseline/alert counts)
- `GET /api/v1/jobs` — scheduler job run history
- `GET /api/v1/jobs/{job_name}` — job runs for a specific job

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
- **Manifests:** Kustomize (base + overlays for dev/prod) in `deploy/`, or Helm chart in `charts/server-price-tracker/`
- **Helm chart:** Production-ready with optional CNPG PostgreSQL (`cnpg.enabled`), Ollama StatefulSet (`ollama.enabled`), Prometheus ServiceMonitor (`serviceMonitor.enabled`), create-or-reference secret pattern, and migration init container
- **Ingress:** Cilium API Gateway (Gateway API HTTPRoute)
- **Observability:** Prometheus ServiceMonitor scraping `/metrics`, Grafana dashboards. LLM token usage is emitted per-backend at `spt_extraction_tokens_total{backend, model, direction}` and `spt_extraction_tokens_per_request{backend, model}` — counters reflect billed tokens (including unparseable responses); dollar conversion is a PromQL concern. See `docs/OPERATIONS.md` ("LLM Token Metrics") for the metric reference and PromQL examples.
- **Secrets:** Kubernetes Secrets (sealed-secrets or external-secrets-operator), not in manifests

## Docker & CI

- **Docker Bake:** `docker-bake.hcl` is the single source of truth for image builds. Three targets: `dev` (local single-arch), `ci` (multi-arch validation), `release` (multi-arch push to GHCR)
- **CI workflow** (`.github/workflows/ci.yml`): lint, test with coverage, security scan (govulncheck + Trivy), GoReleaser snapshot build, Docker Bake multi-arch validation, Helm chart lint + install testing via chart-testing-action (kind cluster)
- **Release workflow** (`.github/workflows/release.yml`): PR labels (`major`, `minor`, `patch`) drive `pr-semver-bump` to create a git tag on merge to `main`. Then: GoReleaser release, Docker multi-arch build+push with metadata-action tags, and automatic Helm chart versioning — CI bumps `Chart.yaml` `appVersion` to match the release tag and auto-increments the chart `version` patch number. **Do not manually bump `Chart.yaml` version or appVersion; CI owns both.** Charts are published to GitHub Pages via chart-releaser.
- **Chart testing:** `ct.yaml` configures chart-testing-action. `charts/.yamllint.yml` and `charts/.yamlfmt.yml` provide chart-specific YAML lint/format rules. `charts/server-price-tracker/ci/ci-values.yaml` provides CI install overrides (nginx stub image, no probes/migration). Helm unit tests use `helm-unittest` plugin in `charts/server-price-tracker/tests/`
- **Helm repo:** Charts are published to GitHub Pages at `https://donaldgifford.github.io/server-price-tracker/` via chart-releaser-action. Add with `helm repo add spt https://donaldgifford.github.io/server-price-tracker/`
- **Security workflow** (`.github/workflows/security.yml`): scheduled weekly govulncheck with SARIF upload to GitHub Code Scanning

## Design Documentation

Documentation is managed via `docz` CLI (`.docz.yaml` config at repo root). Run `docz list` to see all tracked documents, `docz create <type> <title>` to create new ones. Original pre-docz docs remain in place; docz versions live in `docs/design/`, `docs/impl/`, `docs/rfc/`.

Key documents (docz-tracked):
- `DESIGN-0001` — Server Price Tracker Architecture (full architecture, interfaces, data model)
- `DESIGN-0002` — LLM Extraction Pipeline (prompts, grammars, validation, product keys)
- `DESIGN-0003` — Helm Chart Testing and Releasing CI/CD
- `DESIGN-0004` — Inactive Listings Lifecycle (active flag, soft-deactivation)
- `IMPL-0001` — MVP Build Plan (10-phase implementation)
- `IMPL-0005` — Codebase Refactoring (in progress)
- `RFC-0001` — Migrate from Swaggo to Huma v2

Key documents (legacy, pre-docz):
- `docs/DESIGN.md` — Original architecture document
- `docs/IMPLEMENTATION.md` — Original MVP implementation plan
- `docs/EXTRACTION.md` — LLM backend options, prompts, grammars, normalization rules
- `docs/OPERATIONS.md` — Operations guide for setup and running, includes reextract / backfill runbook
- `docs/SQL_HELPERS.md` — Catalog of psql queries for diagnosis and backfill (extraction queue snapshot, reactivating stuck listings, etc.)
