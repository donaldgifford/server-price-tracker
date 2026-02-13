// Package handlers implements HTTP handlers for the server-price-tracker API.
package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/donaldgifford/server-price-tracker/internal/store"
)

// HealthHandler provides health and readiness endpoints.
type HealthHandler struct {
	store store.Store
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(s store.Store) *HealthHandler {
	return &HealthHandler{store: s}
}

// Healthz returns 200 if the process is running.
func (*HealthHandler) Healthz(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz returns 200 if the database is reachable, 503 otherwise.
func (h *HealthHandler) Readyz(c echo.Context) error {
	if err := h.store.Ping(c.Request().Context()); err != nil {
		return c.JSON(
			http.StatusServiceUnavailable,
			map[string]string{"status": "unavailable"},
		)
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
}
