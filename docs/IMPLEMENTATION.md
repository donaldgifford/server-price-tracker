# Server Price Tracker — MVP Implementation Plan

## MVP Scope

The MVP delivers an API-first service (Echo HTTP server) that monitors eBay for server hardware listings, extracts structured attributes via LLM, scores them against price baselines, and sends Discord webhook notifications when deals exceed a threshold. The CLI acts as a client to the API. The architecture supports future Discord bot integration, Grafana dashboards via Prometheus metrics, and external tooling via exported packages.

### Development Methodology

**Test-Driven Development (TDD)** is the primary development approach:

- Every external dependency is abstracted behind a Go interface
- **Mockery** generates mock implementations for all interfaces
- Tests are **table-driven** using `testify/assert` and `testify/require`
- Tests are written alongside code in **every phase**, not deferred
- Each phase includes mock generation, unit tests, and success criteria that require passing tests
- Code that cannot be tested via TDD must be annotated: `// TODO(test): <explanation>`
- Target: as close to 100% coverage as practical via TDD and mockery

This approach means we can build and fully test every component before eBay API access is approved, before an LLM is running, and before Discord webhooks are configured. When external services become available, we connect real implementations to already-tested interfaces.

### What's In

- eBay Browse API integration (search + item details)
- LLM-based attribute extraction with backend abstraction (Ollama default, Anthropic Claude API optional, OpenAI-compatible)
- All five component types: RAM, drives, servers, CPUs, NICs
- Composite scoring with price percentile baselines
- PostgreSQL storage for listings, watches, baselines, alerts
- Echo HTTP API server with versioned REST endpoints
- Prometheus metrics endpoint for Grafana
- Discord webhook notifications (rich embeds)
- CLI client for the API (manages watches, inspects listings/scores)
- Cron-based scheduling (internal, not system cron)
- Health and readiness endpoints

### What's Out (v2+)

- Web UI or TUI dashboard
- Discord bot (command-based querying) — the webhook foundation supports this later
- Sold/completed listing ingestion in the main service (see baseline-seeder tool below)
- Vector search / semantic similarity
- Multi-marketplace support (UK, DE, etc.)
- User accounts / multi-tenancy

### External Tooling (separate binary, not part of main MVP but designed for)

- `tools/baseline-seeder` — imports `server-price-tracker/pkg/*` to bootstrap baselines from eBay Finding API completed/sold listings. Addresses the cold-start problem.

### Resolved Decisions

1. **eBay developer account** — Registration submitted, awaiting approval. Free tier (5k calls/day) is sufficient for MVP. TDD with mocked eBay client means we don't block on this.
2. **Sold listings strategy** — Not part of the main service. A separate `tools/baseline-seeder` tool will use the eBay Finding API (different auth) to bootstrap baselines. The main service uses active listing prices as approximate baselines until sold data is seeded. Cold-start price factor defaults to neutral 50.
3. **LLM backend** — Ollama is the default (Mistral 7B Q4/Q5 or llama3.1:8b). Anthropic Claude API (Haiku or any configurable model) is supported as an alternative. Backend is abstracted behind an interface. Not defaulting to Claude API.
4. **Notifications** — Discord webhooks (rich embeds with images, score breakdowns, eBay links). No ntfy. Webhook foundation enables future Discord bot.
5. **Baseline cold start** — Price factor defaults to neutral 50 when baseline has < min_baseline_samples. The `tools/baseline-seeder` can be run to import completed/sold listings for specific categories. It imports `pkg/*` packages from this repo, keeping the main codebase clean.
6. **Deployment** — Talos Linux Kubernetes cluster, ArgoCD for GitOps, Kustomize manifests, Cilium API Gateway for ingress. No Docker Compose for the full stack.

---

## Phase 0: Project Bootstrap

**Goal:** Compilable Go project with working dependency tree, config loading, mockery setup, project structure, and the foundation for TDD across all phases.

### Tasks

- [x] **0.1 — Initialize Go module**
  - Run `go mod init github.com/donaldgifford/server-price-tracker`
  - Add core dependencies:
    | Package | Purpose |
    |---|---|
    | `github.com/spf13/cobra` | CLI framework |
    | `github.com/labstack/echo/v4` | HTTP server |
    | `github.com/jackc/pgx/v5` | Postgres driver |
    | `github.com/jackc/pgx/v5/pgxpool` | Connection pooling |
    | `github.com/charmbracelet/log` | Structured logging |
    | `github.com/robfig/cron/v3` | Internal scheduler |
    | `gopkg.in/yaml.v3` | Config parsing |
    | `github.com/google/uuid` | UUID generation |
    | `github.com/prometheus/client_golang` | Prometheus metrics |
  - Add test dependencies:
    | Package | Purpose |
    |---|---|
    | `github.com/stretchr/testify` | Assert/require/mock |
    | `github.com/vektra/mockery/v2` | Mock generation (go install) |
  - Run `go mod tidy`

- [x] **0.2 — Mockery configuration**
  - Create `.mockery.yaml` at repo root:
    ```yaml
    with-expecter: true
    dir: "{{.InterfaceDir}}/mocks"
    outpkg: "mocks"
    filename: "mock_{{.InterfaceName | snakecase}}.go"
    mockname: "Mock{{.InterfaceName}}"
    packages:
      github.com/donaldgifford/server-price-tracker/internal/store:
        interfaces:
          Store:
      github.com/donaldgifford/server-price-tracker/internal/ebay:
        interfaces:
          EbayClient:
          TokenProvider:
      github.com/donaldgifford/server-price-tracker/internal/notify:
        interfaces:
          Notifier:
      github.com/donaldgifford/server-price-tracker/pkg/extract:
        interfaces:
          LLMBackend:
          Extractor:
    ```
  - Add `make mocks` target to Makefile:
    ```makefile
    mocks:
    	mockery
    ```
  - Add generated mock directories to `.gitignore` or commit them (project preference)

- [x] **0.3 — Project structure**
  - Create the directory layout:
    ```
    server-price-tracker/
    ├── cmd/server-price-tracker/
    │   ├── main.go                 # entry point, cobra root
    │   └── serve.go                # serve command (starts API + scheduler)
    ├── internal/
    │   ├── api/
    │   │   ├── server.go           # Echo server setup, middleware, routes
    │   │   ├── handlers/
    │   │   │   ├── watches.go      # watch CRUD handlers
    │   │   │   ├── listings.go     # listing query handlers
    │   │   │   ├── baselines.go    # baseline handlers
    │   │   │   ├── operations.go   # ingest, rescore, extract, search
    │   │   │   └── health.go       # healthz, readyz
    │   │   └── middleware/
    │   │       └── metrics.go      # Prometheus HTTP middleware
    │   ├── config/
    │   │   └── config.go           # config struct + YAML loader
    │   ├── ebay/
    │   │   ├── client.go           # EbayClient interface + BrowseClient implementation
    │   │   ├── auth.go             # TokenProvider interface + OAuthTokenProvider
    │   │   └── types.go            # API response structs
    │   ├── engine/
    │   │   ├── ingest.go           # ingestion pipeline (depends on interfaces)
    │   │   ├── baseline.go         # baseline recomputation
    │   │   ├── alert.go            # alert evaluation + dispatch
    │   │   └── scheduler.go        # ties loops together
    │   ├── notify/
    │   │   ├── notifier.go         # Notifier interface
    │   │   └── discord.go          # Discord webhook implementation
    │   └── store/
    │       ├── store.go            # Store interface (datastore abstraction)
    │       ├── postgres.go         # PostgresStore implementation
    │       └── queries.go          # SQL query constants
    ├── pkg/
    │   ├── extract/
    │   │   ├── extractor.go        # Extractor interface + LLMExtractor
    │   │   ├── backend.go          # LLMBackend interface
    │   │   ├── ollama.go           # Ollama implementation
    │   │   ├── anthropic.go        # Claude API implementation
    │   │   ├── openai.go           # OpenAI-compatible implementation
    │   │   ├── prompts.go          # prompt templates
    │   │   └── validate.go         # response validation
    │   ├── scorer/
    │   │   └── scorer.go           # scoring engine (exists)
    │   └── types/
    │       └── types.go            # domain types (exists)
    ├── models/
    │   └── 001_initial_schema.sql  # database schema (exists)
    ├── .mockery.yaml
    ├── config.example.yaml
    ├── Makefile
    ├── go.mod
    └── go.sum
    ```
  - Note: `pkg/` contains exported packages that external tools (like `tools/baseline-seeder`) can import. `internal/` contains application-specific code. Every package with an interface gets a `mocks/` subdirectory via mockery.

- [x] **0.4 — Config loader**
  - Define `Config` struct mapping to `config.yaml` sections: database, ebay, llm, scoring, schedule, notifications, server, logging
  - Add `server` config section:
    ```yaml
    server:
      host: "0.0.0.0"
      port: 8080
      read_timeout: 30s
      write_timeout: 30s
    ```
  - Environment variable substitution via `os.ExpandEnv()` for secrets
  - Validate required fields on load (DB connection, LLM endpoint)
  - **Tests:** Table-driven tests for config loading:
    - Valid config parses correctly
    - Missing required fields return descriptive errors
    - Env var substitution works (`${VAR}` → value)
    - Default values applied when optional fields omitted
    - Invalid YAML returns parse error

- [x] **0.5 — Database migration runner**
  - Simple migration runner: read SQL files from `embed.FS`, track in `schema_migrations (version TEXT, applied_at TIMESTAMPTZ)`, apply pending in order
  - `server-price-tracker migrate` CLI command
  - No down migrations (fix forward)
  - Note: Migration runner itself talks to Postgres directly — `// TODO(test): migration runner requires live Postgres, tested via integration tests only`

- [x] **0.6 — Makefile**
  ```makefile
  .PHONY: build run test test-coverage lint fmt migrate mocks

  build:
  	go build -o bin/server-price-tracker ./cmd/server-price-tracker

  run:
  	go run ./cmd/server-price-tracker serve

  test:
  	go test ./...

  test-integration:
  	go test -tags integration ./...

  test-coverage:
  	go test -race -coverprofile=coverage.out -covermode=atomic ./...

  lint:
  	golangci-lint run ./...

  fmt:
  	goimports -w . && golines -w .

  migrate:
  	go run ./cmd/server-price-tracker migrate

  mocks:
  	mockery
  ```

- [x] **0.7 — Root command and serve command**
  - Cobra root command with `--config` flag (default: `config.yaml`)
  - `serve` command: loads config, connects to Postgres, starts Echo server with health endpoints, blocks on signal
  - `migrate` command: loads config, runs migrations, exits
  - `version` command: prints build version

- [x] **0.8 — Health endpoints**
  - `GET /healthz` — returns 200 if process is running
  - `GET /readyz` — returns 200 if DB is connected (via `Store.Ping()`); 503 otherwise
  - **Tests:** Table-driven handler tests using `httptest.NewRecorder`:
    - healthz always returns 200
    - readyz returns 200 when mock store `Ping()` succeeds
    - readyz returns 503 when mock store `Ping()` returns error

- [x] **0.9 — Prometheus metrics setup**
  - Register custom metrics (counters, histograms, gauges) in an `internal/metrics` package
  - Expose `GET /metrics` via `promhttp.Handler()`
  - Add HTTP middleware to record request duration/status

### Success Criteria

- [ ] `go build ./...` compiles with zero errors
- [ ] `make mocks` generates mock files for all interfaces without errors
- [ ] `go test ./...` passes — config loader tests, health handler tests all green
- [ ] `server-price-tracker version` prints version string
- [ ] `server-price-tracker serve` starts Echo server on configured port
- [ ] `GET /healthz` returns 200
- [ ] `GET /metrics` returns Prometheus-formatted output
- [ ] `golangci-lint run ./...` passes with zero issues
- [ ] Config loads from YAML with env var substitution working
- [ ] `.mockery.yaml` configured for all planned interfaces

---

## Phase 1: eBay API Client

**Goal:** eBay client with interface, mock, and full test coverage — functional with or without live eBay API access.

### Tasks

- [ ] **1.1 — EbayClient interface + types**
  - Define in `internal/ebay/client.go`:
    ```go
    type EbayClient interface {
        Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)
    }

    type SearchRequest struct {
        Query      string
        CategoryID string
        Limit      int
        Offset     int
        Sort       string  // "newlyListed"
        Filters    map[string]string
    }

    type SearchResponse struct {
        Items      []ItemSummary
        Total      int
        Offset     int
        Limit      int
        HasMore    bool
    }
    ```
  - Define `TokenProvider` interface in `internal/ebay/auth.go`:
    ```go
    type TokenProvider interface {
        Token(ctx context.Context) (string, error)
    }
    ```
  - Run `make mocks` to generate `MockEbayClient` and `MockTokenProvider`

- [ ] **1.2 — eBay response types + parsing**
  - Define raw eBay API response structs in `internal/ebay/types.go`
  - Conversion function: `func toListings(items []ItemSummary) []domain.Listing`
  - Field mapping per DESIGN.md table (itemId → ebay_item_id, price.value → price, etc.)
  - **Tests:** Table-driven tests for `toListings()`:
    - Complete item with all fields → correct Listing
    - Item missing optional fields (no shipping, no image) → Listing with zero values
    - Auction vs BIN vs Best Offer → correct listing_type mapping
    - Various eBay condition strings → correct condition_raw

- [ ] **1.3 — OAuth2 TokenProvider implementation**
  - `OAuthTokenProvider` implements `TokenProvider`:
    - Endpoint: `https://api.ebay.com/identity/v1/oauth2/token`
    - Client credentials flow, base64-encoded app_id:cert_id
    - Cache token in memory with expiry
    - Refresh automatically when expired or within 60s of expiry
    - Thread-safe (sync.Mutex around token refresh)
  - **Tests:** Table-driven tests with `httptest.NewServer`:
    - Successful token fetch → returns token, caches it
    - Second call within expiry → returns cached token (no HTTP call)
    - Call after expiry → refreshes token
    - Server returns 401 → returns error
    - Server returns 500 → returns error
    - Concurrent calls → only one HTTP request (mutex test)

- [ ] **1.4 — BrowseClient implementation**
  - Implements `EbayClient` interface
  - Uses `TokenProvider` for auth (injected, not hard-coded)
  - Primary endpoint: `GET https://api.ebay.com/buy/browse/v1/item_summary/search`
  - **Tests:** Table-driven tests with `httptest.NewServer` + `MockTokenProvider`:
    - Successful search → correct parsing into SearchResponse
    - Empty results → empty items, hasMore false
    - 401 response → error
    - 429 response → error (rate limited)
    - 500 response → error
    - Network error → error
    - Query parameters are correctly encoded

- [ ] **1.5 — Rate limiting**
  - Use `golang.org/x/time/rate` token bucket, ~3 calls/second
  - Daily counter that stops ingestion if approaching the 5,000/day free tier limit
  - **Tests:** Table-driven tests:
    - Rate limiter allows burst within limit
    - Rate limiter blocks when exceeded
    - Daily counter resets at boundary

- [ ] **1.6 — Pagination strategy**
  - For each watch, paginate sorted by `newlyListed` until:
    - Hit a listing already in DB (dedup by `ebay_item_id`), OR
    - Hit `max_calls_per_cycle` pages, OR
    - No more results
  - First run for a watch: cap at 5 pages (1000 listings)
  - **Tests:** Table-driven tests with `MockEbayClient` + `MockStore`:
    - Stops when known listing found (mock store returns existing)
    - Stops at max pages
    - Stops when no more results (hasMore false)
    - First run caps at 5 pages

- [ ] **1.7 — Search API endpoint + CLI command**
  - `POST /api/v1/search` — accepts query and optional filters, returns raw eBay results without persisting
  - Handler uses `EbayClient` interface (testable with mock)
  - CLI command: `server-price-tracker search "32GB DDR4 ECC" --limit 5`
  - **Tests:** Table-driven handler tests with `MockEbayClient`:
    - Valid request → 200 with results
    - Missing query → 400
    - eBay client error → 502

### Success Criteria

- [ ] `make mocks` generates `MockEbayClient` and `MockTokenProvider`
- [ ] All table-driven tests pass for response parsing, token caching, rate limiting, pagination
- [ ] Handler tests pass using mocked eBay client (no live API needed)
- [ ] BrowseClient integration test exists (tagged `//go:build integration`) for when API access is available
- [ ] `go test ./internal/ebay/...` achieves >= 90% coverage
- [ ] `go test ./internal/api/handlers/...` covers search handler

---

## Phase 2: LLM Extraction Pipeline

**Goal:** Full extraction pipeline with interface, mock, and tests for all five component types — functional without a running LLM.

### Tasks

- [ ] **2.1 — LLMBackend interface + Extractor interface**
  - Define in `pkg/extract/backend.go`:
    ```go
    type LLMBackend interface {
        Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
        Name() string
    }
    ```
  - Define in `pkg/extract/extractor.go`:
    ```go
    type Extractor interface {
        Classify(ctx context.Context, title string) (domain.ComponentType, error)
        Extract(ctx context.Context, componentType domain.ComponentType, title string, itemSpecifics map[string]string) (map[string]any, error)
        ClassifyAndExtract(ctx context.Context, title string, itemSpecifics map[string]string) (domain.ComponentType, map[string]any, error)
    }
    ```
  - Run `make mocks` to generate `MockLLMBackend` and `MockExtractor`

- [ ] **2.2 — Prompt templates**
  - Embed all prompts from `docs/EXTRACTION.md` as Go string constants in `pkg/extract/prompts.go`
  - Use `text/template` for variable parts (title, item specifics, description)
  - Prompts for all 5 component types: RAM, Drive, Server, CPU, NIC
  - Classification prompt
  - **Tests:** Table-driven tests for prompt rendering:
    - Each component type renders with correct title substitution
    - Classification prompt renders correctly
    - Special characters in title don't break template

- [ ] **2.3 — Response validation**
  - `pkg/extract/validate.go` — validate extracted JSON per component type per rules in EXTRACTION.md
  - **Tests:** Table-driven tests per component type:
    - Valid RAM extraction → passes
    - capacity_gb = 0 → fails (out of range)
    - capacity_gb = 2048 → fails (out of range)
    - Missing required field (generation) → fails
    - Invalid enum for condition → fails
    - confidence > 1.0 → fails
    - quantity < 1 → fails
    - Valid CPU, NIC, Drive, Server extractions → pass
    - Each component type's edge cases

- [ ] **2.4 — Condition normalization**
  - Map eBay condition strings and LLM-extracted condition to normalized enum
  - **Tests:** Table-driven:
    - "New" → new, "Brand New" → new, "Factory Sealed" → new
    - "Open Box" → like_new, "Manufacturer Refurbished" → like_new
    - "Used" → used_working, "Pre-Owned" → used_working, "Tested Working" → used_working
    - "For Parts" → for_parts, "Not Working" → for_parts
    - "Something Random" → unknown
    - Empty string → unknown

- [ ] **2.5 — Product key generation**
  - Pure function: `ProductKey(componentType string, attrs map[string]any) string`
  - **Tests:** Table-driven for all 5 types:
    - RAM with full attrs → `ram:ddr4:ecc_reg:32gb:2666`
    - RAM missing speed → `ram:ddr4:ecc_reg:32gb:unknown`
    - Drive SSD → `drive:nvme:2.5:3.84tb:ssd`
    - Server → `server:dell:r740xd:sff`
    - CPU → `cpu:intel:xeon:gold_6130`
    - NIC → `nic:10gbe:2p:sfp+`
    - Unknown type → `other:unknown`
    - Nil attrs → all segments "unknown"

- [ ] **2.6 — LLMExtractor implementation**
  - Implements `Extractor` interface, takes `LLMBackend` as dependency
  - `Classify`: renders classification prompt, calls backend, parses response
  - `Extract`: renders component-specific prompt, calls backend, parses JSON, validates
  - `ClassifyAndExtract`: calls both in sequence
  - Configurable concurrency via semaphore
  - **Tests:** Table-driven tests with `MockLLMBackend`:
    - Classify with mock returning "ram" → ComponentTypeRAM
    - Classify with mock returning invalid → error
    - Extract with mock returning valid RAM JSON → parsed attributes
    - Extract with mock returning invalid JSON → error, confidence 0.0
    - Extract with mock returning out-of-range values → validation error
    - ClassifyAndExtract chains both calls correctly
    - Concurrency limit respected (test with slow mock)

- [ ] **2.7 — Ollama backend implementation**
  - HTTP client for Ollama `/api/generate` endpoint
  - **Tests:** Table-driven tests with `httptest.NewServer`:
    - Successful generation → correct response
    - Timeout → error
    - Invalid JSON response → error
    - Server error → error

- [ ] **2.8 — Anthropic Claude API backend**
  - HTTP client for `https://api.anthropic.com/v1/messages`
  - **Tests:** Table-driven tests with `httptest.NewServer`:
    - Successful generation → correct response
    - Missing API key → error
    - Rate limited (429) → error
    - Server error → error

- [ ] **2.9 — OpenAI-compatible backend**
  - HTTP client for `/v1/chat/completions` endpoint
  - **Tests:** Table-driven tests with `httptest.NewServer`

- [ ] **2.10 — Extraction metrics**
  - Record `spt_extraction_duration_seconds` histogram per extraction
  - Record `spt_extraction_failures_total` counter

- [ ] **2.11 — Extract API endpoint + CLI command**
  - `POST /api/v1/extract` — accepts title and optional item specifics, returns extracted JSON
  - Handler uses `Extractor` interface (testable with mock)
  - CLI command: `server-price-tracker extract "Samsung 32GB 2Rx4 PC4-2666V DDR4 ECC REG"`
  - **Tests:** Table-driven handler tests with `MockExtractor`:
    - Valid request → 200 with extracted JSON
    - Missing title → 400
    - Extractor error → 500

### Success Criteria

- [ ] `make mocks` generates `MockLLMBackend` and `MockExtractor`
- [ ] All table-driven tests pass for prompts, validation, condition normalization, product keys
- [ ] Extractor tests pass using `MockLLMBackend` (no running LLM needed)
- [ ] Handler tests pass using `MockExtractor`
- [ ] Each backend has tests against `httptest.NewServer` mocks
- [ ] Integration tests exist (tagged `//go:build integration`) for real Ollama
- [ ] `go test ./pkg/extract/...` achieves >= 90% coverage
- [ ] Switching `llm.backend` config between `ollama` and `anthropic` works without code changes

---

## Phase 3: Storage Layer

**Goal:** Datastore abstraction with Store interface, Postgres implementation, and mock-based testing for all consumers.

### Tasks

- [ ] **3.1 — Store interface (datastore abstraction)**
  - Define in `internal/store/store.go`:
    ```go
    type Store interface {
        // Listings
        UpsertListing(ctx context.Context, l *domain.Listing) error
        GetListing(ctx context.Context, ebayID string) (*domain.Listing, error)
        GetListingByID(ctx context.Context, id string) (*domain.Listing, error)
        ListListings(ctx context.Context, opts ListingQuery) ([]domain.Listing, int, error)
        UpdateListingExtraction(ctx context.Context, id string, componentType string, attrs map[string]any, confidence float64, productKey string) error
        UpdateScore(ctx context.Context, id string, score int, breakdown json.RawMessage) error
        ListUnextractedListings(ctx context.Context, limit int) ([]domain.Listing, error)
        ListUnscoredListings(ctx context.Context, limit int) ([]domain.Listing, error)

        // Watches
        CreateWatch(ctx context.Context, w *domain.Watch) error
        GetWatch(ctx context.Context, id string) (*domain.Watch, error)
        ListWatches(ctx context.Context, enabledOnly bool) ([]domain.Watch, error)
        UpdateWatch(ctx context.Context, w *domain.Watch) error
        DeleteWatch(ctx context.Context, id string) error
        SetWatchEnabled(ctx context.Context, id string, enabled bool) error

        // Baselines
        GetBaseline(ctx context.Context, productKey string) (*domain.PriceBaseline, error)
        ListBaselines(ctx context.Context) ([]domain.PriceBaseline, error)
        RecomputeBaseline(ctx context.Context, productKey string, windowDays int) error
        RecomputeAllBaselines(ctx context.Context, windowDays int) error

        // Alerts
        CreateAlert(ctx context.Context, a *domain.Alert) error
        ListPendingAlerts(ctx context.Context) ([]domain.Alert, error)
        ListAlertsByWatch(ctx context.Context, watchID string, limit int) ([]domain.Alert, error)
        MarkAlertNotified(ctx context.Context, id string) error
        MarkAlertsNotified(ctx context.Context, ids []string) error

        // Migrations
        Migrate(ctx context.Context) error

        // Health
        Ping(ctx context.Context) error
    }
    ```
  - Run `make mocks` to generate `MockStore`
  - All consumers (handlers, engine, CLI) depend on this interface, never on `pgx` directly

- [ ] **3.2 — ListingQuery builder**
  - `ListingQuery` struct with optional filters:
    ```go
    type ListingQuery struct {
        ComponentType *string
        MinScore      *int
        MaxScore      *int
        ProductKey    *string
        SellerMinFB   *int
        Conditions    []string
        Limit         int    // default 50
        Offset        int
        OrderBy       string // "score", "price", "first_seen_at"
    }
    ```
  - `func (q ListingQuery) ToSQL() (string, []any)` builds WHERE clause dynamically
  - **Tests:** Table-driven tests for query builder:
    - Empty query → base SELECT with default limit
    - ComponentType set → `WHERE component_type = $1`
    - Multiple filters → correct AND chain with correct parameter numbering
    - All filters combined → correct SQL
    - OrderBy maps to valid column names (prevents injection)
    - Invalid OrderBy → falls back to default

- [ ] **3.3 — SQL query constants**
  - Keep all SQL in `internal/store/queries.go` as constants
  - Organize by entity (listings, watches, baselines, alerts)

- [ ] **3.4 — PostgresStore implementation**
  - Use `pgxpool` for connection pooling (pool size: 10)
  - Raw SQL with `pgx.NamedArgs` — no ORM
  - **UpsertListing:** `INSERT ... ON CONFLICT (ebay_item_id) DO UPDATE`
  - Implements every method of `Store` interface
  - `// TODO(test): PostgresStore methods require live Postgres, tested via integration tests`
  - **Integration tests** (tagged `//go:build integration`):
    - Use `testcontainers-go` to spin up ephemeral Postgres
    - Table-driven tests for each Store method:
      - UpsertListing insert, then upsert with changed price
      - GetListing found vs not found
      - ListListings with various filter combinations
      - Watch CRUD lifecycle
      - Alert creation with dedup (UNIQUE constraint)
      - Baseline recomputation with test data
      - Pagination returns correct total counts

- [ ] **3.5 — Connection management**
  - Pool size: 10 connections
  - Health check on startup via `Ping()`
  - Graceful shutdown: close pool on context cancellation

- [ ] **3.6 — Migration runner**
  - Read SQL files from embedded `embed.FS`
  - Track in `schema_migrations (version TEXT, applied_at TIMESTAMPTZ)`
  - Apply pending in order, no down migrations

### Success Criteria

- [ ] `make mocks` generates `MockStore`
- [ ] `ListingQuery.ToSQL()` tests pass for all filter combinations
- [ ] `MockStore` is usable in handler tests, engine tests, CLI tests (verified by importing)
- [ ] Integration tests pass against testcontainers Postgres (when run with `-tags integration`)
- [ ] All CRUD operations work in integration tests
- [ ] UpsertListing correctly handles insert and update paths
- [ ] Pagination returns correct total counts in integration tests
- [ ] `go test ./internal/store/...` (unit) achieves 100% on query builder
- [ ] `go test -tags integration ./internal/store/...` covers all Store methods

---

## Phase 4: Scoring Engine Integration

**Goal:** Score every listing with a composite deal score, fully testable via mocked Store.

### Tasks

- [ ] **4.1 — Wire scorer to store (via interfaces)**
  - Engine scoring function takes `Store` and `Scorer` interfaces:
    ```go
    func ScoreListing(ctx context.Context, store Store, scorer Scorer, listing *domain.Listing) error {
        baseline, err := store.GetBaseline(ctx, listing.ProductKey)
        // ... build ListingData, call scorer, persist via store.UpdateScore()
    }
    ```
  - **Tests:** Table-driven with `MockStore`:
    - Listing with baseline → correct score persisted
    - Listing without baseline (cold start) → price factor defaults to 50, score still computed
    - Listing with nil product key → score remains NULL
    - Store.GetBaseline error → error propagated
    - Store.UpdateScore error → error propagated

- [ ] **4.2 — Batch re-scoring**
  - When baselines are recomputed, re-score all active listings
  - **Tests:** Table-driven with `MockStore`:
    - 3 listings with same product key → all re-scored when baseline changes
    - Mixed product keys → correct baseline looked up for each
    - Store errors mid-batch → continues with remaining, returns aggregated errors

- [ ] **4.3 — Score staleness handling**
  - `score` column is nullable — NULL means "not yet scored" or "extraction failed"
  - Record `spt_scoring_distribution` histogram

- [ ] **4.4 — Rescore API endpoint**
  - `POST /api/v1/rescore` — triggers re-scoring
  - **Tests:** Table-driven handler tests with mocks

- [ ] **4.5 — Listings API endpoints**
  - `GET /api/v1/listings` — list with filters
  - `GET /api/v1/listings/:id` — full detail
  - **Tests:** Table-driven handler tests with `MockStore`:
    - List with various query params → correct `ListingQuery` passed to store
    - Get by ID found → 200 with detail
    - Get by ID not found → 404
    - Store error → 500

### Success Criteria

- [ ] All scoring wiring tests pass with `MockStore` (no database needed)
- [ ] Batch re-scoring tested with mock returning multiple listings
- [ ] Handler tests cover list/show/rescore endpoints
- [ ] Cold-start behavior tested (no baseline → neutral price score)
- [ ] `go test ./internal/engine/...` covers scoring logic >= 90%
- [ ] `go test ./internal/api/handlers/...` covers listing handlers

---

## Phase 5: Notification System

**Goal:** Discord webhook notifications with rich embeds, fully testable via mocked Notifier.

### Tasks

- [ ] **5.1 — Notifier interface**
  - Define in `internal/notify/notifier.go`:
    ```go
    type Notifier interface {
        SendAlert(ctx context.Context, alert AlertPayload) error
        SendBatchAlert(ctx context.Context, alerts []AlertPayload, watchName string) error
    }
    ```
  - Run `make mocks` to generate `MockNotifier`

- [ ] **5.2 — Discord webhook implementation**
  - `DiscordNotifier` implements `Notifier`
  - POST to Discord webhook URL with rich embed
  - Color coding by score: green (90+), yellow (80-89), orange (75-79)
  - **Tests:** Table-driven tests with `httptest.NewServer` (mock Discord API):
    - Valid alert → correct embed JSON posted
    - Score 92 → green color
    - Score 85 → yellow color
    - Score 76 → orange color
    - Embed includes all fields (title, price, seller, score, image URL)
    - Discord returns 429 → error (rate limited)
    - Discord returns 400 → error
    - Network error → error

- [ ] **5.3 — Notification dedup + marking**
  - `alerts` table UNIQUE(watch_id, listing_id) prevents duplicates
  - Only notify when `notified = false`, mark `notified = true` after send
  - **Tests:** Table-driven with `MockStore` + `MockNotifier`:
    - New alert → notify called, then MarkAlertNotified called
    - Already notified alert → notify not called
    - Notify fails → MarkAlertNotified not called

- [ ] **5.4 — Notification batching**
  - 5+ alerts for same watch → batch into single Discord message
  - **Tests:** Table-driven:
    - 3 alerts → 3 individual SendAlert calls
    - 7 alerts → 1 SendBatchAlert call
    - Exactly 5 alerts → 1 SendBatchAlert call

- [ ] **5.5 — Error handling**
  - Discord rate limits: respect 429 with `Retry-After` header
  - On send failure: log error, don't mark as notified
  - Record `spt_alerts_fired_total` and `spt_notification_failures_total`

### Success Criteria

- [ ] `make mocks` generates `MockNotifier`
- [ ] DiscordNotifier tests pass with `httptest.NewServer` mock
- [ ] Embed JSON is correctly formatted for all score ranges
- [ ] Dedup logic tested: duplicate alerts don't trigger notifications
- [ ] Batching logic tested: correct threshold triggers batch vs individual
- [ ] Error handling tested: failed sends don't mark alerts as notified
- [ ] `go test ./internal/notify/...` achieves >= 90% coverage

---

## Phase 6: Engine — Putting It All Together

**Goal:** Orchestrate ingestion, scoring, alerting, and scheduling — fully testable via injected interfaces.

### Tasks

- [ ] **6.1 — Engine struct with injected dependencies**
  ```go
  type Engine struct {
      store     store.Store
      ebay      ebay.EbayClient
      extractor extract.Extractor
      notifier  notify.Notifier
      scorer    scorer.Scorer
      config    *config.Config
      log       *log.Logger
  }
  ```
  All dependencies are interfaces. The entire engine is testable with mocks.

- [ ] **6.2 — Ingestion loop**
  - `Engine.RunIngestion(ctx)`:
    ```
    for each enabled watch (from store.ListWatches):
        1. Search eBay via ebay.Search()
        2. For each new listing:
           a. store.UpsertListing()
           b. extractor.ClassifyAndExtract()
           c. store.UpdateListingExtraction()
        3. Score new listings
        4. Evaluate alerts
    ```
  - Error isolation: extraction failure for one listing → log and continue
  - **Tests:** Table-driven with all mocks:
    - 2 watches, 3 listings each → 6 UpsertListing calls, 6 extract calls
    - Extraction fails for 1 listing → continues with remaining 5
    - eBay error for 1 watch → skips watch, processes second watch
    - All listings already exist → no extraction calls
    - Metrics incremented correctly

- [ ] **6.3 — Baseline recomputation loop**
  - `Engine.RunBaselineRefresh(ctx)`: calls `store.RecomputeAllBaselines()`, then re-scores
  - **Tests:** Table-driven with `MockStore`:
    - Recompute called → re-score triggered
    - Recompute error → error returned

- [ ] **6.4 — Alert evaluation**
  - Runs inline after ingestion: check new listings against watches
  - **Tests:** Table-driven with `MockStore` + `MockNotifier`:
    - Listing score 90, watch threshold 80, filters match → alert created + notify
    - Listing score 70, watch threshold 80 → no alert
    - Listing matches score but not filters → no alert
    - Duplicate alert → store returns unique constraint error, no notify

- [ ] **6.5 — Scheduler**
  - Use `robfig/cron` for scheduling
  - `serve` command starts both Echo + scheduler
  - **Tests:** Verify cron entries are registered (unit test cron setup function)

- [ ] **6.6 — Manual trigger endpoints**
  - `POST /api/v1/ingest` — trigger immediate ingestion
  - `POST /api/v1/baselines/refresh` — trigger immediate baseline refresh
  - **Tests:** Table-driven handler tests (verify engine methods called via mock)

- [ ] **6.7 — Context and cancellation**
  - On SIGINT/SIGTERM: graceful shutdown
  - `// TODO(test): signal handling requires process-level testing, verified manually`

- [ ] **6.8 — Watch staggering**
  - Stagger watch polling to avoid API bursts

### Success Criteria

- [ ] Full ingestion pipeline tested end-to-end with all mocked dependencies
- [ ] Error isolation tested: one bad listing doesn't stop the pipeline
- [ ] Alert evaluation tested: correct threshold + filter matching
- [ ] Scheduler registers correct cron entries
- [ ] Manual trigger endpoints call engine methods
- [ ] `go test ./internal/engine/...` achieves >= 85% coverage
- [ ] `go test ./...` (all unit tests) still pass — no regressions

---

## Phase 7: CLI Commands

**Goal:** Full CLI client for the API, with handler-level tests via mocked Store.

### Tasks

- [ ] **7.1 — HTTP client for CLI**
  - Thin HTTP client targeting `--api-url` (default: `http://localhost:8080`)
  - JSON request/response marshaling
  - Error handling: connection refused → "API server not running"

- [ ] **7.2 — Watch commands**
  - `watch add`, `watch list`, `watch show`, `watch edit`, `watch enable`, `watch disable`, `watch remove`
  - Parse `--filter` flags into `WatchFilters` (the `attr:` prefix routes to AttributeFilters)
  - **Tests:** Table-driven tests for filter parsing:
    - `seller_min_feedback=500` → correct WatchFilters field
    - `attr:capacity_gb=32` → correct AttributeFilter
    - `conditions=used_working,new` → correct slice
    - Invalid filter key → error

- [ ] **7.3 — Listings commands**
  - `listings list`, `listings show`, `listings rescore`
  - Default: compact table output, `--json` flag for raw JSON

- [ ] **7.4 — Baseline commands**
  - `baselines list`, `baselines show`, `baselines refresh`

- [ ] **7.5 — Output formatting**
  - Default: compact table (use `text/tabwriter` or lipgloss)
  - `--json` flag on all list/show commands
  - **Tests:** Table-driven tests for table formatting:
    - Listing data → correctly formatted table string
    - Empty list → "No listings found" message
    - `--json` → valid JSON output

- [ ] **7.6 — Handler tests for all API endpoints**
  - Every watch/listing/baseline handler tested with `MockStore`:
    - Watch CRUD lifecycle (create → list → get → update → delete)
    - Listings list with filters → correct query passed to store
    - Error cases: not found, validation errors, store errors

### Success Criteria

- [ ] Filter parsing tested for all filter types
- [ ] Output formatting tested for table and JSON modes
- [ ] All handler endpoints have table-driven tests with `MockStore`
- [ ] CLI shows clear error when API server is not running
- [ ] `--api-url` flag works
- [ ] `go test ./internal/api/handlers/...` achieves >= 90% coverage

---

## Phase 8: Hardening & Integration Testing

**Goal:** Edge case coverage, integration tests with real services, logging, and production readiness. Unit test coverage should already be high from Phases 0–7 — this phase fills gaps and adds real-service integration.

### Tasks

- [ ] **8.1 — Coverage audit**
  - Run `go test -coverprofile=coverage.out ./...`
  - Identify packages below 80% coverage
  - Add missing test cases to close gaps
  - Target: >= 90% on `pkg/`, >= 80% on `internal/`

- [ ] **8.2 — Edge case tests**
  Add table-driven test cases for:
  | Scenario | Package | Test |
  |---|---|---|
  | Title in non-Latin script | `pkg/extract` | Classification returns `other` |
  | Price is $0.01 (auction start) | `pkg/scorer` | Score still computes, time factor weighted |
  | Quantity "1" but title "LOT OF 4" | `pkg/extract` | LLM extraction override tested with mock |
  | Seller has 0 feedback | `pkg/scorer` | Seller score = 0 |
  | LLM returns invalid JSON | `pkg/extract` | Confidence 0.0, no attributes |
  | LLM hallucinates values | `pkg/extract` | Validation catches out-of-range |
  | eBay returns HTML | `internal/ebay` | Content-Type check, error returned |
  | Discord webhook invalid URL | `internal/notify` | Error returned, alert not marked |
  | Nil baseline | `pkg/scorer` | Price factor neutral 50 |
  | 0 quantity listing | `pkg/scorer` | Handled without divide-by-zero |

- [ ] **8.3 — Integration tests**
  All tagged `//go:build integration`:
  - Full pipeline with testcontainers (Postgres):
    - Upsert listing → extract (mock LLM) → score → alert → notify (mock)
  - PostgresStore against real Postgres: all Store methods
  - Ollama backend against real Ollama (when available)
  - API server with real store: httptest client → Echo → Postgres

- [ ] **8.4 — Logging strategy**
  - `charmbracelet/log` with structured fields
  - Log levels: DEBUG (raw responses), INFO (summaries), WARN (failures), ERROR (unrecoverable)
  - Request logging middleware: method, path, status, duration, request ID

- [ ] **8.5 — Request logging middleware**
  - Echo middleware that logs all requests
  - Request ID propagated through context for correlation
  - **Tests:** Table-driven middleware test:
    - Request → log entry with correct method/path/status

- [ ] **8.6 — Panic recovery**
  - Echo recovery middleware
  - Engine loop recovery
  - `// TODO(test): panic recovery tested manually and via integration tests`

### Success Criteria

- [ ] `go test ./...` passes with 0 failures
- [ ] Coverage >= 90% on `pkg/`, >= 80% on `internal/`
- [ ] All edge cases from table have explicit test cases
- [ ] Integration tests pass with `-tags integration` (where services available)
- [ ] `golangci-lint run ./...` passes with zero issues
- [ ] All `// TODO(test):` annotations reviewed — either tests added or rationale documented
- [ ] No panics escape (recovery middleware verified)
- [ ] Structured logging verified at all levels

---

## Phase 9: Deployment

**Goal:** Container image, Kustomize manifests, and ArgoCD configuration for deployment to a Talos Kubernetes cluster with Cilium API Gateway.

Note: Full end-to-end deployment depends on external services (eBay API approval, Postgres in cluster, Ollama with GPU scheduling). This phase focuses on the infrastructure-as-code that's ready to deploy when services are available.

### Tasks

- [ ] **9.1 — Dockerfile**
  ```dockerfile
  FROM golang:1.25-alpine AS builder
  WORKDIR /app
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  RUN CGO_ENABLED=0 go build -o /server-price-tracker ./cmd/server-price-tracker

  FROM alpine:3.21
  RUN apk add --no-cache ca-certificates
  COPY --from=builder /server-price-tracker /usr/local/bin/server-price-tracker
  EXPOSE 8080
  ENTRYPOINT ["server-price-tracker"]
  CMD ["serve"]
  ```

- [ ] **9.2 — Docker Compose (local development only)**
  - Minimal compose for local dev: just Postgres + the app
  - Ollama, Prometheus, Grafana are optional and run separately
  ```yaml
  services:
    postgres:
      image: postgres:17-alpine
      environment:
        POSTGRES_DB: server_price_tracker
        POSTGRES_USER: tracker
        POSTGRES_PASSWORD: ${DB_PASSWORD:-devpassword}
      ports:
        - "5432:5432"
      volumes:
        - pgdata:/var/lib/postgresql/data
      healthcheck:
        test: ["CMD-SHELL", "pg_isready -U tracker"]
        interval: 5s

  volumes:
    pgdata:
  ```
  - This is **not** the production deployment — just a convenience for local development

- [ ] **9.3 — Kustomize base manifests**
  ```
  deploy/
  ├── base/
  │   ├── kustomization.yaml
  │   ├── deployment.yaml
  │   ├── service.yaml
  │   ├── configmap.yaml
  │   ├── httproute.yaml          # Cilium Gateway API HTTPRoute
  │   ├── serviceaccount.yaml
  │   └── servicemonitor.yaml     # Prometheus ServiceMonitor
  ├── overlays/
  │   ├── dev/
  │   │   ├── kustomization.yaml
  │   │   └── patches/
  │   └── prod/
  │       ├── kustomization.yaml
  │       └── patches/
  └── argocd/
      └── application.yaml
  ```

- [ ] **9.4 — Deployment manifest**
  - `deploy/base/deployment.yaml`:
    - Single replica (MVP)
    - Liveness probe: `GET /healthz`
    - Readiness probe: `GET /readyz`
    - Resource requests/limits
    - Config mounted from ConfigMap
    - Secrets from Kubernetes Secret (DB creds, API keys, webhook URL)
    - Init container runs `server-price-tracker migrate`

- [ ] **9.5 — Cilium HTTPRoute**
  - `deploy/base/httproute.yaml`:
    ```yaml
    apiVersion: gateway.networking.k8s.io/v1
    kind: HTTPRoute
    metadata:
      name: server-price-tracker
    spec:
      parentRefs:
        - name: cilium-gateway
          namespace: kube-system
      hostnames:
        - "tracker.example.com"
      rules:
        - matches:
            - path:
                type: PathPrefix
                value: /
          backendRefs:
            - name: server-price-tracker
              port: 8080
    ```

- [ ] **9.6 — ServiceMonitor for Prometheus**
  - `deploy/base/servicemonitor.yaml`:
    ```yaml
    apiVersion: monitoring.coreos.com/v1
    kind: ServiceMonitor
    metadata:
      name: server-price-tracker
    spec:
      selector:
        matchLabels:
          app: server-price-tracker
      endpoints:
        - port: http
          path: /metrics
          interval: 15s
    ```

- [ ] **9.7 — ArgoCD Application**
  - `deploy/argocd/application.yaml` pointing to this repo's `deploy/overlays/prod`
  - Auto-sync with prune enabled

- [ ] **9.8 — Kustomize overlays**
  - `dev`: lower resource limits, debug log level, 1 replica
  - `prod`: production resources, info log level, potential HPA

- [ ] **9.9 — .env.example**
  - Document all required environment variables:
    ```
    DB_PASSWORD=
    EBAY_APP_ID=
    EBAY_CERT_ID=
    DISCORD_WEBHOOK_URL=
    ANTHROPIC_API_KEY=       # optional, only if using anthropic backend
    ```

- [ ] **9.10 — CI container build**
  - GitHub Actions workflow or GoReleaser config builds and pushes container image
  - Image tag from git SHA or semver tag
  - ArgoCD detects new image and deploys

### Success Criteria

- [ ] `docker build .` produces a working container image
- [ ] Container starts, serves `/healthz`, responds on port 8080
- [ ] `kustomize build deploy/base/` produces valid Kubernetes manifests
- [ ] `kustomize build deploy/overlays/dev/` overlays correctly
- [ ] HTTPRoute, ServiceMonitor, and ArgoCD Application manifests are valid YAML
- [ ] Deployment manifest has correct probes, resource limits, and secret references
- [ ] Init container migration strategy documented
- [ ] `.env.example` documents all required secrets

---

## Milestone Summary

| Phase | Description                  | Dependencies | Testing Approach |
|-------|------------------------------|--------------|------------------|
| 0     | Project bootstrap            | None         | Config tests, health handler tests |
| 1     | eBay API client              | Phase 0      | MockTokenProvider, httptest, MockStore for pagination |
| 2     | LLM extraction pipeline      | Phase 0      | MockLLMBackend, httptest for backends |
| 3     | Storage layer                | Phase 0      | MockStore for consumers, testcontainers for Postgres |
| 4     | Scoring integration          | Phases 2, 3  | MockStore + real scorer |
| 5     | Notifications                | Phase 3      | MockNotifier, httptest for Discord |
| 6     | Engine orchestration         | Phases 1–5   | All mocks injected into Engine |
| 7     | CLI commands                 | Phase 6      | MockStore for handlers, filter parsing tests |
| 8     | Hardening & integration      | Phase 7      | Coverage audit, edge cases, real-service integration |
| 9     | Deployment                   | Phase 8      | Manifest validation, container build |

Phases 1, 2, and 3 can be developed in parallel after Phase 0 is complete. Phase 4 requires the extraction pipeline (2) and storage (3). Phase 6 brings everything together. Phases 7–9 are sequential.

```
Phase 0
  ├── Phase 1 (eBay)  ──┐
  ├── Phase 2 (LLM)   ──┼── Phase 4 (Scoring) ─┐
  └── Phase 3 (Store)  ──┤                      ├── Phase 6 (Engine) → Phase 7 (CLI) → Phase 8 (Harden) → Phase 9 (Deploy)
                         └── Phase 5 (Notify) ──┘
```

### TDD Workflow Per Phase

Every phase follows this workflow:

1. **Define interface** for the component being built
2. **Run `make mocks`** to generate mock implementation
3. **Write table-driven tests** using the mock — tests should fail initially
4. **Implement the real code** until tests pass
5. **Add edge case tests** for error paths and boundary conditions
6. **Run `make lint`** to verify code quality
7. **Check coverage** with `make test-coverage` — target >= 85% per package
8. If any code cannot be tested via TDD, annotate: `// TODO(test): <explanation>`
