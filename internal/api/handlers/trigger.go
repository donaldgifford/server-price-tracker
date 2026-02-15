package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
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

// IngestOutput is the response body for the ingest endpoint.
type IngestOutput struct {
	Body struct {
		Status string `json:"status" example:"ingestion completed" doc:"Ingestion status"`
	}
}

// Ingest triggers a full ingestion pipeline run.
func (h *IngestHandler) Ingest(ctx context.Context, _ *struct{}) (*IngestOutput, error) {
	if err := h.ingester.RunIngestion(ctx); err != nil {
		return nil, huma.Error500InternalServerError("ingestion failed: " + err.Error())
	}

	resp := &IngestOutput{}
	resp.Body.Status = "ingestion completed"
	return resp, nil
}

// BaselineRefreshHandler handles manual baseline refresh requests.
type BaselineRefreshHandler struct {
	refresher BaselineRefresher
}

// NewBaselineRefreshHandler creates a new BaselineRefreshHandler.
func NewBaselineRefreshHandler(r BaselineRefresher) *BaselineRefreshHandler {
	return &BaselineRefreshHandler{refresher: r}
}

// RefreshOutput is the response body for the baseline refresh endpoint.
type RefreshOutput struct {
	Body struct {
		Status string `json:"status" example:"baseline refresh completed" doc:"Refresh status"`
	}
}

// Refresh triggers baseline recomputation for all product keys.
func (h *BaselineRefreshHandler) Refresh(ctx context.Context, _ *struct{}) (*RefreshOutput, error) {
	if err := h.refresher.RunBaselineRefresh(ctx); err != nil {
		return nil, huma.Error500InternalServerError("baseline refresh failed: " + err.Error())
	}

	resp := &RefreshOutput{}
	resp.Body.Status = "baseline refresh completed"
	return resp, nil
}

// RegisterTriggerRoutes registers trigger endpoints with the Huma API.
func RegisterTriggerRoutes(api huma.API, ingestH *IngestHandler, baselineH *BaselineRefreshHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "trigger-ingest",
		Method:      http.MethodPost,
		Path:        "/api/v1/ingest",
		Summary:     "Trigger manual ingestion",
		Description: "Runs the full ingestion pipeline: fetch listings from eBay, " +
			"extract attributes via LLM, score, and alert.",
		Tags:   []string{"ingest"},
		Errors: []int{http.StatusInternalServerError},
	}, ingestH.Ingest)

	huma.Register(api, huma.Operation{
		OperationID: "refresh-baselines",
		Method:      http.MethodPost,
		Path:        "/api/v1/baselines/refresh",
		Summary:     "Refresh price baselines",
		Description: "Recomputes percentile price baselines for all product keys.",
		Tags:        []string{"scoring"},
		Errors:      []int{http.StatusInternalServerError},
	}, baselineH.Refresh)
}
