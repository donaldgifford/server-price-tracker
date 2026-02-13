package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ExtractHandler handles LLM extraction requests.
type ExtractHandler struct {
	extractor extract.Extractor
}

// NewExtractHandler creates a new ExtractHandler.
func NewExtractHandler(extractor extract.Extractor) *ExtractHandler {
	return &ExtractHandler{extractor: extractor}
}

type extractRequest struct {
	Title         string            `json:"title"`
	ItemSpecifics map[string]string `json:"item_specifics,omitempty"`
}

type extractResponse struct {
	ComponentType domain.ComponentType `json:"component_type"`
	Attributes    map[string]any       `json:"attributes"`
	ProductKey    string               `json:"product_key"`
}

// Extract handles POST /api/v1/extract.
func (h *ExtractHandler) Extract(c echo.Context) error {
	var req extractRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
	}

	if req.Title == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "title is required",
		})
	}

	ct, attrs, err := h.extractor.ClassifyAndExtract(
		c.Request().Context(),
		req.Title,
		req.ItemSpecifics,
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "extraction failed: " + err.Error(),
		})
	}

	pk := extract.ProductKey(string(ct), attrs)

	return c.JSON(http.StatusOK, extractResponse{
		ComponentType: ct,
		Attributes:    attrs,
		ProductKey:    pk,
	})
}
