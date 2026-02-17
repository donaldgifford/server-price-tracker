package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ListingsResponse wraps a paginated listings response.
type ListingsResponse struct {
	Listings []domain.Listing `json:"listings"`
	Total    int              `json:"total"`
}

// ListListingsParams defines query parameters for listing queries.
type ListListingsParams struct {
	ComponentType string
	ProductKey    string
	MinScore      int
	MaxScore      int
	Limit         int
	Offset        int
	OrderBy       string
}

// ListListings returns listings matching the given parameters.
func (c *Client) ListListings(
	ctx context.Context,
	params *ListListingsParams,
) (*ListingsResponse, error) {
	q := url.Values{}
	if params.ComponentType != "" {
		q.Set("component_type", params.ComponentType)
	}
	if params.ProductKey != "" {
		q.Set("product_key", params.ProductKey)
	}
	if params.MinScore > 0 {
		q.Set("min_score", strconv.Itoa(params.MinScore))
	}
	if params.MaxScore > 0 {
		q.Set("max_score", strconv.Itoa(params.MaxScore))
	}
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Offset > 0 {
		q.Set("offset", strconv.Itoa(params.Offset))
	}
	if params.OrderBy != "" {
		q.Set("order_by", params.OrderBy)
	}

	path := "/api/v1/listings"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var resp ListingsResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetListing returns a single listing by ID.
func (c *Client) GetListing(ctx context.Context, id string) (*domain.Listing, error) {
	var l domain.Listing
	if err := c.get(ctx, fmt.Sprintf("/api/v1/listings/%s", id), &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// Rescore triggers a full rescore of all listings.
func (c *Client) Rescore(ctx context.Context) (int, error) {
	var resp struct {
		Scored int `json:"scored"`
	}
	if err := c.post(ctx, "/api/v1/rescore", nil, &resp); err != nil {
		return 0, err
	}
	return resp.Scored, nil
}

// ReExtract triggers re-extraction of listings with incomplete data.
func (c *Client) ReExtract(ctx context.Context, componentType string, limit int) (int, error) {
	body := map[string]any{}
	if componentType != "" {
		body["component_type"] = componentType
	}
	if limit > 0 {
		body["limit"] = limit
	}

	var resp struct {
		ReExtracted int `json:"re_extracted"`
	}
	if err := c.post(ctx, "/api/v1/reextract", body, &resp); err != nil {
		return 0, err
	}
	return resp.ReExtracted, nil
}
