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
//
// @Summary List watches
// @Description Returns all watches, optionally filtered by enabled status.
// @Tags watches
// @Produce json
// @Param enabled query string false "Filter by enabled status" Enums(true, false)
// @Success 200 {array} domain.Watch
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/watches [get]
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
//
// @Summary Get a watch by ID
// @Description Returns a single watch by its UUID.
// @Tags watches
// @Produce json
// @Param id path string true "Watch UUID"
// @Success 200 {object} domain.Watch
// @Failure 404 {object} ErrorResponse
// @Router /api/v1/watches/{id} [get]
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
//
// @Summary Create a watch
// @Description Creates a new watch with the given configuration.
// @Tags watches
// @Accept json
// @Produce json
// @Param watch body domain.Watch true "Watch to create"
// @Success 201 {object} domain.Watch
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/watches [post]
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
//
// @Summary Update a watch
// @Description Updates an existing watch by its UUID.
// @Tags watches
// @Accept json
// @Produce json
// @Param id path string true "Watch UUID"
// @Param watch body domain.Watch true "Updated watch"
// @Success 200 {object} domain.Watch
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/watches/{id} [put]
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
	Enabled bool `json:"enabled" example:"true"`
}

// SetEnabled handles PUT /api/v1/watches/:id/enabled.
//
// @Summary Enable or disable a watch
// @Description Sets the enabled status of a watch.
// @Tags watches
// @Accept json
// @Produce json
// @Param id path string true "Watch UUID"
// @Param body body setEnabledRequest true "Enabled status"
// @Success 200 {object} StatusResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/watches/{id}/enabled [put]
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
//
// @Summary Delete a watch
// @Description Deletes a watch by its UUID.
// @Tags watches
// @Param id path string true "Watch UUID"
// @Success 204
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/watches/{id} [delete]
func (h *WatchHandler) Delete(c echo.Context) error {
	id := c.Param("id")

	if err := h.store.DeleteWatch(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "deleting watch: " + err.Error(),
		})
	}

	return c.NoContent(http.StatusNoContent)
}
