package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// ReExtractor defines the interface for re-extraction operations.
type ReExtractor interface {
	RunReExtraction(ctx context.Context, componentType string, limit int) (int, error)
}

// ReExtractHandler handles re-extraction requests.
type ReExtractHandler struct {
	reExtractor ReExtractor
}

// NewReExtractHandler creates a new ReExtractHandler.
func NewReExtractHandler(re ReExtractor) *ReExtractHandler {
	return &ReExtractHandler{reExtractor: re}
}

// ReExtractInput is the request body for the re-extract endpoint.
type ReExtractInput struct {
	Body struct {
		ComponentType string `json:"component_type,omitempty" doc:"Filter by component type (e.g., 'ram')" example:"ram"`
		Limit         int    `json:"limit,omitempty" doc:"Max listings to re-extract (default 100)" example:"100"`
	}
}

// ReExtractOutput is the response body for the re-extract endpoint.
type ReExtractOutput struct {
	Body struct {
		ReExtracted int `json:"re_extracted" example:"42" doc:"Number of listings successfully re-extracted"`
	}
}

// ReExtract triggers re-extraction of listings with incomplete data.
func (h *ReExtractHandler) ReExtract(
	ctx context.Context,
	input *ReExtractInput,
) (*ReExtractOutput, error) {
	count, err := h.reExtractor.RunReExtraction(ctx, input.Body.ComponentType, input.Body.Limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("re-extraction failed: " + err.Error())
	}

	resp := &ReExtractOutput{}
	resp.Body.ReExtracted = count
	return resp, nil
}

// RegisterReExtractRoutes registers re-extract endpoints with the Huma API.
func RegisterReExtractRoutes(api huma.API, h *ReExtractHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "reextract-listings",
		Method:      http.MethodPost,
		Path:        "/api/v1/reextract",
		Summary:     "Re-extract listings with incomplete data",
		Description: "Re-runs LLM extraction on listings with quality issues (e.g., missing RAM speed).",
		Tags:        []string{"extract"},
		Errors:      []int{http.StatusInternalServerError},
	}, h.ReExtract)
}
