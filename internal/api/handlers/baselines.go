package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/donaldgifford/server-price-tracker/internal/store"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// BaselinesHandler handles baseline read operations.
type BaselinesHandler struct {
	store store.Store
}

// NewBaselinesHandler creates a new BaselinesHandler.
func NewBaselinesHandler(s store.Store) *BaselinesHandler {
	return &BaselinesHandler{store: s}
}

// --- Input/Output types ---

// ListBaselinesOutput is the response for listing baselines.
type ListBaselinesOutput struct {
	Body []domain.PriceBaseline
}

// GetBaselineInput is the input for getting a single baseline.
type GetBaselineInput struct {
	ProductKey string `path:"product_key" doc:"Product key"`
}

// GetBaselineOutput is the response for getting a single baseline.
type GetBaselineOutput struct {
	Body domain.PriceBaseline
}

// --- Handlers ---

// ListBaselines returns all price baselines.
func (h *BaselinesHandler) ListBaselines(
	ctx context.Context,
	_ *struct{},
) (*ListBaselinesOutput, error) {
	baselines, err := h.store.ListBaselines(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list baselines: " + err.Error())
	}

	if baselines == nil {
		baselines = []domain.PriceBaseline{}
	}

	return &ListBaselinesOutput{Body: baselines}, nil
}

// GetBaseline returns a single baseline by product key.
func (h *BaselinesHandler) GetBaseline(
	ctx context.Context,
	input *GetBaselineInput,
) (*GetBaselineOutput, error) {
	b, err := h.store.GetBaseline(ctx, input.ProductKey)
	if err != nil {
		return nil, huma.Error404NotFound("baseline not found")
	}

	return &GetBaselineOutput{Body: *b}, nil
}

// RegisterBaselineRoutes registers baseline read endpoints with the Huma API.
func RegisterBaselineRoutes(api huma.API, h *BaselinesHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "list-baselines",
		Method:      http.MethodGet,
		Path:        "/api/v1/baselines",
		Summary:     "List price baselines",
		Description: "Returns all price baselines with percentile statistics.",
		Tags:        []string{"scoring"},
	}, h.ListBaselines)

	huma.Register(api, huma.Operation{
		OperationID: "get-baseline",
		Method:      http.MethodGet,
		Path:        "/api/v1/baselines/{product_key}",
		Summary:     "Get a baseline by product key",
		Description: "Returns a single price baseline for the given product key.",
		Tags:        []string{"scoring"},
		Errors:      []int{http.StatusNotFound},
	}, h.GetBaseline)
}
