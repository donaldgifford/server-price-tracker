package handlers

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

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

type listingsResponse struct {
	Listings []domain.Listing `json:"listings"`
	Total    int              `json:"total"`
	Limit    int              `json:"limit"`
	Offset   int              `json:"offset"`
}

// List handles GET /api/v1/listings.
//
// @Summary List listings
// @Description Returns listings with optional filters for component type, score range, and pagination.
// @Tags listings
// @Produce json
// @Param component_type query string false "Filter by component type" Enums(ram, drive, server, cpu, nic, other)
// @Param product_key query string false "Filter by product key"
// @Param min_score query int false "Minimum composite score"
// @Param max_score query int false "Maximum composite score"
// @Param limit query int false "Number of results (default 50)"
// @Param offset query int false "Pagination offset"
// @Param order_by query string false "Sort field" Enums(score, price, first_seen_at)
// @Success 200 {object} listingsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/listings [get]
func (h *ListingsHandler) List(c echo.Context) error {
	q := &store.ListingQuery{}

	if ct := c.QueryParam("component_type"); ct != "" {
		q.ComponentType = &ct
	}

	if pk := c.QueryParam("product_key"); pk != "" {
		q.ProductKey = &pk
	}

	if v := c.QueryParam("min_score"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid min_score",
			})
		}
		q.MinScore = &n
	}

	if v := c.QueryParam("max_score"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid max_score",
			})
		}
		q.MaxScore = &n
	}

	if v := c.QueryParam("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid limit",
			})
		}
		q.Limit = n
	}

	if v := c.QueryParam("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid offset",
			})
		}
		q.Offset = n
	}

	if v := c.QueryParam("order_by"); v != "" {
		q.OrderBy = v
	}

	listings, total, err := h.store.ListListings(c.Request().Context(), q)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "listing query failed: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, listingsResponse{
		Listings: listings,
		Total:    total,
		Limit:    q.Limit,
		Offset:   q.Offset,
	})
}

// GetByID handles GET /api/v1/listings/:id.
//
// @Summary Get a listing by ID
// @Description Returns a single listing by its UUID.
// @Tags listings
// @Produce json
// @Param id path string true "Listing UUID"
// @Success 200 {object} domain.Listing
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/listings/{id} [get]
func (h *ListingsHandler) GetByID(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "id is required",
		})
	}

	listing, err := h.store.GetListingByID(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "listing not found",
		})
	}

	return c.JSON(http.StatusOK, listing)
}
