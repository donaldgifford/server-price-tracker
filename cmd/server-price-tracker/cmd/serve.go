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

	"github.com/charmbracelet/log"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	handlers "github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	apimw "github.com/donaldgifford/server-price-tracker/internal/api/middleware"
	"github.com/donaldgifford/server-price-tracker/internal/config"
	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	"github.com/donaldgifford/server-price-tracker/internal/engine"
	"github.com/donaldgifford/server-price-tracker/internal/notify"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	"github.com/donaldgifford/server-price-tracker/pkg/extract"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the API server and scheduler",
	RunE:  runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := log.NewWithOptions(os.Stderr, log.Options{
		Level: parseLogLevel(cfg.Logging.Level),
	})

	slogLevel := parseSlogLevel(cfg.Logging.Level)
	slogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slogLevel,
	}))

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

	// --- Routes ---
	registerRoutes(e, pgStore, ebayClient, extractor, eng)

	// Prometheus metrics.
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logger.Info("starting server", "addr", addr)

	// Start scheduler.
	if scheduler != nil {
		scheduler.Start()
		slogger.Info("scheduler started")
	}

	// Start server in a goroutine.
	go func() {
		if err := e.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
		}
	}()

	// Wait for interrupt signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server")

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

	logger.Info("server stopped")
	return nil
}

func registerRoutes(
	e *echo.Echo,
	s store.Store,
	ebayClient ebay.EbayClient,
	extractor extract.Extractor,
	eng *engine.Engine,
) {
	// Health endpoints.
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
	return extract.NewLLMExtractor(backend)
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

func parseLogLevel(level string) log.Level {
	switch level {
	case "debug":
		return log.DebugLevel
	case "warn":
		return log.WarnLevel
	case "error":
		return log.ErrorLevel
	default:
		return log.InfoLevel
	}
}

func parseSlogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
