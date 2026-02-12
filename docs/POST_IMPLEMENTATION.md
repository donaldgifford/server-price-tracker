# Post-Implementation Plan: Wiring & Local Dev Environment

This document covers the remaining work to go from "all components built and
tested in isolation" to "a running system that pulls eBay data, extracts
attributes, scores listings, and sends alerts."

**Design decisions:**

- The API server starts regardless of external service availability (eBay,
  Ollama, Discord). Connection failures are logged but do not block startup.
- A `NoOpNotifier` is used when Discord is disabled or unconfigured.
- Local dev uses `qwen2.5:3b` for Ollama (small, fast). Cluster deployments
  use `mistral:7b-instruct` or larger.
- A mock eBay server (`tools/mock-server`) provides realistic test data
  locally, since the eBay sandbox returns synthetic/limited results.
- `make run` uses a `CONFIG` override variable defaulting to
  `configs/config.dev.yaml`.

---

## Phase 1: Local Dev Docker Environment

Set up `scripts/docker/` with a docker-compose stack for local development
(PostgreSQL + Ollama), wire the Makefile docker targets, and verify the app
Docker image builds.

### 1.1 Create `scripts/docker/docker-compose.yml`

- [x] Create `scripts/docker/docker-compose.yml` with:
  - **postgres** service (postgres:17-alpine)
    - `POSTGRES_DB=server_price_tracker`, `POSTGRES_USER=tracker`,
      `POSTGRES_PASSWORD=${DB_PASSWORD:-devpassword}`
    - Port 5432 exposed
    - Named volume `pgdata` for persistence
    - Healthcheck: `pg_isready -U tracker`
  - **ollama** service (ollama/ollama:latest)
    - Port 11434 exposed
    - Named volume `ollama_data` for model persistence across restarts
    - Healthcheck: `curl -f http://localhost:11434/api/tags || exit 1`
- [x] Add a `profiles: [gpu]` variant or comment for GPU passthrough (nvidia)
- [x] Add a comment documenting the model pull step:
      `docker compose exec ollama ollama pull <model>`

### 1.2 Wire `scripts/makefiles/docker.mk`

- [x] Uncomment and update docker targets to use
      `scripts/docker/docker-compose.yml`
- [x] `docker-up`: Start postgres and ollama, wait for healthchecks
- [x] `docker-down`: Stop all containers
- [x] `docker-clean`: Stop and remove containers, volumes, images
- [x] `docker-logs`: Tail logs
- [x] Add `ollama-pull` target:
      `docker compose exec ollama ollama pull $(OLLAMA_MODEL)`
- [x] Add `OLLAMA_MODEL` variable in `common.mk` (default: `qwen2.5:3b`)
- [x] Add `CONFIG` variable in `common.mk` (default:
      `configs/config.dev.yaml`), usable as `make run CONFIG=configs/other.yaml`
- [x] Add `dev-setup` target that runs `docker-up`, waits for postgres, runs
      `migrate`, pulls the ollama model

### 1.3 Verify app Docker image builds

- [x] Run `docker build -t server-price-tracker:dev .` using the root
      `Dockerfile`
- [x] Verify the image starts and responds to `/healthz` (will fail on DB
      connect for readyz — that's expected)

### 1.4 Move root `docker-compose.yml` into `scripts/docker/`

- [x] The current root `docker-compose.yml` (postgres-only) should be replaced
      by the new one in `scripts/docker/`
- [x] Delete root `docker-compose.yml`

**Success criteria:**

- `make docker-up` starts Postgres and Ollama containers, both pass healthchecks
- `make ollama-pull` pulls the configured model into the Ollama container
- `make dev-setup` brings up the full local dev stack from scratch (containers +
  migrate + model pull)
- `make docker-down` and `make docker-clean` work correctly
- `docker build -t server-price-tracker:dev .` succeeds

---

## Phase 2: eBay Mock Server

Create `tools/mock-server`, a small Go HTTP service that mimics the eBay Browse
API and OAuth token endpoint. It reads canned responses from a JSON fixture file
and returns data that closely resembles real eBay production responses. This
gives us realistic test data without depending on the eBay sandbox.

### 2.1 Scaffold `tools/mock-server`

- [x] Create `tools/mock-server/main.go` — Cobra or plain `net/http` server
- [x] Listen on a configurable port (default: `8089`)
- [x] Log requests for debugging

### 2.2 OAuth token endpoint

- [x] `POST /identity/v1/oauth2/token` — returns a static access token response:
      ```json
      {"access_token":"mock-token-xxx","expires_in":7200,"token_type":"Application Access Token"}
      ```
- [x] Validate Basic Auth header is present (don't need to verify actual creds)

### 2.3 Browse API search endpoint

- [x] `GET /buy/browse/v1/item_summary/search` — returns items from a JSON
      fixture file
- [x] Read fixture from `tools/mock-server/testdata/search_response.json`
- [x] Support `q` query parameter for basic filtering (substring match on
      title) or return all items regardless
- [x] Support `limit` and `offset` for pagination

### 2.4 Create fixture data

- [x] Create `tools/mock-server/testdata/search_response.json` with 10-20
      realistic server hardware listings covering:
  - DDR4/DDR5 ECC RAM (various capacities: 16GB, 32GB, 64GB, 128GB)
  - NVMe/SAS/SATA drives
  - Dell/HP/Supermicro servers
  - Xeon/EPYC CPUs
  - Mellanox/Intel NICs
- [x] Include realistic pricing, seller info, shipping, conditions, images
- [x] Include a mix of auction, buy_it_now, and best_offer listing types
- [x] Structure matches eBay Browse API `itemSummaries` response format

### 2.5 Wire into local dev

- [x] Add `mock-server` service to `scripts/docker/docker-compose.yml`
      (build from `tools/mock-server/Dockerfile` or run via `go run`)
- [x] OR add a `mock-server` Makefile target to run it standalone
- [x] Update `configs/config.dev.yaml` comments to show how to point
      `EBAY_TOKEN_URL` and `EBAY_BROWSE_URL` at the mock server
      (e.g., `http://localhost:8089`)

### 2.6 Tests

- [x] Unit tests for the mock server handler (fixture loading, query filtering)

**Success criteria:**

- `go run ./tools/mock-server` starts and serves on port 8089
- `curl http://localhost:8089/buy/browse/v1/item_summary/search?q=DDR4` returns
  realistic eBay-shaped JSON with multiple item summaries
- `curl -X POST http://localhost:8089/identity/v1/oauth2/token` returns a valid
  token response
- The main app can be configured to use the mock server as its eBay API and
  successfully run ingestion against it

---

## Phase 3: Wire `serve.go`

Connect all the independently-built components in
`cmd/server-price-tracker/cmd/serve.go`. This is the main integration point.
The server starts regardless of external service availability — all connection
failures are logged but do not prevent startup.

### 3.1 Database connection

- [x] Create `pgxpool.Pool` from `cfg.Database.DSN()`
- [x] Create `store.NewPostgresStore(ctx, dsn)` (or accept pool directly —
      check constructor)
- [x] Defer `store.Close()`
- [x] Log error if database is unreachable but continue startup
- [x] Replace inline `/healthz` handler with
      `handlers.NewHealthHandler(store).Healthz`
- [x] Replace inline `/readyz` handler with
      `handlers.NewHealthHandler(store).Readyz`

### 3.2 eBay client

- [x] Build OAuth token provider:
      `NewOAuthTokenProvider(cfg.Ebay.AppID, cfg.Ebay.CertID, WithTokenURL(cfg.Ebay.TokenURL))`
- [x] Build Browse client:
      `NewBrowseClient(tokenProvider, WithBrowseURL(cfg.Ebay.BrowseURL), WithMarketplace(cfg.Ebay.Marketplace))`
- [x] If `AppID` or `CertID` are empty, log a warning and continue without
      eBay (search/ingest handlers will return an error when called)

### 3.3 LLM extractor

- [x] Switch on `cfg.LLM.Backend` to create the appropriate backend:
  - `"ollama"` →
    `NewOllamaBackend(cfg.LLM.Ollama.Endpoint, cfg.LLM.Ollama.Model)`
  - `"anthropic"` →
    `NewAnthropicBackend(WithAnthropicModel(cfg.LLM.Anthropic.Model))`
  - `"openai_compat"` →
    `NewOpenAICompatBackend(cfg.LLM.OpenAICompat.Endpoint, cfg.LLM.OpenAICompat.Model)`
- [x] Create `NewLLMExtractor(backend)`
- [x] Log the configured backend on startup

### 3.4 Notifier

- [x] Create a `NoOpNotifier` in `internal/notify/noop.go` that implements
      `Notifier` and logs discarded alerts
- [x] If `cfg.Notifications.Discord.Enabled` and webhook URL is non-empty:
      create `NewDiscordNotifier(cfg.Notifications.Discord.WebhookURL)`
- [x] Otherwise: use `NoOpNotifier` and log that notifications are disabled

### 3.5 Engine

- [x] Create engine with all four dependencies:
      ```
      NewEngine(store, ebayClient, extractor, notifier,
          WithLogger(slogger),
          WithBaselineWindowDays(cfg.Scoring.BaselineWindowDays),
          WithStaggerOffset(cfg.Schedule.StaggerOffset),
      )
      ```
- [x] If eBay client is nil (credentials missing), pass nil and handle
      gracefully in engine — or skip engine creation and register handlers
      that return 503 for search/ingest routes
- [x] Log which components are active vs disabled

### 3.6 Scheduler

- [x] Create scheduler:
      `NewScheduler(engine, cfg.Schedule.IngestionInterval, cfg.Schedule.BaselineInterval, slogger)`
- [x] Call `scheduler.Start()` after Echo server starts
- [x] Only create scheduler if engine was successfully created

### 3.7 Register handlers and routes

- [x] Instantiate all handlers:
      ```
      healthH    := handlers.NewHealthHandler(store)
      listingsH  := handlers.NewListingsHandler(store)
      watchH     := handlers.NewWatchHandler(store)
      searchH    := handlers.NewSearchHandler(ebayClient)
      extractH   := handlers.NewExtractHandler(extractor)
      rescoreH   := handlers.NewRescoreHandler(store)
      ingestH    := handlers.NewIngestHandler(engine)
      baselineH  := handlers.NewBaselineRefreshHandler(engine)
      ```
- [x] Register routes on Echo:
      ```
      // Health
      e.GET("/healthz", healthH.Healthz)
      e.GET("/readyz", healthH.Readyz)

      // API v1
      api := e.Group("/api/v1")
      api.GET("/listings", listingsH.List)
      api.GET("/listings/:id", listingsH.GetByID)
      api.GET("/watches", watchH.List)
      api.GET("/watches/:id", watchH.Get)
      api.POST("/watches", watchH.Create)
      api.PUT("/watches/:id", watchH.Update)
      api.PUT("/watches/:id/enabled", watchH.SetEnabled)
      api.DELETE("/watches/:id", watchH.Delete)
      api.POST("/search", searchH.Search)
      api.POST("/extract", extractH.Extract)
      api.POST("/rescore", rescoreH.Rescore)
      api.POST("/ingest", ingestH.Ingest)
      api.POST("/baselines/refresh", baselineH.Refresh)

      // Prometheus
      e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))
      ```
- [x] Remove the inline healthz/readyz handlers that are there now
- [x] Remove all `TODO(wire)` comments

### 3.8 Shutdown sequence

- [x] On signal, stop scheduler first, then shut down Echo:
      ```
      schedCtx := scheduler.Stop()
      <-schedCtx.Done()
      e.Shutdown(ctx)
      store.Close()
      ```
- [x] Remove `TODO(wire)` comment about scheduler stop

**Success criteria:**

- `make run` starts the server, logs connection status for each external service
- Server starts even if eBay/Ollama/Discord are unavailable (with warnings)
- `/api/v1/watches` returns `[]` (empty list, but hits the database)
- `/api/v1/extract` with a test title returns extracted attributes (hits Ollama)
- `/api/v1/search` with a query returns eBay results (hits sandbox or mock)
- Server shuts down cleanly on SIGINT with scheduler stop logged
- No `TODO(wire)` comments remain in serve.go

---

## Phase 4: End-to-End Local Smoke Test

Validate the full pipeline works locally with mock eBay data (or sandbox),
Ollama, and Postgres.

### 4.1 Bring up local dev environment

- [ ] `make dev-setup` (docker-up + migrate + ollama-pull)
- [ ] Start mock server: `go run ./tools/mock-server`
- [ ] Set `EBAY_TOKEN_URL=http://localhost:8089/identity/v1/oauth2/token` and
      `EBAY_BROWSE_URL=http://localhost:8089/buy/browse/v1/item_summary/search`
      in `.env` (or keep sandbox URLs to test against real eBay)
- [ ] `make run`
- [ ] Verify `/healthz` returns 200
- [ ] Verify `/readyz` returns 200 (database connected)

### 4.2 Create a watch via CLI or curl

- [ ] Create a watch:
      ```bash
      curl -X POST http://localhost:8080/api/v1/watches \
        -H 'Content-Type: application/json' \
        -d '{"name":"DDR4 ECC 32GB","search_query":"32GB DDR4 ECC RDIMM","component_type":"ram","score_threshold":70}'
      ```
- [ ] Verify watch appears in `GET /api/v1/watches`

### 4.3 Test eBay search (via mock or sandbox)

- [ ] `POST /api/v1/search` with `{"query":"32GB DDR4 ECC","limit":3}`
- [ ] Verify response contains realistic item summaries
- [ ] If using sandbox and results are empty, switch to mock server

### 4.4 Test LLM extraction

- [ ] `POST /api/v1/extract` with
      `{"title":"Samsung 32GB DDR4-2666 PC4-21300 ECC Registered RDIMM"}`
- [ ] Verify response contains `component_type`, `attributes`, `product_key`
- [ ] Verify Ollama logs show the request was processed

### 4.5 Trigger manual ingestion

- [ ] `POST /api/v1/ingest`
- [ ] Watch server logs for ingestion pipeline steps (search -> extract ->
      score -> alert evaluation)
- [ ] Verify listings appear in `GET /api/v1/listings`
- [ ] Verify listings have `component_type`, `attributes`, `product_key`, and
      `score` populated

### 4.6 Verify scheduled ingestion

- [ ] Set a short ingestion interval in config (e.g., 2m) for testing
- [ ] Let the server run past one interval
- [ ] Verify logs show scheduled ingestion firing
- [ ] Verify new/updated listings appear

### 4.7 Test baseline and rescoring

- [ ] `POST /api/v1/baselines/refresh`
- [ ] If enough listings exist, verify baselines are computed
- [ ] `POST /api/v1/rescore` — verify listings get re-scored with baseline
      context

### 4.8 Test Discord notifications (optional)

- [ ] If `DISCORD_WEBHOOK_URL` is set, create a watch with a low
      `score_threshold` (e.g., 1)
- [ ] Trigger ingestion
- [ ] Verify Discord receives an alert embed

**Success criteria:**

- Full pipeline runs: eBay search -> LLM extraction -> scoring -> database
  storage
- Listings are queryable via the API with extracted attributes and scores
- Scheduler fires on interval
- No panics or unhandled errors in logs
- CLI `watch` commands work against the running server

---

## Phase 5: Deployment Readiness

Ensure the wired application works in the Docker image and Kubernetes manifests
are accurate.

### 5.1 Docker image with wired serve.go

- [x] Rebuild: `docker build -t server-price-tracker:latest .`
- [ ] Run with docker-compose (pass config via volume mount or env vars)
- [ ] Verify the container starts, connects to Postgres, and serves `/healthz`

### 5.2 Update Kustomize configmap

- [x] Verify `deploy/base/configmap.yaml` has the config sections needed by the
      wired serve.go
- [x] Add any missing config sections (the configmap currently has a minimal
      config — may need full sections for LLM backend, scoring, schedule)

### 5.3 Validate Kustomize manifests

- [x] `kubectl kustomize deploy/base` — no errors
- [x] `kubectl kustomize deploy/overlays/dev` — no errors
- [x] `kubectl kustomize deploy/overlays/prod` — no errors
- [x] Verify deployment env vars match what serve.go expects
- [x] Verify the init container migration command still works with the wired
      config

### 5.4 Document the deployment

- [x] Update CLAUDE.md if any deployment details changed
- [x] Verify `.env.example` matches all env vars the wired serve.go references
- [x] Verify `configs/config.example.yaml` is complete and accurate

**Success criteria:**

- Docker image runs the fully-wired server
- Kustomize manifests render without errors and include all required
  config/secrets
- `.env.example` and `configs/config.example.yaml` are accurate and complete
- A new developer can clone the repo, run `make dev-setup && make run`, and
  have a working system
