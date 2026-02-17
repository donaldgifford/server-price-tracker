package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// ExtractionStatsProvider defines the interface for extraction quality queries.
type ExtractionStatsProvider interface {
	CountIncompleteExtractions(ctx context.Context) (int, error)
	CountIncompleteExtractionsByType(ctx context.Context) (map[string]int, error)
}

// ExtractionStatsHandler handles extraction quality statistics requests.
type ExtractionStatsHandler struct {
	store ExtractionStatsProvider
}

// NewExtractionStatsHandler creates a new ExtractionStatsHandler.
func NewExtractionStatsHandler(s ExtractionStatsProvider) *ExtractionStatsHandler {
	return &ExtractionStatsHandler{store: s}
}

// ExtractionStatsOutput is the response for extraction quality statistics.
type ExtractionStatsOutput struct {
	Body struct {
		TotalIncomplete int            `json:"total_incomplete" example:"42" doc:"Total listings with incomplete extraction"`
		ByType          map[string]int `json:"by_type" doc:"Incomplete extraction count per component type"`
	}
}

// Stats returns extraction quality statistics.
func (h *ExtractionStatsHandler) Stats(
	ctx context.Context,
	_ *struct{},
) (*ExtractionStatsOutput, error) {
	total, err := h.store.CountIncompleteExtractions(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to count incomplete extractions: " + err.Error())
	}

	byType, err := h.store.CountIncompleteExtractionsByType(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to count incomplete extractions by type: " + err.Error())
	}

	if byType == nil {
		byType = map[string]int{}
	}

	resp := &ExtractionStatsOutput{}
	resp.Body.TotalIncomplete = total
	resp.Body.ByType = byType
	return resp, nil
}

// RegisterExtractionStatsRoutes registers extraction stats endpoints with the Huma API.
func RegisterExtractionStatsRoutes(api huma.API, h *ExtractionStatsHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "extraction-stats",
		Method:      http.MethodGet,
		Path:        "/api/v1/extraction/stats",
		Summary:     "Get extraction quality statistics",
		Description: "Returns the total count and per-component-type breakdown of listings with incomplete extraction data.",
		Tags:        []string{"extract"},
	}, h.Stats)
}
