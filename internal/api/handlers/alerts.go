package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/donaldgifford/server-price-tracker/internal/store"
	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
)

// AlertsAPIHandler serves the JSON `/api/v1/alerts/...` endpoints. The
// HTML alert review UI is handled separately by AlertsUIHandler — this
// type is for programmatic clients and the templ component that
// renders the View Trace deep-link.
type AlertsAPIHandler struct {
	store            store.Store
	langfuseEndpoint string
}

// NewAlertsAPIHandler returns a handler that resolves alert trace IDs
// against the configured Langfuse endpoint. When the endpoint is empty
// (langfuse disabled) every trace lookup returns 404 — there's nothing
// to deep-link to.
func NewAlertsAPIHandler(s store.Store, langfuseEndpoint string) *AlertsAPIHandler {
	return &AlertsAPIHandler{store: s, langfuseEndpoint: langfuseEndpoint}
}

// GetAlertTraceInput is the path-parameter wrapper for the trace
// lookup endpoint. Huma binds the URL `:id` segment into ID via the
// `path` tag.
type GetAlertTraceInput struct {
	ID string `path:"id" doc:"Alert ID (UUID)"`
}

// GetAlertTraceOutput is the response shape for the trace lookup. The
// URL is rendered by the operator UI as a deep-link button — empty
// string is never returned because the handler 404s when no URL can
// be built.
type GetAlertTraceOutput struct {
	Body struct {
		TraceURL string `json:"trace_url" example:"https://langfuse.example.com/trace/abc123" doc:"Deep-link to the Langfuse trace for this alert"`
	}
}

// GetAlertTrace resolves an alert ID to its Langfuse trace deep-link.
// Returns 404 when:
//   - Langfuse is disabled (endpoint config empty), or
//   - The alert has no trace_id (pre-IMPL-0019 alert), or
//   - The alert ID doesn't exist.
//
// 200 with the deep-link otherwise.
func (h *AlertsAPIHandler) GetAlertTrace(ctx context.Context, in *GetAlertTraceInput) (*GetAlertTraceOutput, error) {
	if h.langfuseEndpoint == "" {
		return nil, huma.Error404NotFound("langfuse not configured — no trace URL available")
	}

	d, err := h.store.GetAlertDetail(ctx, in.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, huma.Error404NotFound("alert not found")
		}
		return nil, huma.Error500InternalServerError("fetching alert: " + err.Error())
	}

	if d.Alert.TraceID == nil || *d.Alert.TraceID == "" {
		return nil, huma.Error404NotFound("alert has no trace ID")
	}

	url := langfuse.BuildTraceURL(h.langfuseEndpoint, *d.Alert.TraceID)
	resp := &GetAlertTraceOutput{}
	resp.Body.TraceURL = url
	return resp, nil
}

// RegisterAlertsAPIRoutes registers the JSON alert endpoints onto the
// Huma API. Single endpoint today (trace lookup); more can be added
// here without touching the larger Echo UI handler.
func RegisterAlertsAPIRoutes(api huma.API, h *AlertsAPIHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "get-alert-trace",
		Method:      http.MethodGet,
		Path:        "/api/v1/alerts/{id}/trace",
		Summary:     "Get the Langfuse trace deep-link for an alert",
		Description: "Returns the Langfuse trace URL for the given alert. Returns 404 when Langfuse is disabled or the alert has no trace ID recorded.",
		Tags:        []string{"alerts"},
	}, h.GetAlertTrace)
}
