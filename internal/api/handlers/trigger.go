package handlers

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"
)

// Ingester defines the interface for triggering ingestion.
type Ingester interface {
	RunIngestion(ctx context.Context) error
}

// BaselineRefresher defines the interface for triggering baseline refresh.
type BaselineRefresher interface {
	RunBaselineRefresh(ctx context.Context) error
}

// IngestHandler handles manual ingestion trigger requests.
type IngestHandler struct {
	ingester Ingester
}

// NewIngestHandler creates a new IngestHandler.
func NewIngestHandler(ing Ingester) *IngestHandler {
	return &IngestHandler{ingester: ing}
}

// Ingest handles POST /api/v1/ingest.
func (h *IngestHandler) Ingest(c echo.Context) error {
	if err := h.ingester.RunIngestion(c.Request().Context()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "ingestion failed: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"status": "ingestion completed",
	})
}

// BaselineRefreshHandler handles manual baseline refresh requests.
type BaselineRefreshHandler struct {
	refresher BaselineRefresher
}

// NewBaselineRefreshHandler creates a new BaselineRefreshHandler.
func NewBaselineRefreshHandler(r BaselineRefresher) *BaselineRefreshHandler {
	return &BaselineRefreshHandler{refresher: r}
}

// Refresh handles POST /api/v1/baselines/refresh.
func (h *BaselineRefreshHandler) Refresh(c echo.Context) error {
	if err := h.refresher.RunBaselineRefresh(c.Request().Context()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "baseline refresh failed: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"status": "baseline refresh completed",
	})
}
