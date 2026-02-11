// Package ebay provides an eBay Browse API client abstracted behind interfaces
// for testability.
package ebay

import (
	"context"
)

// SearchRequest defines the parameters for an eBay search.
type SearchRequest struct {
	Query      string
	CategoryID string
	Limit      int
	Offset     int
	Sort       string // "newlyListed"
	Filters    map[string]string
}

// SearchResponse holds the results of an eBay search.
type SearchResponse struct {
	Items   []ItemSummary
	Total   int
	Offset  int
	Limit   int
	HasMore bool
}

// EbayClient defines the interface for interacting with the eBay API.
type EbayClient interface {
	Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)
}

// TokenProvider defines the interface for obtaining OAuth2 tokens.
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}
