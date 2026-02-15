package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/donaldgifford/server-price-tracker/internal/store"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// WatchHandler handles Watch CRUD operations.
type WatchHandler struct {
	store store.Store
}

// NewWatchHandler creates a new WatchHandler.
func NewWatchHandler(s store.Store) *WatchHandler {
	return &WatchHandler{store: s}
}

// --- Input/Output types ---

// ListWatchesInput is the input for listing watches.
type ListWatchesInput struct {
	Enabled string `query:"enabled" doc:"Filter by enabled status (true/false)"`
}

// ListWatchesOutput is the response for listing watches.
type ListWatchesOutput struct {
	Body []domain.Watch
}

// GetWatchInput is the input for getting a single watch.
type GetWatchInput struct {
	ID string `path:"id" doc:"Watch UUID"`
}

// GetWatchOutput is the response for getting a single watch.
type GetWatchOutput struct {
	Body domain.Watch
}

// CreateWatchInput is the input for creating a watch.
type CreateWatchInput struct {
	Body struct {
		Name           string               `json:"name" minLength:"1" doc:"Watch name"`
		SearchQuery    string               `json:"search_query" minLength:"1" doc:"eBay search query"`
		CategoryID     string               `json:"category_id,omitempty" doc:"eBay category ID"`
		ComponentType  domain.ComponentType `json:"component_type,omitempty" doc:"Component type filter"`
		Filters        domain.WatchFilters  `json:"filters,omitempty" doc:"Watch filters"`
		ScoreThreshold int                  `json:"score_threshold,omitempty" doc:"Score threshold for alerts"`
		Enabled        bool                 `json:"enabled,omitempty" doc:"Whether the watch is enabled"`
	}
}

// CreateWatchOutput is the response for creating a watch.
type CreateWatchOutput struct {
	Body domain.Watch
}

// UpdateWatchInput is the input for updating a watch.
type UpdateWatchInput struct {
	ID   string `path:"id" doc:"Watch UUID"`
	Body struct {
		Name           string               `json:"name,omitempty" doc:"Watch name"`
		SearchQuery    string               `json:"search_query,omitempty" doc:"eBay search query"`
		CategoryID     string               `json:"category_id,omitempty" doc:"eBay category ID"`
		ComponentType  domain.ComponentType `json:"component_type,omitempty" doc:"Component type filter"`
		Filters        domain.WatchFilters  `json:"filters,omitempty" doc:"Watch filters"`
		ScoreThreshold int                  `json:"score_threshold,omitempty" doc:"Score threshold for alerts"`
		Enabled        bool                 `json:"enabled,omitempty" doc:"Whether the watch is enabled"`
	}
}

// UpdateWatchOutput is the response for updating a watch.
type UpdateWatchOutput struct {
	Body domain.Watch
}

// SetWatchEnabledInput is the input for enabling/disabling a watch.
type SetWatchEnabledInput struct {
	ID   string `path:"id" doc:"Watch UUID"`
	Body struct {
		Enabled bool `json:"enabled" doc:"Whether to enable or disable the watch" example:"true"`
	}
}

// SetWatchEnabledOutput is the response for enabling/disabling a watch.
type SetWatchEnabledOutput struct {
	Body struct {
		Status string `json:"status" example:"updated" doc:"Operation result"`
	}
}

// DeleteWatchInput is the input for deleting a watch.
type DeleteWatchInput struct {
	ID string `path:"id" doc:"Watch UUID"`
}

// --- Handlers ---

// ListWatches returns all watches, optionally filtered by enabled status.
func (h *WatchHandler) ListWatches(
	ctx context.Context,
	input *ListWatchesInput,
) (*ListWatchesOutput, error) {
	enabledOnly := input.Enabled == "true"

	watches, err := h.store.ListWatches(ctx, enabledOnly)
	if err != nil {
		return nil, huma.Error500InternalServerError("listing watches: " + err.Error())
	}

	if watches == nil {
		watches = []domain.Watch{}
	}

	return &ListWatchesOutput{Body: watches}, nil
}

// GetWatch returns a single watch by ID.
func (h *WatchHandler) GetWatch(ctx context.Context, input *GetWatchInput) (*GetWatchOutput, error) {
	w, err := h.store.GetWatch(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("watch not found")
	}

	return &GetWatchOutput{Body: *w}, nil
}

// CreateWatch creates a new watch.
func (h *WatchHandler) CreateWatch(
	ctx context.Context,
	input *CreateWatchInput,
) (*CreateWatchOutput, error) {
	w := &domain.Watch{
		Name:           input.Body.Name,
		SearchQuery:    input.Body.SearchQuery,
		CategoryID:     input.Body.CategoryID,
		ComponentType:  input.Body.ComponentType,
		Filters:        input.Body.Filters,
		ScoreThreshold: input.Body.ScoreThreshold,
		Enabled:        input.Body.Enabled,
	}

	if err := h.store.CreateWatch(ctx, w); err != nil {
		return nil, huma.Error500InternalServerError("creating watch: " + err.Error())
	}

	return &CreateWatchOutput{Body: *w}, nil
}

// UpdateWatch updates an existing watch.
func (h *WatchHandler) UpdateWatch(
	ctx context.Context,
	input *UpdateWatchInput,
) (*UpdateWatchOutput, error) {
	w := &domain.Watch{
		ID:             input.ID,
		Name:           input.Body.Name,
		SearchQuery:    input.Body.SearchQuery,
		CategoryID:     input.Body.CategoryID,
		ComponentType:  input.Body.ComponentType,
		Filters:        input.Body.Filters,
		ScoreThreshold: input.Body.ScoreThreshold,
		Enabled:        input.Body.Enabled,
	}

	if err := h.store.UpdateWatch(ctx, w); err != nil {
		return nil, huma.Error500InternalServerError("updating watch: " + err.Error())
	}

	return &UpdateWatchOutput{Body: *w}, nil
}

// SetWatchEnabled enables or disables a watch.
func (h *WatchHandler) SetWatchEnabled(
	ctx context.Context,
	input *SetWatchEnabledInput,
) (*SetWatchEnabledOutput, error) {
	if err := h.store.SetWatchEnabled(ctx, input.ID, input.Body.Enabled); err != nil {
		return nil, huma.Error500InternalServerError("setting watch enabled: " + err.Error())
	}

	resp := &SetWatchEnabledOutput{}
	resp.Body.Status = "updated"
	return resp, nil
}

// DeleteWatch deletes a watch by ID.
func (h *WatchHandler) DeleteWatch(
	ctx context.Context,
	input *DeleteWatchInput,
) (*struct{}, error) {
	if err := h.store.DeleteWatch(ctx, input.ID); err != nil {
		return nil, huma.Error500InternalServerError("deleting watch: " + err.Error())
	}

	return &struct{}{}, nil
}

// RegisterWatchRoutes registers watch endpoints with the Huma API.
func RegisterWatchRoutes(api huma.API, h *WatchHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "list-watches",
		Method:      http.MethodGet,
		Path:        "/api/v1/watches",
		Summary:     "List watches",
		Description: "Returns all watches, optionally filtered by enabled status.",
		Tags:        []string{"watches"},
	}, h.ListWatches)

	huma.Register(api, huma.Operation{
		OperationID: "get-watch",
		Method:      http.MethodGet,
		Path:        "/api/v1/watches/{id}",
		Summary:     "Get a watch by ID",
		Description: "Returns a single watch by its UUID.",
		Tags:        []string{"watches"},
		Errors:      []int{http.StatusNotFound},
	}, h.GetWatch)

	huma.Register(api, huma.Operation{
		OperationID:   "create-watch",
		Method:        http.MethodPost,
		Path:          "/api/v1/watches",
		Summary:       "Create a watch",
		Description:   "Creates a new watch with the given configuration.",
		Tags:          []string{"watches"},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusInternalServerError},
	}, h.CreateWatch)

	huma.Register(api, huma.Operation{
		OperationID: "update-watch",
		Method:      http.MethodPut,
		Path:        "/api/v1/watches/{id}",
		Summary:     "Update a watch",
		Description: "Updates an existing watch by its UUID.",
		Tags:        []string{"watches"},
		Errors:      []int{http.StatusInternalServerError},
	}, h.UpdateWatch)

	huma.Register(api, huma.Operation{
		OperationID: "set-watch-enabled",
		Method:      http.MethodPut,
		Path:        "/api/v1/watches/{id}/enabled",
		Summary:     "Enable or disable a watch",
		Description: "Sets the enabled status of a watch.",
		Tags:        []string{"watches"},
		Errors:      []int{http.StatusInternalServerError},
	}, h.SetWatchEnabled)

	huma.Register(api, huma.Operation{
		OperationID: "delete-watch",
		Method:      http.MethodDelete,
		Path:        "/api/v1/watches/{id}",
		Summary:     "Delete a watch",
		Description: "Deletes a watch by its UUID.",
		Tags:        []string{"watches"},
		Errors:      []int{http.StatusInternalServerError},
	}, h.DeleteWatch)
}
