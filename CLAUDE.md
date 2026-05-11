# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Server Price Tracker is an API-first Go service that monitors eBay listings for server hardware deals (RAM, drives, servers, CPUs, NICs, GPUs). It extracts structured attributes via LLM (Ollama default, Anthropic Claude API optional), scores listings against historical price baselines, and sends deal alerts via Discord webhooks. Two binaries: `server-price-tracker` (API server) and `spt` (CLI client).

**Current state:** MVP implementation is complete. All handlers use Huma v2 typed input/output structs with runtime OpenAPI spec generation. The `spt` CLI client consumes the HTTP API via Cobra + Viper. Scheduler state, extraction queue, and rate limiter state are DB-backed (migrations 002-006). Baselines use active listing prices as a proxy since the eBay Browse API only returns active listings (migration 007). Stale unextracted listings are soft-deactivated via an `active` flag (migration 008). Alerts gained a `dismissed_at` column for the alert review UI (migration 009). GPU added to the `component_type` CHECK constraints on `watches` and `listings` (migration 010); workstation+desktop added in migration 011. Documentation is managed via `docz` CLI — see `docs/design/`, `docs/impl/`, `docs/rfc/`. **Dev and prod share the same Postgres database** — `spt-dev.fartlab.dev` and `spt.fartlab.dev` are two app deployments pointing at the same data. Re-queue/re-extract runs done "in dev" actually update the live prod dataset; Phase 7 backfill is usually moot once Phase 5 dev validation completes. Image deploy is the only real difference between the two deploys.

**Notification & alert review (DESIGN-0008/0009/0010, IMPL-0015):** The Discord notifier chunks batches into ≤10-embed POSTs, parses `X-RateLimit-*` headers for bucket-aware sleeps, and retries non-global 429s once. `Notifier.SendBatchAlert` returns `(sent int, err error)` so the engine records per-ID delivery outcomes (succeeded=true for the first `sent`, false for the rest). The alert review UI at `/alerts` is templ + HTMX + Alpine, gated by `config.web.enabled`. Discord can be flipped to summary mode via `config.notifications.discord.summary_only`: each scheduler tick produces one embed linking back to `<web.alerts_url_base>/alerts`, with the dashboard becoming the work surface. The `spt watches update` subcommand patches watches in place (threshold, filters, etc.) — see `docs/cli/spt_watches_update.md`.

**GPU support (DESIGN-0012, IMPL-0017, merged in PR #47):** GPUs are a first-class `ComponentType` (`gpu`). Product key is `gpu:<manufacturer>:<family>:<model>:<vram>gb`. The `model` field is canonicalised by `CanonicalizeGPUModel` — strips leading `rtx[_\s-]?` brand prefix and normalises Ti/Super separators so `RTX 3090`, `rtx_3090`, `rtx3090`, and `3090` all collapse to `3090` (and `3090_ti`/`3090ti` collapse to `3090ti`); `3090` and `3090ti` stay distinct because they're separate SKUs. For known canonical models (P40/P100/V100/K80/M40/M60/T4 → tesla; A10/A30/A40/A100 → a-series; L4/L40/L40S → l-series; H100/H200 → h-series; MI50-300 → instinct; 2/3/4/5-series → geforce-rtx; A2000/A4000/A5000/A6000 → quadro-rtx) `DetectGPUFamilyFromModel` *overrides* whatever the LLM put in the family field — the LLM is non-deterministic between legacy brand ("Tesla") and architectural family ("Ampere"/"A-series") and would fragment baselines. For ambiguous models (P4000, RTX 4000) the LLM-supplied family is canonicalised by `CanonicalizeGPUFamily` instead. Required attributes are manufacturer + vram_gb; family/model start optional and tighten as the dataset matures. The pre-classifier extends `primaryComponentPatterns` with GPU brand/family tokens (tesla/quadro/rtx a-series/a100/h100/l40/mi-series/radeon pro) — these are guards against the accessory short-circuit (so "Tesla P40 + heatsink" defers to the LLM rather than being routed to `other`); the actual gpu/server/ram/etc. classification still happens in the LLM classifier. Operator runbook: GPU watch threshold defaults to 65 (cold-start) and bumps to 80 once at least one `gpu:%` baseline reaches `sample_size ≥ 10` — see `docs/SQL_HELPERS.md` ("GPU baseline maturity check"). Backfill helper for `other`-bucketed historical GPUs is documented in the same file.

**Workstation + desktop support (DESIGN-0015, IMPL-0018, merged in PR #49 as v0.8.0):** Workstations and desktops are peer ComponentTypes alongside `server`/`gpu`/etc. — separate buckets so they don't pollute server baselines. Product keys are `workstation:<vendor>:<line>:<model>` and `desktop:<vendor>:<line>:<model>` — same pre-tier shape as servers. `pkg/extract/system_normalize.go::NormalizeSystemExtraction` is the single entry point (Open Q4) that runs vendor canonicalisation (Dell Inc. / Hewlett-Packard / IBM-Lenovo → canonical lowercase), line canonicalisation (Precision Tower / Z by HP / PROMAX → canonical), brand-prefix stripping on model ("Lenovo P620" → p620), and conservative line-from-model inference for known SKU patterns (T-series → precision, P-series → thinkstation, Z\d+ G\d+ → z-by-hp, M-series → thinkcentre, OptiPlex/EliteDesk/ProDesk prefix). Per Open Q5 "Pro Max" stays distinct from "precision" so the post-rebrand pricing curve is separable. Required attributes are vendor + model; line is optional with normaliser-fill. **`systemServerLineDenylist`** drops LLM hallucinations on the line field (PowerEdge/ProLiant/UCS/Supermicro/Dell-EMC/System-x → fall through to model inference); surfaced when 31 Dell Precision T3620 listings landed at `workstation:dell:poweredge:t3620` instead of joining the 103-sample `precision:t3620` baseline. Three pre-class hooks now run before the LLM in `(*LLMExtractor).ClassifyAndExtract`, in order: (1) `DetectSystemTypeFromTitle` — chassis token (precision T-pattern, ThinkStation, HP Z\d, Pro Max, OptiPlex, EliteDesk, ThinkCentre, ProDesk) AND a system-completeness signal (cores/GHz/Xeon/Gold xxxx/i\d-xxxx/Win10-11/RAM-storage spec) routes to workstation/desktop. Runs FIRST so it overrides both `IsAccessoryOnly`'s "power cable" compound short-circuit AND the LLM classifier's tendency to route ThinkStation/HP Z systems to `server`. (2) `IsAccessoryOnly` (existing). (3) `DetectSystemTypeFromSpecifics` inspects eBay item specifics (`Most Suitable For`, `Series`, `Product Line`, `Form Factor`) — Open Q1. Pre-classifier `primaryComponentPatterns` also extended with workstation/desktop chassis tokens; ambiguous "Dell Pro" requires desktop form-factor co-token to avoid "Dell ProSupport" false positives (Open Q6). Classifier prompt gained "servers stay rack-mountable" + "bundled GPU stays with workstation/desktop, not gpu" rules (Q1 single-classification ruling). Operator runbook: cold-start threshold 65, bump to 80 per-watch as each product key reaches `sample_count ≥ 10` — see `docs/SQL_HELPERS.md` ("Workstation/desktop baseline maturity check"). Phase 7 backfill SQL documented in the same file (skipped for IMPL-0018 because dev/prod share the same DB — Phase 5 re-extracts already cleaned prod). Three known follow-ups parked: BOXX APEXX brand support, `dell t\d{4}` chassis token without "Precision" prefix, Threadripper Pro custom builds without vendor chassis.

**Adding a new ComponentType — eight touchpoints to keep in sync:** (1) `pkg/types/types.go` const; (2) `pkg/extract/extractor.go` `validComponentTypes` allowlist; (3) `pkg/extract/prompts.go` extraction template + classifier rules; (4) `pkg/extract/preclassify.go` `primaryComponentPatterns` regex; (5) `pkg/extract/productkey.go` switch case; (6) `pkg/extract/validate.go` validator; (7) `pkg/extract/normalize.go` switch arm; (8) `migrations/NNN_*.sql` and embedded copy — drop + re-add CHECK constraints on `watches.component_type` and `listings.component_type` (Postgres can't add to CHECK in place). Missing the migration triggers `SQLSTATE 23514` on `spt watches create --type <new>`. The seven Go touchpoints + one DB migration is the IMPL-0017 / IMPL-0018 phase-plan shape.

**Baseline refresh leaves orphan rows when product_keys change.** `recompute_baseline` (migration 008) only INSERT-UPDATEs; it never deletes baselines whose `product_key` no longer matches any active listing. Whenever a normaliser update changes how `product_key` is generated (server tier suffix in IMPL-0016, GPU model canonicalisation in IMPL-0017, future ComponentType additions), old baseline rows linger as zombies with stale `sample_count`. After any normaliser change, manually clean up: `DELETE FROM price_baselines WHERE product_key NOT IN (SELECT DISTINCT product_key FROM listings WHERE active = true AND product_key IS NOT NULL);`. A real fix to refresh logic is parked as a follow-up PR.

## Git Workflow

**Never commit directly to `main`.** Always create a feature branch for changes (e.g., `fix/condition-normalization`, `feat/new-endpoint`, `chore/update-deps`). Push the branch and open a PR for review.

**PR descriptions: never pipe Markdown bodies through `gh pr create --body "..."`.** The shell pre-escapes backticks (and sometimes `$`), so triple-backtick code fences and inline `code` render as literal `` \` `` in the GitHub UI — the description shows up as a wall of escaped text instead of formatted Markdown. Write the body to a temp file first and pass `--body-file`:

```bash
cat > /tmp/pr-body.md <<'EOF'
## Summary
...body with `inline code` and ```code blocks``` renders correctly...
EOF
gh pr create --title "..." --body-file /tmp/pr-body.md
```

Same gotcha applies to `gh pr edit --body` and `gh issue create --body`. If a PR already shipped with escaped backticks, fix it with `gh pr edit <num> --body-file <path>`.

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

# Generate templ components (alert review UI at /alerts)
make templ-generate           # or: templ generate
make templ-watch              # rebuild on change during development

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
3. **LLM Extraction** (regex pre-pass + two-pass LLM + normalize):
   `pkg/extract/preclassify.go::IsAccessoryOnly` is a deterministic title regex that short-circuits the LLM for bare server-part listings (backplanes, caddies, rails, bezels, fans, cables, …) — they route directly to `ComponentOther` with `extraction_confidence=0.95`. Mixed titles that hit both an accessory and a primary-component keyword (DDR4/NVMe/Xeon/form factor) defer to the LLM. Then classify component type from title (server accessories the regex missed still route to `other`), then extract component-specific attributes using the configured backend. A pre-validation **`NormalizeExtraction`** pass (`pkg/extract/normalize.go`) repairs common LLM mistakes — capacity unit confusion (`32GB` returned as `32768`), placeholder enum values (`"N/A"`, `"unknown"`), out-of-range `speed_mhz` recovered from PC4 markers, missing `confidence` defaulted to 0.5. Anthropic responses are de-fenced (markdown ```` ``` ```` code blocks stripped) before JSON parse. See `docs/EXTRACTION.md`.
4. **Product Key** generation normalizes attributes for baseline grouping (e.g., `ram:ddr4:ecc_reg:32gb:2666`). Server keys include a `tier` suffix (`server:dell:r740xd:sff:barebone|partial|configured`) so chassis-only shells get their own baseline instead of competing with fully-configured systems — see `pkg/extract/server_tier.go`.
5. **Scoring** computes a weighted 0–100 composite score (price 40%, seller 20%, condition 15%, quantity 10%, quality 10%, time 5%). Baselines are computed from active listing prices within a 90-day window (`updated_at`-based). Price factor defaults to neutral 50 when baseline has insufficient samples (cold start). The `priceScore` curve was recalibrated in DESIGN-0011 — P25 → 70, P50 → 30, P75 → 10 (was 85/50/25) — so the median listing's composite score is ~60 instead of the prior ~88 noise floor that fired alerts on every listing.
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
tools/dashgen/                Grafana dashboard + Prometheus rules generator (Go-defined panels)
tools/regression-runner/      Classifier accuracy gate (`make test-regression`)
tools/dataset-bootstrap/      Stratified live-DB sample → editable golden dataset
tools/dataset-upload/         Upload golden dataset to Langfuse (idempotent, title-hash IDs)
tools/judge-bootstrap/        Cold-start CLI for labelling judge few-shot examples
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
- `pkg/observability/langfuse/` — In-house Langfuse HTTP client (traces/generations/scores/dataset items+runs), buffered async client, MockClient. Optional `DatasetItem.ID` enables idempotent upserts so `tools/dataset-upload` is safe to re-run.
- `pkg/judge/` — `Judge` interface + `LLMJudge` (prompt template + few-shot `examples.json`), `Worker` with daily-budget enforcement, `Score` ↔ `Verdict` plumbing. Persists to Postgres (`judge_scores`, migration 013) and Langfuse (best-effort).

**Internal (`internal/`)** — application-specific, not importable:
- `internal/api/` — Echo HTTP server with Huma v2 typed handlers, middleware (Prometheus metrics, request logging, panic recovery), API client for CLI. Also hosts the alert review UI at `/alerts` (DESIGN-0010): server-rendered HTML via [templ](https://templ.guide) components in `internal/api/web/components/`, [HTMX](https://htmx.org) 1.9 for swap-in-place interactions, [Alpine.js](https://alpinejs.dev) 3.14 for small reactive bits. Generated `*_templ.go` files are gitignored — `make build` runs `templ generate` first. Static assets (htmx.min.js, alpine.min.js, spt.css) ship via `go:embed` in `internal/api/web/embed.go`.
- `internal/store/` — `Store` interface (datastore abstraction) + `PostgresStore` implementation (raw SQL with pgx, no ORM)
- `internal/engine/` — Ingestion loop, baseline recomputation, alert evaluation, scheduler. Takes all dependencies as interfaces.
- `internal/ebay/` — `EbayClient` and `TokenProvider` interfaces + implementations
- `internal/notify/` — `Notifier` interface + `DiscordNotifier` implementation
- `internal/config/` — YAML config loader with `os.ExpandEnv()` for secrets
- `internal/observability/` — OTel SDK bootstrap (OTLP/gRPC trace + metric exporters), tracer/meter providers, propagators. No-op when `observability.otel.enabled=false`.
- `internal/regression/` — Shared types (`Item`, `TitleHash`, `LoadDataset`) used by all four Phase 6 operator CLIs. `sha256-trunc-8(title)` ID convention is defined here once so `tools/dataset-upload` (DatasetItem.ID) and `tools/regression-runner` (DatasetRunItem.DatasetItemID) align in Langfuse without out-of-band coordination.

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
- `GET /api/v1/alerts/{id}/trace` — Langfuse trace deep-link for an alert (returns `{trace_url}`); off when `observability.langfuse.enabled=false`
- `POST /api/v1/judge/run` — manually trigger a judge worker pass over recent un-scored alerts (operator backfill)

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

## Observability (DESIGN-0016 / IMPL-0019)

The OTel + Clickhouse + Langfuse stack is **fully optional** — three independently disable-able config subtrees, all default off. With every flag false the binary's external behaviour is byte-identical to the pre-IMPL-0019 deployment shape.

```yaml
observability:
  otel:       { enabled: false, endpoint: "", service_name: "", insecure: false, timeout: 10s }
  langfuse:   { enabled: false, endpoint: "", public_key: "", secret_key: "", buffer_size: 1000, model_costs: {} }
  judge:      { enabled: false, backend: "", model: "", interval: 15m, lookback: 6h, batch_size: 50, daily_budget_usd: 10 }
```

Phase-by-phase scaffolding:

- **Phase 1-2** (OTel SDK + pipeline spans): `internal/observability/otel.go` boots OTLP/gRPC exporters; `pkg/extract/extractor.go` and `internal/engine/scheduler.go` start root + child spans tagged with `spt.backend`, `spt.component.type`, `spt.llm.tokens.*`, etc. Trace IDs propagate via `extraction_queue.trace_id` and `alerts.trace_id` (migration 012, NULLIF coercion in store layer). Two OTel histograms (`spt.extraction.duration`, `spt.alert.eval.duration`) live in `pkg/extract/meter.go` and `internal/engine/meter.go` — lazily registered via `sync.OnceValue` so the global MeterProvider can be set by `observability.Init` after package import.
- **Phase 3** (Langfuse): `pkg/observability/langfuse` — in-house HTTP client + buffered client (drops oldest on overflow), `pkg/extract/langfuse_backend.go` decorates LLMBackend per Generate call. `extraction_self_confidence` Langfuse score is auto-pushed on every successful Extract. Per-model rate table (`langfuse.ModelCost`) feeds CostUSD when configured; empty map → Langfuse server-side rates apply.
- **Phase 4** (Alert review UI): `GET /api/v1/alerts/{id}/trace` returns `{trace_url}`; `AlertRow` + `AlertDetailPage` render Trace ↗ deep-links via the shared `TableOptions{LangfuseEndpoint, JudgeEnabled}` struct. Dismiss actions emit `langfuse.Score(traceID, "operator_dismissed", 1.0, "")`; restore actions emit `0.0` symmetrically (`Store.RestoreAlerts` returns trace IDs alongside the row count, just like `DismissAlerts`) so the judge regression set sees explicit positive labels on retraction rather than relying on absence.
- **Phase 5** (Judge): `pkg/judge` — `Judge` interface, `LLMJudge` (prompt + few-shot in `judge_prompt.tmpl` + `examples.json`), `Worker` with daily-budget enforcement (mid-batch rechecks, `ErrJudgeBudgetExhausted` exit). Migration 013 owns `judge_scores`. Cron entry registered via `Scheduler.AddJudge`; `POST /api/v1/judge/run` + `spt judge run` for on-demand backfill. Verdicts persist to Postgres (durable) and Langfuse (best-effort). Cold-start labelling: `go run ./tools/judge-bootstrap --config <path>` emits a stratified, interleaved JSON queue of recent alerts (empty `label` + `verdict`); operator fills in `deal`/`edge`/`noise` labels with score+reason, then `--apply labelled.json` validates and writes `pkg/judge/examples.json`.
- **Phase 7** (Grafana): `tools/dashgen/panels/observability.go` adds the Observability row — `JudgeScoreDistribution` (heatmap of `spt_judge_score_bucket`), `JudgeVsOperatorAgreement` (judge "noise" verdict rate vs `spt_alerts_dismissed_total`), `JudgeCostByModel` (per-model `spt_judge_cost_usd_total`), and `PipelineStageVolume` (proxy for OTel span volume from histogram `_count` rates across ingestion / extraction / alerts query / notification). Spec'd `LangfuseGenerationCost` and `TraceVolumeByPipelineStage` panels are deferred — both depend on Langfuse-side polling or OTel-derived span counters that don't have Prometheus surfaces yet. Dashgen test asserts 8 rows and 38 inner panels; new metric names registered in `tools/dashgen/config.go::KnownMetrics`.

**Regression test workflow (Phase 6):** Any PR that touches `pkg/extract/prompts.go`, `pkg/extract/preclassify.go`, `pkg/extract/normalize.go`, or any classifier/normaliser logic must run `make test-regression` against the configured backend and paste the per-component accuracy lines into the PR description. The PR template (`.github/PULL_REQUEST_TEMPLATE.md`) includes a checkbox for this. Three operator CLIs make up the Phase 6 toolchain — all share `internal/regression` (`Item` struct, `TitleHash`, `LoadDataset`) so the dataset shape and ID derivation are defined once. The `sha256-trunc-8(title)` convention means runs and items align in Langfuse without out-of-band coordination:

- `tools/dataset-bootstrap` — pulls stratified live-DB sample, pre-fills `expected_component` / `expected_product_key` from current LLM labels for in-place audit (not label-from-scratch).
- `tools/dataset-upload` — POSTs one `DatasetItem` per row to Langfuse with explicit title-hash IDs, idempotent under re-runs (Langfuse upserts on `id`). Requires `--langfuse-dataset-id <id>`.
- `tools/regression-runner` (= `make test-regression`) — runs the dataset against the configured backend, prints accuracy table or JSON, supports `--backends` for side-by-side comparison (`$/1k extractions` is `—` until per-extraction token usage is surfaced through `LLMExtractor`'s return path). With `--langfuse-dataset-id <id>` and Langfuse enabled, posts one `CreateDatasetRun` per backend named `classify_prompt:<sha>:<backend>`; SHA defaults to `git rev-parse HEAD`.

Build-tagged `pkg/extract/regression_test.go` lives alongside as a documentation stub for the JSON shape. The full first-time operator pipeline is documented in `docs/OPERATIONS.md` §8 ("Quarterly dataset relabelling").

Adding a new judge metric, span attribute, or pre-classification hook: keep all three optional flags in mind — code paths must remain free of behaviour changes when each is disabled. The disabled-mode guarantee is enforced by the `make test` suite running with all three off (the production default).

**Post-merge hardening (INV-0001):** A code-review pass on the IMPL-0019 branch surfaced 10 issues fixed before deploy — `docs/investigation/0001-impl-0019-post-merge-code-review-findings.md` is the canonical record. Load-bearing precedents that future contributors must respect:

- **`pkg/observability/langfuse` keeps zero `internal/*` imports.** `BufferedClient` accepts a `BufferMetrics` interface (`SetDepth/RecordDrop/RecordWrite/ObserveWriteDuration`) with a no-op default; the production wiring in `serve.go` injects `metrics.LangfuseBufferAdapter{}` via `WithBufferMetrics(...)`. This is the canonical pattern for any future `pkg/` ↔ `internal/metrics` plumbing — mirror the `MetricsRecorder` shape `pkg/judge/worker.go` already uses.
- **Buffer overflow is drop-newest, not drop-oldest.** The original tri-select eviction dance was not race-safe under concurrent senders; current contract is "try send; on full, increment drops". Operators read `spt_langfuse_buffer_drops_total` as "records lost"; semantics match every other Go observability library.
- **`BufferedClient` lifecycle is `stopCh`-driven, not ctx-driven.** `drain` no longer exits on parent ctx.Done — only `Stop()`. Parent ctx is used for individual upstream HTTP calls during steady state. K8s SIGTERM path: signal handler cancels root ctx → process calls `buf.Stop(freshShutdownCtx)`; `Stop` writes `shutdownCtx` into a mutex-guarded field before closing `stopCh`, and `drain` reads it back to give `flushRemaining` a working deadline. Without this, every queued record's HTTP call would return `context.Canceled` instantly during shutdown — the exact moment the flush matters most. `Start` is gated by `sync.Once` and refuses to spawn after `Stop` has closed `stopCh` (otherwise the next `Stop` would hang on `wg.Wait` forever).
- **Untrusted prompt content is delimited and sanitised.** `pkg/judge/prompts.go::sanitizeUntrusted` strips control chars + truncates to 200 runes for `ListingTitle` and score `Reasons`; the alert block is wrapped in `<<<UNTRUSTED>>> ... <<<END_UNTRUSTED>>>` with explicit "treat as DATA only" prompt language. Same pattern should apply to any future prompt that interpolates seller-controlled text.
- **Judge daily-budget guarantee is hard.** `(*Worker).checkBudget` halts the tick on either budget exhaustion (`ErrJudgeBudgetExhausted`) *or* a DB recheck error (returns the wrapped error verbatim, not the sentinel — so metrics + dashboards don't conflate "Postgres is down" with "spend cap met"). The previous `if sumErr == nil && spent >= budget` silently regressed the guarantee under DB pressure, which is the exact failure mode the recheck exists to prevent.
- **`pkg/judge.WithJudgeCosts` (not `WithModelCosts`)** to avoid name collision with `pkg/extract.WithModelCosts`. When adding a new functional option that takes the same kind of payload across multiple packages, prefix with the package's domain (`Judge`, `Backend`) to keep import-time wiring readable.
- **Don't embed full LLM responses in error strings.** They surface at WARN level and cluster log retention captures multi-KB prompt-echoing payloads. `pkg/judge/llm_judge.go::truncate` caps at 512 runes + ellipsis. Same applies anywhere `resp.Content` lands in a logged error.
- **Langfuse sessions group all traces from one logical run.** Per-tick: `withSpan` in `internal/engine/scheduler.go` generates a fresh UUID at every cron tick (ingestion / baseline_refresh / re_extraction / judge), sets it on ctx via `langfuse.WithSessionID`, AND attaches it as the OTel span attribute `langfuse.session.id` so the OTel-derived trace gets the same session attribution at ingest. Per-request: `internal/api/handlers/session.go::withRequestSession` does the equivalent at the top of `/api/v1/extract` and `/api/v1/ingest` handlers — manual API triggers surface as their own session in the UI rather than bleeding into a scheduled-tick session. The in-house Langfuse client reads ctx via `langfuse.SessionIDFromContext` and threads `sessionId` onto every `generation-create` and `trace-create` event body. Both transports converge on the same session ID. Adding new long-running operations? Either route the work through `withSpan` (gives you the session attribution for free) or call `withRequestSession` at the entry handler.
- **Langfuse writes go through `/api/public/ingestion`, not the deprecated standalone endpoints.** Langfuse v3 retained POST `/api/public/generations` / `/scores` / `/traces` only as partial-compat shims that silently drop `input` and `output` from generations — symptom is the generation rendering with model+latency+metadata populated but Input/Output null in the UI. The canonical path is the batch ingestion endpoint with per-event envelopes (`generation-create`, `score-create`, `trace-create`), client-generated UUIDs for both event ID and observation/trace ID, and **nested `usage`** (input/output/total/unit) instead of flat `promptTokens`/`completionTokens`. The ingestion API returns 200 with a per-event `{successes, errors}` envelope — `(*HTTPClient).ingest` surfaces body-level errors as a non-retryable failure so a 200 with rejection doesn't masquerade as success. `CreateTrace` now generates the trace ID client-side and returns it via `TraceHandle` (the ingestion API doesn't echo back IDs). Datasets endpoints (`/api/public/dataset-items`, `/api/public/dataset-run-items`) stay on their dedicated paths — they're a separate API surface and weren't deprecated alongside the observability triplet.

`docs/OPERATIONS.md §8` is the operator-facing runbook for OTel/Langfuse/judge config; this CLAUDE.md note is the intent + entry-points view.

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
