package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
)

// QuotaHandler provides the eBay API quota status endpoint.
type QuotaHandler struct {
	rl *ebay.RateLimiter
}

// NewQuotaHandler creates a new QuotaHandler.
func NewQuotaHandler(rl *ebay.RateLimiter) *QuotaHandler {
	return &QuotaHandler{rl: rl}
}

// QuotaOutput is the response body for the quota endpoint.
type QuotaOutput struct {
	Body struct {
		DailyLimit int64     `json:"daily_limit" example:"5000"                    doc:"Configured daily API call limit"`
		DailyUsed  int64     `json:"daily_used"  example:"142"                     doc:"API calls used in the current 24-hour window"`
		Remaining  int64     `json:"remaining"   example:"4858"                    doc:"API calls remaining in the current window"`
		ResetAt    time.Time `json:"reset_at"    example:"2025-06-16T14:30:00Z"    doc:"When the current 24-hour window expires"`
	}
}

// GetQuota returns the current eBay API quota status.
func (h *QuotaHandler) GetQuota(_ context.Context, _ *struct{}) (*QuotaOutput, error) {
	resp := &QuotaOutput{}
	if h.rl == nil {
		return resp, nil
	}

	resp.Body.DailyLimit = h.rl.MaxDaily()
	resp.Body.DailyUsed = h.rl.DailyCount()
	resp.Body.Remaining = h.rl.Remaining()
	resp.Body.ResetAt = h.rl.ResetAt()

	return resp, nil
}

// RegisterQuotaRoutes registers the quota endpoint with the Huma API.
func RegisterQuotaRoutes(api huma.API, h *QuotaHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "get-quota",
		Method:      http.MethodGet,
		Path:        "/api/v1/quota",
		Summary:     "Get eBay API quota status",
		Description: "Returns the current daily API call usage, remaining quota, and window reset time.",
		Tags:        []string{"ebay"},
	}, h.GetQuota)
}
