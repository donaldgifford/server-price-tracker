package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/donaldgifford/server-price-tracker/internal/engine"
	"github.com/donaldgifford/server-price-tracker/internal/store"
)

// RescoreHandler handles re-scoring requests.
type RescoreHandler struct {
	store store.Store
}

// NewRescoreHandler creates a new RescoreHandler.
func NewRescoreHandler(s store.Store) *RescoreHandler {
	return &RescoreHandler{store: s}
}

// RescoreOutput is the response body for the rescore endpoint.
type RescoreOutput struct {
	Body struct {
		Scored int `json:"scored" example:"42" doc:"Number of listings rescored"`
	}
}

// Rescore recalculates composite scores for all listings.
func (h *RescoreHandler) Rescore(ctx context.Context, _ *struct{}) (*RescoreOutput, error) {
	scored, err := engine.RescoreAll(ctx, h.store)
	if err != nil {
		return nil, huma.Error500InternalServerError("rescore failed: " + err.Error())
	}

	resp := &RescoreOutput{}
	resp.Body.Scored = scored
	return resp, nil
}

// RegisterRescoreRoutes registers rescore endpoints with the Huma API.
func RegisterRescoreRoutes(api huma.API, h *RescoreHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "rescore-listings",
		Method:      http.MethodPost,
		Path:        "/api/v1/rescore",
		Summary:     "Re-score all listings",
		Description: "Recalculates composite scores for all listings using current baselines.",
		Tags:        []string{"scoring"},
		Errors:      []int{http.StatusInternalServerError},
	}, h.Rescore)
}
