package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// SystemStateProvider queries the system_state DB view.
type SystemStateProvider interface {
	GetSystemState(ctx context.Context) (*domain.SystemState, error)
}

// SystemStateHandler handles GET /api/v1/system/state.
type SystemStateHandler struct {
	store SystemStateProvider
}

// NewSystemStateHandler creates a SystemStateHandler.
func NewSystemStateHandler(s SystemStateProvider) *SystemStateHandler {
	return &SystemStateHandler{store: s}
}

// SystemStateOutput is the response for GET /api/v1/system/state.
type SystemStateOutput struct {
	Body *domain.SystemState
}

// GetSystemState returns current aggregate system counts from the DB view.
func (h *SystemStateHandler) GetSystemState(
	ctx context.Context,
	_ *struct{},
) (*SystemStateOutput, error) {
	state, err := h.store.GetSystemState(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to get system state")
	}
	return &SystemStateOutput{Body: state}, nil
}

// RegisterSystemStateRoutes registers the system state route on the Huma API.
func RegisterSystemStateRoutes(api huma.API, h *SystemStateHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "get-system-state",
		Method:      http.MethodGet,
		Path:        "/api/v1/system/state",
		Summary:     "Get system state",
		Description: "Returns aggregate system counts from the DB view.",
		Tags:        []string{"system"},
	}, h.GetSystemState)
}
