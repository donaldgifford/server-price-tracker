package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/donaldgifford/server-price-tracker/internal/store"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ListingsHandler handles listing query endpoints.
type ListingsHandler struct {
	store store.Store
}

// NewListingsHandler creates a new ListingsHandler.
func NewListingsHandler(s store.Store) *ListingsHandler {
	return &ListingsHandler{store: s}
}

// --- Input/Output types ---

// ListListingsInput is the input for listing listings with optional filters.
type ListListingsInput struct {
	ComponentType string `query:"component_type" doc:"Filter by component type"       enum:"ram,drive,server,cpu,nic,other,"`
	ProductKey    string `query:"product_key"    doc:"Filter by product key"`
	MinScore      int    `query:"min_score"      doc:"Minimum composite score"                                               minimum:"0" maximum:"100"`
	MaxScore      int    `query:"max_score"      doc:"Maximum composite score"                                               minimum:"0" maximum:"100"`
	Limit         int    `query:"limit"          doc:"Number of results (default 50)"                                        minimum:"1" maximum:"1000"`
	Offset        int    `query:"offset"         doc:"Pagination offset"                                                     minimum:"0"`
	OrderBy       string `query:"order_by"       doc:"Sort field"                     enum:"score,price,first_seen_at,"`
}

// ListListingsOutput is the response for listing listings.
type ListListingsOutput struct {
	Body struct {
		Listings []domain.Listing `json:"listings"`
		Total    int              `json:"total"`
		Limit    int              `json:"limit"`
		Offset   int              `json:"offset"`
	}
}

// GetListingInput is the input for getting a single listing.
type GetListingInput struct {
	ID string `path:"id" doc:"Listing UUID"`
}

// GetListingOutput is the response for getting a single listing.
type GetListingOutput struct {
	Body domain.Listing
}

// --- Handlers ---

// ListListings returns listings with optional filters for component type, score range,
// and pagination.
func (h *ListingsHandler) ListListings(
	ctx context.Context,
	input *ListListingsInput,
) (*ListListingsOutput, error) {
	q := &store.ListingQuery{
		Offset:  input.Offset,
		OrderBy: input.OrderBy,
	}

	if input.ComponentType != "" {
		q.ComponentType = &input.ComponentType
	}

	if input.ProductKey != "" {
		q.ProductKey = &input.ProductKey
	}

	if input.MinScore != 0 {
		q.MinScore = &input.MinScore
	}

	if input.MaxScore != 0 {
		q.MaxScore = &input.MaxScore
	}

	if input.Limit != 0 {
		q.Limit = input.Limit
	}

	listings, total, err := h.store.ListListings(ctx, q)
	if err != nil {
		return nil, huma.Error500InternalServerError("listing query failed: " + err.Error())
	}

	resp := &ListListingsOutput{}
	resp.Body.Listings = listings
	resp.Body.Total = total
	resp.Body.Limit = q.Limit
	resp.Body.Offset = q.Offset

	return resp, nil
}

// GetListing returns a single listing by ID.
func (h *ListingsHandler) GetListing(
	ctx context.Context,
	input *GetListingInput,
) (*GetListingOutput, error) {
	listing, err := h.store.GetListingByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("listing not found")
	}

	return &GetListingOutput{Body: *listing}, nil
}

// RegisterListingRoutes registers listing endpoints with the Huma API.
func RegisterListingRoutes(api huma.API, h *ListingsHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "list-listings",
		Method:      http.MethodGet,
		Path:        "/api/v1/listings",
		Summary:     "List listings",
		Description: "Returns listings with optional filters for component type, score range, and pagination.",
		Tags:        []string{"listings"},
	}, h.ListListings)

	huma.Register(api, huma.Operation{
		OperationID: "get-listing",
		Method:      http.MethodGet,
		Path:        "/api/v1/listings/{id}",
		Summary:     "Get a listing by ID",
		Description: "Returns a single listing by its UUID.",
		Tags:        []string{"listings"},
		Errors:      []int{http.StatusNotFound},
	}, h.GetListing)
}
