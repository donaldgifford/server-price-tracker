package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"

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

// List handles GET /api/v1/watches.
func (h *WatchHandler) List(c echo.Context) error {
	enabledOnly := c.QueryParam("enabled") == "true"

	watches, err := h.store.ListWatches(c.Request().Context(), enabledOnly)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "listing watches: " + err.Error(),
		})
	}

	if watches == nil {
		watches = []domain.Watch{}
	}

	return c.JSON(http.StatusOK, watches)
}

// Get handles GET /api/v1/watches/:id.
func (h *WatchHandler) Get(c echo.Context) error {
	id := c.Param("id")

	w, err := h.store.GetWatch(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "watch not found",
		})
	}

	return c.JSON(http.StatusOK, w)
}

// Create handles POST /api/v1/watches.
func (h *WatchHandler) Create(c echo.Context) error {
	var w domain.Watch
	if err := c.Bind(&w); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body: " + err.Error(),
		})
	}

	if w.Name == "" || w.SearchQuery == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "name and search_query are required",
		})
	}

	if err := h.store.CreateWatch(c.Request().Context(), &w); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "creating watch: " + err.Error(),
		})
	}

	return c.JSON(http.StatusCreated, w)
}

// Update handles PUT /api/v1/watches/:id.
func (h *WatchHandler) Update(c echo.Context) error {
	id := c.Param("id")

	var w domain.Watch
	if err := c.Bind(&w); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body: " + err.Error(),
		})
	}

	w.ID = id
	if err := h.store.UpdateWatch(c.Request().Context(), &w); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "updating watch: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, w)
}

type setEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// SetEnabled handles PUT /api/v1/watches/:id/enabled.
func (h *WatchHandler) SetEnabled(c echo.Context) error {
	id := c.Param("id")

	var req setEnabledRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body: " + err.Error(),
		})
	}

	if err := h.store.SetWatchEnabled(c.Request().Context(), id, req.Enabled); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "setting watch enabled: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"status": "updated",
	})
}

// Delete handles DELETE /api/v1/watches/:id.
func (h *WatchHandler) Delete(c echo.Context) error {
	id := c.Param("id")

	if err := h.store.DeleteWatch(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "deleting watch: " + err.Error(),
		})
	}

	return c.NoContent(http.StatusNoContent)
}
