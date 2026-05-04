package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	apimw "github.com/donaldgifford/server-price-tracker/internal/api/middleware"
	"github.com/donaldgifford/server-price-tracker/internal/api/web"
	"github.com/donaldgifford/server-price-tracker/internal/config"
	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	"github.com/donaldgifford/server-price-tracker/internal/engine"
	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	"github.com/donaldgifford/server-price-tracker/internal/notify"
	"github.com/donaldgifford/server-price-tracker/internal/observability"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	"github.com/donaldgifford/server-price-tracker/pkg/judge"
	sptlog "github.com/donaldgifford/server-price-tracker/pkg/logger"
	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
)

func startServer(opts *Options) error {
	cfg, err := config.Load(opts.Config)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	slogger := sptlog.New(cfg.Logging.Level, cfg.Logging.Format)
	slog.SetDefault(slogger)

	// --- OpenTelemetry (no-op when observability.otel.enabled=false) ---
	otelShutdown, err := initOTel(cfg.Observability.Otel, slogger)
	if err != nil {
		return err
	}
	defer otelShutdown()

	// --- Langfuse (no-op when observability.langfuse.enabled=false) ---
	lfClient, lfShutdown, err := initLangfuse(&cfg.Observability.Langfuse, slogger)
	if err != nil {
		return err
	}
	defer lfShutdown()

	// --- Database ---
	ctx := context.Background()
	var pgStore store.Store
	pg, err := store.NewPostgresStore(ctx, cfg.Database.DSN())
	if err != nil {
		slogger.Error("database connection failed, continuing without DB", "error", err)
	} else {
		pgStore = pg
		defer pg.Close()
		slogger.Info("database connected")
	}

	// --- eBay client ---
	ebayClient, rateLimiter, analyticsClient := buildEbayClient(cfg, slogger)

	// --- LLM extractor (Langfuse-decorated when langfuse client is real) ---
	extractor := buildExtractor(cfg, slogger, lfClient)

	// --- Notifier ---
	notifier := buildNotifier(cfg, slogger)

	// --- Extraction workers ---
	workerCtx, workerCancel := context.WithCancel(context.Background())

	// --- Engine + Scheduler ---
	eng, scheduler := buildEngine(
		cfg, pgStore, ebayClient, extractor, notifier,
		analyticsClient, rateLimiter, lfClient, slogger,
	)
	if eng != nil {
		eng.StartExtractionWorkers(workerCtx)
		slogger.Info("extraction workers started", "count", cfg.LLM.Concurrency)
	}

	// --- Echo + Huma ---
	e, humaAPI := buildHTTPServer(slogger)

	// --- Routes ---
	registerRoutes(humaAPI, pgStore, ebayClient, extractor, eng, rateLimiter, cfg.Observability.Langfuse.Endpoint)

	if err := registerAlertsUI(e, cfg, pgStore, notifier, lfClient, slogger); err != nil {
		workerCancel()
		return err
	}

	// Prometheus metrics.
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	slogger.Info("starting server", "addr", addr)

	// Start scheduler.
	if scheduler != nil {
		scheduler.Start()
		slogger.Info("scheduler started")
	}

	// Start server in a goroutine.
	go func() {
		if err := e.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slogger.Error("server error", "err", err)
		}
	}()

	// Wait for interrupt signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	return shutdownServer(e, scheduler, workerCancel, slogger)
}

// shutdownServer runs the orderly shutdown sequence: cancel extraction
// workers, drain the scheduler, then close the HTTP server with a
// 10-second deadline. Extracted from startServer to keep its statement
// count under the funlen budget; behaviour is unchanged.
func shutdownServer(
	e *echo.Echo,
	scheduler *engine.Scheduler,
	workerCancel context.CancelFunc,
	logger *slog.Logger,
) error {
	logger.Info("shutting down server")

	workerCancel()
	logger.Info("extraction workers stopped")

	if scheduler != nil {
		schedCtx := scheduler.Stop()
		<-schedCtx.Done()
		logger.Info("scheduler stopped")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}

	logger.Info("server stopped")
	return nil
}

// initLangfuse builds a Langfuse client from cfg. When disabled, returns
// langfuse.NoopClient + a no-op shutdown — the rest of the app never
// has to branch on "is Langfuse enabled". When enabled, wraps the
// HTTP client in a BufferedClient so transient outages don't block
// the extract path; the deferred shutdown drains pending writes
// within a 5-second deadline.
func initLangfuse(cfg *config.LangfuseConfig, logger *slog.Logger) (langfuse.Client, func(), error) {
	if !cfg.Enabled {
		return langfuse.NoopClient{}, func() {}, nil
	}
	if cfg.Endpoint == "" || cfg.PublicKey == "" || cfg.SecretKey == "" {
		return nil, nil, fmt.Errorf(
			"langfuse enabled but endpoint/public_key/secret_key missing — refusing to start",
		)
	}

	httpClient, err := langfuse.NewHTTPClient(cfg.Endpoint, cfg.PublicKey, cfg.SecretKey)
	if err != nil {
		return nil, nil, fmt.Errorf("building langfuse http client: %w", err)
	}

	buffered := langfuse.NewBufferedClient(httpClient, cfg.BufferSize, langfuse.WithBufferLogger(logger))
	buffered.Start(context.Background())
	logger.Info("langfuse enabled",
		"endpoint", cfg.Endpoint,
		"buffer_size", cfg.BufferSize,
	)

	return buffered, func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := buffered.Stop(shutdownCtx); err != nil {
			logger.Error("langfuse shutdown error", "err", err)
		}
	}, nil
}

// initOTel boots the OTel SDK from cfg and returns a deferred-friendly
// shutdown closure that swallows shutdown errors (logging them) so the
// caller can defer it without juggling errors. Returns a no-op closure
// when otel is disabled — safe to defer regardless.
func initOTel(cfg config.OtelConfig, logger *slog.Logger) (func(), error) {
	initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer initCancel()

	shutdown, err := observability.Init(initCtx, cfg)
	if err != nil {
		return nil, fmt.Errorf("initialising observability: %w", err)
	}

	if cfg.Enabled {
		logger.Info("opentelemetry enabled",
			"endpoint", cfg.Endpoint,
			"service_name", cfg.ServiceName,
		)
	}

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			logger.Error("otel shutdown error", "err", err)
		}
	}, nil
}

// registerAlertsUI mounts the alert review UI under /alerts and the static
// asset handler under /static/* when web.enabled is true and the store is
// available. Lives outside Huma because it serves HTML, not JSON on /api/v1.
func registerAlertsUI(
	e *echo.Echo,
	cfg *config.Config,
	pgStore store.Store,
	notifier notify.Notifier,
	lf langfuse.Client,
	logger *slog.Logger,
) error {
	if !cfg.Web.Enabled || pgStore == nil {
		return nil
	}
	alertsUI := handlers.NewAlertsUIHandler(&handlers.AlertsUIDeps{
		Store:            pgStore,
		Notifier:         notifier,
		Langfuse:         lf,
		LangfuseEndpoint: cfg.Observability.Langfuse.Endpoint,
		JudgeEnabled:     cfg.Observability.Judge.Enabled,
		AlertsURLBase:    cfg.Web.AlertsURLBase,
		Logger:           logger,
	})
	handlers.RegisterAlertsUIRoutes(e, alertsUI)
	staticSub, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		return fmt.Errorf("scoping web static FS: %w", err)
	}
	e.GET("/static/*", echo.WrapHandler(
		http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))),
	))
	logger.Info("alert review UI enabled", "path", "/alerts")
	return nil
}

// buildHTTPServer creates the Echo server and Huma API with standard middleware and config.
func buildHTTPServer(logger *slog.Logger) (*echo.Echo, huma.API) {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Recovery middleware must be first to catch panics from other middleware.
	e.Use(apimw.Recovery(logger))
	e.Use(apimw.RequestLog(logger))
	e.Use(apimw.Metrics())

	humaConfig := huma.DefaultConfig("Server Price Tracker API", "1.0.0")
	humaConfig.Info.Description = "API for monitoring eBay server hardware listings, " +
		"extracting structured attributes via LLM, scoring deals, and sending alerts."
	humaConfig.Info.Contact = &huma.Contact{
		Name: "Donald Gifford",
		URL:  "https://github.com/donaldgifford/server-price-tracker",
	}
	humaConfig.Info.License = &huma.License{
		Name: "Apache 2.0",
		URL:  "http://www.apache.org/licenses/LICENSE-2.0.html",
	}

	return e, humaecho.New(e, humaConfig)
}

// registerRoutes sets up all HTTP routes on the Huma API. The
// langfuseEndpoint is wired through to the alerts trace handler so
// trace deep-links can be resolved by clients without re-reading
// config.
func registerRoutes(
	humaAPI huma.API,
	s store.Store,
	ebayClient ebay.EbayClient,
	extractor extract.Extractor,
	eng *engine.Engine,
	rl *ebay.RateLimiter,
	langfuseEndpoint string,
) {
	// Health endpoints (Huma).
	healthH := handlers.NewHealthHandler(s)
	handlers.RegisterHealthRoutes(humaAPI, healthH)

	// Quota endpoint (always registered; returns zeroes when rl is nil).
	quotaH := handlers.NewQuotaHandler(rl)
	handlers.RegisterQuotaRoutes(humaAPI, quotaH)

	// Store-dependent routes (Huma).
	if s != nil {
		listingsH := handlers.NewListingsHandler(s)
		handlers.RegisterListingRoutes(humaAPI, listingsH)

		watchH := handlers.NewWatchHandler(s)
		handlers.RegisterWatchRoutes(humaAPI, watchH)

		rescoreH := handlers.NewRescoreHandler(eng)
		handlers.RegisterRescoreRoutes(humaAPI, rescoreH)

		baselinesH := handlers.NewBaselinesHandler(s)
		handlers.RegisterBaselineRoutes(humaAPI, baselinesH)

		extractionStatsH := handlers.NewExtractionStatsHandler(s)
		handlers.RegisterExtractionStatsRoutes(humaAPI, extractionStatsH)

		systemStateH := handlers.NewSystemStateHandler(s)
		handlers.RegisterSystemStateRoutes(humaAPI, systemStateH)

		alertsAPI := handlers.NewAlertsAPIHandler(s, langfuseEndpoint)
		handlers.RegisterAlertsAPIRoutes(humaAPI, alertsAPI)

		// Judge manual-trigger endpoint. Always registered so the
		// OpenAPI surface stays stable; the handler responds 503 when
		// the worker isn't configured.
		var judgeRunner handlers.JudgeRunner
		if judgeWorker != nil {
			judgeRunner = judgeWorker
		}
		handlers.RegisterJudgeRoutes(humaAPI, handlers.NewJudgeHandler(judgeRunner))

		jobsH := handlers.NewJobsHandler(s)
		handlers.RegisterJobRoutes(humaAPI, jobsH)
	}

	// Search (Huma).
	if ebayClient != nil {
		searchH := handlers.NewSearchHandler(ebayClient)
		handlers.RegisterSearchRoutes(humaAPI, searchH)
	}

	// Extract (Huma).
	if extractor != nil {
		extractH := handlers.NewExtractHandler(extractor)
		handlers.RegisterExtractRoutes(humaAPI, extractH)
	}

	// Engine-dependent routes (Huma for ingest and baseline refresh).
	if eng != nil {
		ingestH := handlers.NewIngestHandler(eng)
		baselineH := handlers.NewBaselineRefreshHandler(eng)
		handlers.RegisterTriggerRoutes(humaAPI, ingestH, baselineH)

		reextractH := handlers.NewReExtractHandler(eng)
		handlers.RegisterReExtractRoutes(humaAPI, reextractH)
	}
}

func buildEbayClient(
	cfg *config.Config,
	logger *slog.Logger,
) (ebay.EbayClient, *ebay.RateLimiter, *ebay.AnalyticsClient) {
	if cfg.Ebay.AppID == "" || cfg.Ebay.CertID == "" {
		logger.Warn("ebay client disabled: AppID or CertID not configured")
		return nil, nil, nil
	}

	rl := ebay.NewRateLimiter(
		cfg.Ebay.RateLimit.PerSecond,
		cfg.Ebay.RateLimit.Burst,
		cfg.Ebay.RateLimit.DailyLimit,
	)
	logger.Info("rate limiter configured",
		"per_second", cfg.Ebay.RateLimit.PerSecond,
		"burst", cfg.Ebay.RateLimit.Burst,
		"daily_limit", cfg.Ebay.RateLimit.DailyLimit,
	)

	tokenProvider := ebay.NewOAuthTokenProvider(
		cfg.Ebay.AppID,
		cfg.Ebay.CertID,
		ebay.WithTokenURL(cfg.Ebay.TokenURL),
	)
	client := ebay.NewBrowseClient(
		tokenProvider,
		ebay.WithBrowseURL(cfg.Ebay.BrowseURL),
		ebay.WithMarketplace(cfg.Ebay.Marketplace),
		ebay.WithRateLimiter(rl),
	)
	logger.Info("ebay client configured",
		"marketplace", cfg.Ebay.Marketplace,
		"token_url", cfg.Ebay.TokenURL,
		"browse_url", cfg.Ebay.BrowseURL,
	)

	ac := ebay.NewAnalyticsClient(
		tokenProvider,
		ebay.WithAnalyticsURL(cfg.Ebay.AnalyticsURL),
	)
	logger.Info("analytics client configured",
		"analytics_url", cfg.Ebay.AnalyticsURL,
	)

	return client, rl, ac
}

func buildExtractor(cfg *config.Config, logger *slog.Logger, lf langfuse.Client) extract.Extractor {
	backend := buildLLMBackend(cfg, logger)
	if backend == nil {
		logger.Warn("llm extractor disabled")
		return nil
	}
	// Decorate with Langfuse when the client is more than the no-op.
	// NoopClient.LogGeneration is a free non-op so the wrap is safe
	// either way; the conditional is purely to preserve the
	// no-decorator span tree for tests/operators that disabled
	// observability.langfuse explicitly.
	if _, isNoop := lf.(langfuse.NoopClient); !isNoop && lf != nil {
		opts := []extract.LangfuseBackendOption{}
		if len(cfg.Observability.Langfuse.ModelCosts) > 0 {
			opts = append(opts, extract.WithModelCosts(cfg.Observability.Langfuse.ModelCosts))
			logger.Info("langfuse decorator using configured model costs",
				"model_count", len(cfg.Observability.Langfuse.ModelCosts))
		}
		backend = extract.NewLangfuseBackend(backend, lf, opts...)
		logger.Info("llm extractor wrapped with langfuse decorator")
	}
	logger.Info("llm extractor configured", "backend", cfg.LLM.Backend)
	return extract.NewLLMExtractor(
		backend,
		extract.WithLogger(logger),
		extract.WithLangfuseClient(lf),
	)
}

func buildNotifier(cfg *config.Config, logger *slog.Logger) notify.Notifier {
	if cfg.Notifications.Discord.Enabled && cfg.Notifications.Discord.WebhookURL != "" {
		opts := []notify.DiscordOption{}
		if delay := cfg.Notifications.Discord.InterChunkDelay; delay > 0 {
			opts = append(opts, notify.WithInterChunkDelay(delay))
			logger.Info("discord notifications enabled", "inter_chunk_delay", delay)
		} else {
			logger.Info("discord notifications enabled")
		}
		return notify.NewDiscordNotifier(cfg.Notifications.Discord.WebhookURL, opts...)
	}
	logger.Info("notifications disabled, using no-op notifier")
	return notify.NewNoOpNotifier(logger)
}

func buildEngine(
	cfg *config.Config,
	s store.Store,
	ebayClient ebay.EbayClient,
	extractor extract.Extractor,
	notifier notify.Notifier,
	ac *ebay.AnalyticsClient,
	rl *ebay.RateLimiter,
	lf langfuse.Client,
	logger *slog.Logger,
) (*engine.Engine, *engine.Scheduler) {
	if s == nil || ebayClient == nil || extractor == nil {
		logger.Warn("engine disabled: missing store, ebay client, or extractor")
		return nil, nil
	}

	opts := []engine.EngineOption{
		engine.WithLogger(logger),
		engine.WithBaselineWindowDays(cfg.Scoring.BaselineWindowDays),
		engine.WithStaggerOffset(cfg.Schedule.StaggerOffset),
		engine.WithAlertsConfig(cfg.Alerts),
		engine.WithAlertProcessing(engine.AlertProcessingConfig{
			SummaryOnly:   cfg.Notifications.Discord.SummaryOnly,
			AlertsURLBase: cfg.Web.AlertsURLBase,
		}),
	}

	if ac != nil {
		opts = append(opts, engine.WithAnalyticsClient(ac))
	}
	if rl != nil {
		opts = append(opts, engine.WithRateLimiter(rl))
	}

	// Wire paginator when both ebay client and store are available.
	paginator := ebay.NewPaginator(ebayClient, s,
		ebay.WithPaginatorLogger(logger),
	)
	opts = append(opts, engine.WithPaginator(paginator))
	logger.Info("paginator configured")

	if cfg.Ebay.MaxCallsPerCycle > 0 {
		opts = append(opts, engine.WithMaxCallsPerCycle(cfg.Ebay.MaxCallsPerCycle))
	}
	opts = append(opts, engine.WithWorkerCount(cfg.LLM.Concurrency))

	eng := engine.NewEngine(s, ebayClient, extractor, notifier, opts...)
	logger.Info("engine created")

	// Pre-warm rate limiter from persisted state (best-effort, before quota sync).
	if rl != nil {
		st, loadErr := s.LoadRateLimiterState(context.Background())
		switch {
		case loadErr == nil:
			rl.Sync(int64(st.TokensUsed), int64(st.DailyLimit), st.ResetAt)
			logger.Info("rate limiter pre-warmed from persisted state",
				"tokens_used", st.TokensUsed,
				"daily_limit", st.DailyLimit,
				"reset_at", st.ResetAt,
			)
		case !errors.Is(loadErr, pgx.ErrNoRows):
			logger.Warn("failed to load rate limiter state", "error", loadErr)
		}
	}

	// Sync eBay quota on startup (best-effort, before scheduler starts).
	eng.SyncQuota(context.Background())

	// Sync system state gauges on startup (best-effort).
	eng.SyncStateMetrics(context.Background())

	sched, err := engine.NewScheduler(
		eng,
		s,
		cfg.Schedule.IngestionInterval,
		cfg.Schedule.BaselineInterval,
		cfg.Schedule.ReExtractionInterval,
		logger,
	)
	if err != nil {
		logger.Error("scheduler creation failed", "error", err)
		return eng, nil
	}
	logger.Info("scheduler configured",
		"ingestion_interval", cfg.Schedule.IngestionInterval,
		"baseline_interval", cfg.Schedule.BaselineInterval,
	)

	// Recover any job runs that were left in 'running' state at last crash.
	sched.RecoverStaleJobRuns(context.Background())

	// Register the LLM-as-judge worker as a cron entry when enabled.
	// Skipped silently otherwise so judge.enabled = false matches the
	// pre-IMPL-0019 deployment shape exactly. The constructed worker
	// is also returned so registerRoutes can mount the manual-trigger
	// HTTP endpoint over the same instance — keeps the cron and HTTP
	// surfaces sharing one budget tracker.
	worker, err := registerJudgeWorker(cfg, s, lf, sched, logger)
	if err != nil {
		logger.Error("judge worker registration failed", "error", err)
	}
	judgeWorker = worker

	return eng, sched
}

// judgeWorker is set during buildEngine so registerRoutes can mount
// the HTTP handler over the same Worker instance the cron uses.
// Package-level variable rather than a return-value rewire to avoid
// churning every existing buildEngine caller — the alternative is a
// 6-arg signature that nobody else cares about.
var judgeWorker *judge.Worker

// registerJudgeWorker wires the judge.Worker into the scheduler when
// observability.judge.enabled = true. Builds a fresh LLMBackend so the
// extract pipeline and the judge each have their own client (separate
// generation names in Langfuse, no contention on a single Anthropic
// rate limiter); the same model defaults apply unless judge.model
// overrides.
//
// Returns the constructed worker (or nil when disabled). nil is also
// returned alongside an error when construction fails — call sites
// fall back to the disabled-worker code path so judge bugs don't
// crash startup.
func registerJudgeWorker(
	cfg *config.Config,
	s store.Store,
	lf langfuse.Client,
	sched *engine.Scheduler,
	logger *slog.Logger,
) (*judge.Worker, error) {
	if !cfg.Observability.Judge.Enabled {
		return nil, nil //nolint:nilnil // documented surface: nil + nil signals "judge disabled"
	}
	backend := buildLLMBackend(cfg, logger)
	if backend == nil {
		logger.Warn("judge enabled but llm backend is nil; skipping registration")
		return nil, nil //nolint:nilnil // disabled-equivalent outcome — handler responds 503
	}
	if _, isNoop := lf.(langfuse.NoopClient); !isNoop && lf != nil {
		decoratorOpts := []extract.LangfuseBackendOption{extract.WithLangfuseGenerationName("judge-llm")}
		if len(cfg.Observability.Langfuse.ModelCosts) > 0 {
			decoratorOpts = append(decoratorOpts, extract.WithModelCosts(cfg.Observability.Langfuse.ModelCosts))
		}
		backend = extract.NewLangfuseBackend(backend, lf, decoratorOpts...)
	}
	llmJudge, err := judge.NewLLMJudge(backend,
		judge.WithModelCosts(cfg.Observability.Langfuse.ModelCosts),
	)
	if err != nil {
		return nil, fmt.Errorf("constructing LLM judge: %w", err)
	}
	worker, err := judge.NewWorker(&judge.WorkerConfig{
		Judge:          llmJudge,
		Store:          newJudgeStoreAdapter(s),
		Metrics:        metrics.JudgeRecorder{},
		Langfuse:       lf,
		Logger:         logger,
		Lookback:       cfg.Observability.Judge.Lookback,
		BatchSize:      cfg.Observability.Judge.BatchSize,
		DailyBudgetUSD: cfg.Observability.Judge.DailyBudgetUSD,
	})
	if err != nil {
		return nil, fmt.Errorf("constructing judge worker: %w", err)
	}
	if err := sched.AddJudge(cfg.Observability.Judge.Interval, worker.Run); err != nil {
		return nil, fmt.Errorf("registering judge cron entry: %w", err)
	}
	logger.Info("judge worker registered",
		"interval", cfg.Observability.Judge.Interval,
		"lookback", cfg.Observability.Judge.Lookback,
		"batch_size", cfg.Observability.Judge.BatchSize,
		"daily_budget_usd", cfg.Observability.Judge.DailyBudgetUSD,
	)
	return worker, nil
}

func buildLLMBackend(cfg *config.Config, logger *slog.Logger) extract.LLMBackend {
	switch cfg.LLM.Backend {
	case "ollama":
		if cfg.LLM.Ollama.Endpoint == "" {
			logger.Warn("ollama endpoint not configured")
			return nil
		}
		timeout := cfg.LLM.Timeout
		if timeout == 0 {
			timeout = 120 * time.Second
		}
		return extract.NewOllamaBackend(
			cfg.LLM.Ollama.Endpoint,
			cfg.LLM.Ollama.Model,
			extract.WithOllamaHTTPClient(&http.Client{Timeout: timeout}),
		)
	case "anthropic":
		return extract.NewAnthropicBackend(
			extract.WithAnthropicModel(cfg.LLM.Anthropic.Model),
		)
	case "openai_compat":
		if cfg.LLM.OpenAICompat.Endpoint == "" {
			logger.Warn("openai_compat endpoint not configured")
			return nil
		}
		return extract.NewOpenAICompatBackend(
			cfg.LLM.OpenAICompat.Endpoint,
			cfg.LLM.OpenAICompat.Model,
		)
	default:
		logger.Error("unknown LLM backend", "backend", cfg.LLM.Backend)
		return nil
	}
}
