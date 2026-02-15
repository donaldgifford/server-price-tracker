// Package handlers implements HTTP handlers for the server-price-tracker API.
package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

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

// HealthOutput is the response body for health check endpoints.
type HealthOutput struct {
	Body struct {
		Status string `json:"status" example:"ok" doc:"Health status"`
	}
}

// Healthz returns 200 if the process is running.
func (*HealthHandler) Healthz(_ context.Context, _ *struct{}) (*HealthOutput, error) {
	resp := &HealthOutput{}
	resp.Body.Status = "ok"
	return resp, nil
}

// Readyz returns 200 if the database is reachable, 503 otherwise.
func (h *HealthHandler) Readyz(ctx context.Context, _ *struct{}) (*HealthOutput, error) {
	if err := h.store.Ping(ctx); err != nil {
		return nil, huma.Error503ServiceUnavailable("database unavailable")
	}
	resp := &HealthOutput{}
	resp.Body.Status = "ready"
	return resp, nil
}

// RegisterHealthRoutes registers health endpoints with the Huma API.
func RegisterHealthRoutes(api huma.API, h *HealthHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "healthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Liveness check",
		Description: "Returns 200 if the process is running.",
		Tags:        []string{"health"},
	}, h.Healthz)

	huma.Register(api, huma.Operation{
		OperationID: "readyz",
		Method:      http.MethodGet,
		Path:        "/readyz",
		Summary:     "Readiness check",
		Description: "Returns 200 if the database is reachable, 503 otherwise.",
		Tags:        []string{"health"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, h.Readyz)
}
