package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/donaldgifford/server-price-tracker/internal/engine"
	"github.com/donaldgifford/server-price-tracker/internal/store"
)

// RescoreHandler handles re-scoring requests.
type RescoreHandler struct {
	store store.Store
}

// NewRescoreHandler creates a new RescoreHandler.
func NewRescoreHandler(s store.Store) *RescoreHandler {
	return &RescoreHandler{store: s}
}

// Rescore handles POST /api/v1/rescore.
//
// @Summary Re-score all listings
// @Description Recalculates composite scores for all listings using current baselines.
// @Tags scoring
// @Produce json
// @Success 200 {object} map[string]any "scored count"
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/rescore [post]
func (h *RescoreHandler) Rescore(c echo.Context) error {
	scored, err := engine.RescoreAll(c.Request().Context(), h.store)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "rescore failed: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"scored": scored,
	})
}
