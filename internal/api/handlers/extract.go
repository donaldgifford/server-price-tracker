package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ExtractHandler handles LLM extraction requests.
type ExtractHandler struct {
	extractor extract.Extractor
}

// NewExtractHandler creates a new ExtractHandler.
func NewExtractHandler(extractor extract.Extractor) *ExtractHandler {
	return &ExtractHandler{extractor: extractor}
}

// ExtractInput is the request body for the extract endpoint.
type ExtractInput struct {
	Body struct {
		Title         string            `json:"title" minLength:"1" doc:"Listing title to extract attributes from" example:"Samsung 32GB DDR4 2666MHz ECC REG Server RAM"`
		ItemSpecifics map[string]string `json:"item_specifics,omitempty" doc:"Optional eBay item specifics"`
	}
}

// ExtractOutput is the response body for the extract endpoint.
type ExtractOutput struct {
	Body struct {
		ComponentType domain.ComponentType `json:"component_type" example:"ram" doc:"Detected component type"`
		Attributes    map[string]any       `json:"attributes" doc:"Extracted structured attributes"`
		ProductKey    string               `json:"product_key" example:"ram:ddr4:ecc_reg:32gb:2666" doc:"Normalized product key"`
	}
}

// Extract classifies a listing title and extracts structured attributes via LLM.
func (h *ExtractHandler) Extract(ctx context.Context, input *ExtractInput) (*ExtractOutput, error) {
	ct, attrs, err := h.extractor.ClassifyAndExtract(
		ctx,
		input.Body.Title,
		input.Body.ItemSpecifics,
	)
	if err != nil {
		return nil, huma.Error500InternalServerError("extraction failed: " + err.Error())
	}

	pk := extract.ProductKey(string(ct), attrs)

	resp := &ExtractOutput{}
	resp.Body.ComponentType = ct
	resp.Body.Attributes = attrs
	resp.Body.ProductKey = pk
	return resp, nil
}

// RegisterExtractRoutes registers extract endpoints with the Huma API.
func RegisterExtractRoutes(api huma.API, h *ExtractHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "extract-attributes",
		Method:      http.MethodPost,
		Path:        "/api/v1/extract",
		Summary:     "Extract attributes from a listing title",
		Description: "Uses the configured LLM backend to classify the component type " +
			"and extract structured attributes from a listing title.",
		Tags:   []string{"extract"},
		Errors: []int{http.StatusInternalServerError},
	}, h.Extract)
}
