package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// Rescorer is the engine surface this handler needs. The production
// implementation is *engine.Engine; tests can pass a small fake.
type Rescorer interface {
	RescoreAll(ctx context.Context) (int, error)
}

// RescoreHandler handles re-scoring requests.
type RescoreHandler struct {
	rescorer Rescorer
}

// NewRescoreHandler creates a new RescoreHandler. The Rescorer must
// evaluate alerts after each scored listing — that is how a manual
// rescore backfills alerts for listings that became deal-worthy after
// baseline changes.
func NewRescoreHandler(r Rescorer) *RescoreHandler {
	return &RescoreHandler{rescorer: r}
}

// RescoreOutput is the response body for the rescore endpoint.
type RescoreOutput struct {
	Body struct {
		Scored int `json:"scored" example:"42" doc:"Number of listings rescored"`
	}
}

// Rescore recalculates composite scores for all listings and evaluates
// alerts for each newly-eligible listing.
func (h *RescoreHandler) Rescore(ctx context.Context, _ *struct{}) (*RescoreOutput, error) {
	scored, err := h.rescorer.RescoreAll(ctx)
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
