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
	ebayClient := buildEbayClient(cfg, slogger)

	// --- LLM extractor ---
	extractor := buildExtractor(cfg, slogger)

	// --- Notifier ---
	notifier := buildNotifier(cfg, slogger)

	// --- Engine + Scheduler ---
	eng, scheduler := buildEngine(cfg, pgStore, ebayClient, extractor, notifier, slogger)

	// --- Echo server ---
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Recovery middleware (must be first to catch panics from other middleware).
	e.Use(apimw.Recovery(slogger))

	// Request logging middleware.
	e.Use(apimw.RequestLog(slogger))

	// Prometheus HTTP middleware.
	e.Use(apimw.Metrics())

	// --- Huma API ---
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
	humaAPI := humaecho.New(e, humaConfig)

	// --- Routes ---
	registerRoutes(e, humaAPI, pgStore, ebayClient, extractor, eng)

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

	slogger.Info("shutting down server")

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

// registerRoutes sets up all HTTP routes on the Echo instance and Huma API.
// During the migration, raw Echo routes coexist with Huma-registered operations.
// As handlers are migrated to Huma, they move from Echo registration to Huma registration.
func registerRoutes(
	e *echo.Echo,
	_ huma.API, // humaAPI — will be used as handlers are migrated
	s store.Store,
	ebayClient ebay.EbayClient,
	extractor extract.Extractor,
	eng *engine.Engine,
) {
	// Health endpoints (raw Echo — will migrate to Huma in Phase 1).
	healthH := handlers.NewHealthHandler(s)
	e.GET("/healthz", healthH.Healthz)
	e.GET("/readyz", healthH.Readyz)

	// API v1 group.
	api := e.Group("/api/v1")

	// Listings.
	if s != nil {
		listingsH := handlers.NewListingsHandler(s)
		api.GET("/listings", listingsH.List)
		api.GET("/listings/:id", listingsH.GetByID)

		watchH := handlers.NewWatchHandler(s)
		api.GET("/watches", watchH.List)
		api.GET("/watches/:id", watchH.Get)
		api.POST("/watches", watchH.Create)
		api.PUT("/watches/:id", watchH.Update)
		api.PUT("/watches/:id/enabled", watchH.SetEnabled)
		api.DELETE("/watches/:id", watchH.Delete)

		rescoreH := handlers.NewRescoreHandler(s)
		api.POST("/rescore", rescoreH.Rescore)
	}

	// Search.
	if ebayClient != nil {
		searchH := handlers.NewSearchHandler(ebayClient)
		api.POST("/search", searchH.Search)
	}

	// Extract.
	if extractor != nil {
		extractH := handlers.NewExtractHandler(extractor)
		api.POST("/extract", extractH.Extract)
	}

	// Engine-dependent routes.
	if eng != nil {
		ingestH := handlers.NewIngestHandler(eng)
		api.POST("/ingest", ingestH.Ingest)

		baselineH := handlers.NewBaselineRefreshHandler(eng)
		api.POST("/baselines/refresh", baselineH.Refresh)
	}
}

func buildEbayClient(cfg *config.Config, logger *slog.Logger) ebay.EbayClient {
	if cfg.Ebay.AppID == "" || cfg.Ebay.CertID == "" {
		logger.Warn("ebay client disabled: AppID or CertID not configured")
		return nil
	}
	tokenProvider := ebay.NewOAuthTokenProvider(
		cfg.Ebay.AppID,
		cfg.Ebay.CertID,
		ebay.WithTokenURL(cfg.Ebay.TokenURL),
	)
	client := ebay.NewBrowseClient(
		tokenProvider,
		ebay.WithBrowseURL(cfg.Ebay.BrowseURL),
		ebay.WithMarketplace(cfg.Ebay.Marketplace),
	)
	logger.Info("ebay client configured",
		"marketplace", cfg.Ebay.Marketplace,
		"token_url", cfg.Ebay.TokenURL,
		"browse_url", cfg.Ebay.BrowseURL,
	)
	return client
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
	logger *slog.Logger,
) (*engine.Engine, *engine.Scheduler) {
	if s == nil || ebayClient == nil || extractor == nil {
		logger.Warn("engine disabled: missing store, ebay client, or extractor")
		return nil, nil
	}

	eng := engine.NewEngine(
		s, ebayClient, extractor, notifier,
		engine.WithLogger(logger),
		engine.WithBaselineWindowDays(cfg.Scoring.BaselineWindowDays),
		engine.WithStaggerOffset(cfg.Schedule.StaggerOffset),
	)
	logger.Info("engine created")

	sched, err := engine.NewScheduler(
		eng,
		cfg.Schedule.IngestionInterval,
		cfg.Schedule.BaselineInterval,
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
