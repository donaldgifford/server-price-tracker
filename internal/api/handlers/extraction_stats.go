package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ExtractionStatsStore queries the system_state view for extraction quality data.
type ExtractionStatsStore interface {
	GetSystemState(ctx context.Context) (*domain.SystemState, error)
}

// ExtractionStatsHandler handles extraction quality statistics requests.
type ExtractionStatsHandler struct {
	store ExtractionStatsStore
}

// NewExtractionStatsHandler creates a new ExtractionStatsHandler.
func NewExtractionStatsHandler(s ExtractionStatsStore) *ExtractionStatsHandler {
	return &ExtractionStatsHandler{store: s}
}

// ExtractionStatsOutput is the response for extraction quality statistics.
type ExtractionStatsOutput struct {
	Body struct {
		TotalIncomplete int            `json:"total_incomplete" example:"42" doc:"Total listings with incomplete extraction"`
		ByType          map[string]int `json:"by_type" doc:"Incomplete extraction count per component type"`
	}
}

// Stats returns extraction quality statistics derived from the system_state view.
func (h *ExtractionStatsHandler) Stats(
	ctx context.Context,
	_ *struct{},
) (*ExtractionStatsOutput, error) {
	state, err := h.store.GetSystemState(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get extraction stats: " + err.Error())
	}

	resp := &ExtractionStatsOutput{}
	resp.Body.TotalIncomplete = state.ListingsIncompleteExtraction
	resp.Body.ByType = map[string]int{}
	return resp, nil
}

// RegisterExtractionStatsRoutes registers extraction stats endpoints with the Huma API.
func RegisterExtractionStatsRoutes(api huma.API, h *ExtractionStatsHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "extraction-stats",
		Method:      http.MethodGet,
		Path:        "/api/v1/extraction/stats",
		Summary:     "Get extraction quality statistics",
		Description: "Returns the total count of listings with incomplete extraction data.",
		Tags:        []string{"extract"},
	}, h.Stats)
}
