package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// SearchHandler handles eBay search requests.
type SearchHandler struct {
	client ebay.EbayClient
}

// NewSearchHandler creates a new SearchHandler.
func NewSearchHandler(client ebay.EbayClient) *SearchHandler {
	return &SearchHandler{client: client}
}

// SearchInput is the request body for the search endpoint.
type SearchInput struct {
	Body struct {
		Query      string            `json:"query" minLength:"1" doc:"eBay search query" example:"DDR4 ECC REG 32GB server RAM"`
		CategoryID string            `json:"category_id,omitempty" doc:"eBay category ID" example:"170083"`
		Limit      int               `json:"limit,omitempty" minimum:"1" doc:"Maximum results to return (default 10)" example:"10"`
		Sort       string            `json:"sort,omitempty" doc:"Sort order" example:"newlyListed"`
		Filters    map[string]string `json:"filters,omitempty" doc:"Additional eBay filters"`
	}
}

// SearchOutput is the response body for the search endpoint.
type SearchOutput struct {
	Body struct {
		Listings []domain.Listing `json:"listings" doc:"Converted listing results"`
		Total    int              `json:"total" doc:"Total matching items"`
		HasMore  bool             `json:"has_more" doc:"Whether more results are available"`
	}
}

// Search proxies a search request to the eBay Browse API.
func (h *SearchHandler) Search(ctx context.Context, input *SearchInput) (*SearchOutput, error) {
	limit := input.Body.Limit
	if limit <= 0 {
		limit = 10
	}

	resp, err := h.client.Search(ctx, ebay.SearchRequest{
		Query:      input.Body.Query,
		CategoryID: input.Body.CategoryID,
		Limit:      limit,
		Sort:       input.Body.Sort,
		Filters:    input.Body.Filters,
	})
	if err != nil {
		return nil, huma.Error502BadGateway("eBay API error: " + err.Error())
	}

	listings := ebay.ToListings(resp.Items)

	out := &SearchOutput{}
	out.Body.Listings = listings
	out.Body.Total = resp.Total
	out.Body.HasMore = resp.HasMore
	return out, nil
}

// RegisterSearchRoutes registers search endpoints with the Huma API.
func RegisterSearchRoutes(api huma.API, h *SearchHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "search-ebay",
		Method:      http.MethodPost,
		Path:        "/api/v1/search",
		Summary:     "Search eBay listings",
		Description: "Proxies a search request to the eBay Browse API and returns raw listings.",
		Tags:        []string{"search"},
		Errors:      []int{http.StatusBadGateway},
	}, h.Search)
}
