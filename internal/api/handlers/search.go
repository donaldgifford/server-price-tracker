package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
)

// SearchHandler handles eBay search requests.
type SearchHandler struct {
	client ebay.EbayClient
}

// NewSearchHandler creates a new SearchHandler.
func NewSearchHandler(client ebay.EbayClient) *SearchHandler {
	return &SearchHandler{client: client}
}

type searchRequest struct {
	Query      string            `json:"query"`
	CategoryID string            `json:"category_id,omitempty"`
	Limit      int               `json:"limit,omitempty"`
	Sort       string            `json:"sort,omitempty"`
	Filters    map[string]string `json:"filters,omitempty"`
}

// Search handles POST /api/v1/search.
func (h *SearchHandler) Search(c echo.Context) error {
	var req searchRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
	}

	if req.Query == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "query is required",
		})
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	resp, err := h.client.Search(c.Request().Context(), ebay.SearchRequest{
		Query:      req.Query,
		CategoryID: req.CategoryID,
		Limit:      limit,
		Sort:       req.Sort,
		Filters:    req.Filters,
	})
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{
			"error": "eBay API error: " + err.Error(),
		})
	}

	listings := ebay.ToListings(resp.Items)

	return c.JSON(http.StatusOK, map[string]any{
		"listings": listings,
		"total":    resp.Total,
		"has_more": resp.HasMore,
	})
}
