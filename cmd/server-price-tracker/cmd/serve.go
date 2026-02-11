package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	apimw "github.com/donaldgifford/server-price-tracker/internal/api/middleware"
	"github.com/donaldgifford/server-price-tracker/internal/config"
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

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Prometheus HTTP middleware.
	e.Use(apimw.Metrics())

	// Health endpoints.
	// TODO(test): health handler uses nil store until DB wiring in Phase 3.
	// Once Store is connected, readyz will check store.Ping().
	e.GET("/healthz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	e.GET("/readyz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
	})

	// Search endpoint.
	// TODO(wire): Connect real EbayClient once eBay credentials are configured.
	// For now the route is registered but requires the serve command to build the client.

	// Prometheus metrics.
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	logger.Info("starting server", "addr", addr)

	// Start server in a goroutine.
	go func() {
		if err := e.Start(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
		}
	}()

	// TODO(test): signal handling requires process-level testing, verified manually.

	// Wait for interrupt signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server")

	// TODO(wire): Stop scheduler here once Engine is wired:
	// schedCtx := scheduler.Stop()
	// <-schedCtx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}

	logger.Info("server stopped")
	return nil
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
