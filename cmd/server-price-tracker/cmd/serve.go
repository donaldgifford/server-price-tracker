package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	apimw "github.com/donaldgifford/server-price-tracker/internal/api/middleware"
	"github.com/donaldgifford/server-price-tracker/internal/config"
	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	"github.com/donaldgifford/server-price-tracker/internal/engine"
	"github.com/donaldgifford/server-price-tracker/internal/notify"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	sptlog "github.com/donaldgifford/server-price-tracker/pkg/logger"
)

func startServer(opts *Options) error {
	cfg, err := config.Load(opts.Config)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	slogger := sptlog.New(cfg.Logging.Level, cfg.Logging.Format)
	slog.SetDefault(slogger)

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

	// --- LLM extractor ---
	extractor := buildExtractor(cfg, slogger)

	// --- Notifier ---
	notifier := buildNotifier(cfg, slogger)

	// --- Extraction workers ---
	workerCtx, workerCancel := context.WithCancel(context.Background())

	// --- Engine + Scheduler ---
	eng, scheduler := buildEngine(
		cfg, pgStore, ebayClient, extractor, notifier,
		analyticsClient, rateLimiter, slogger,
	)
	if eng != nil {
		eng.StartExtractionWorkers(workerCtx)
		slogger.Info("extraction workers started", "count", cfg.LLM.Concurrency)
	}

	// --- Echo + Huma ---
	e, humaAPI := buildHTTPServer(slogger)

	// --- Routes ---
	registerRoutes(humaAPI, pgStore, ebayClient, extractor, eng, rateLimiter)

	// Prometheus metrics.
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	slogger.Info("starting server", "addr", addr)

	// Start scheduler.
	if scheduler != nil {
		scheduler.Start()
		scheduler.SyncNextRunTimestamps()
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

	slogger.Info("shutting down server")

	workerCancel()
	slogger.Info("extraction workers stopped")

	// Stop scheduler first.
	if scheduler != nil {
		schedCtx := scheduler.Stop()
		<-schedCtx.Done()
		slogger.Info("scheduler stopped")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}

	slogger.Info("server stopped")
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

// registerRoutes sets up all HTTP routes on the Huma API.
func registerRoutes(
	humaAPI huma.API,
	s store.Store,
	ebayClient ebay.EbayClient,
	extractor extract.Extractor,
	eng *engine.Engine,
	rl *ebay.RateLimiter,
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

		rescoreH := handlers.NewRescoreHandler(s)
		handlers.RegisterRescoreRoutes(humaAPI, rescoreH)

		baselinesH := handlers.NewBaselinesHandler(s)
		handlers.RegisterBaselineRoutes(humaAPI, baselinesH)

		extractionStatsH := handlers.NewExtractionStatsHandler(s)
		handlers.RegisterExtractionStatsRoutes(humaAPI, extractionStatsH)

		systemStateH := handlers.NewSystemStateHandler(s)
		handlers.RegisterSystemStateRoutes(humaAPI, systemStateH)

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

func buildExtractor(cfg *config.Config, logger *slog.Logger) extract.Extractor {
	backend := buildLLMBackend(cfg, logger)
	if backend == nil {
		logger.Warn("llm extractor disabled")
		return nil
	}
	logger.Info("llm extractor configured", "backend", cfg.LLM.Backend)
	return extract.NewLLMExtractor(backend, extract.WithLogger(logger))
}

func buildNotifier(cfg *config.Config, logger *slog.Logger) notify.Notifier {
	if cfg.Notifications.Discord.Enabled && cfg.Notifications.Discord.WebhookURL != "" {
		logger.Info("discord notifications enabled")
		return notify.NewDiscordNotifier(cfg.Notifications.Discord.WebhookURL)
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

	return eng, sched
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
