package handlers_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	"github.com/donaldgifford/server-price-tracker/internal/ebay"
)

func TestGetQuota(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		rl           *ebay.RateLimiter
		preCalls     int
		wantStatus   int
		wantLimit    int64
		wantUsed     int64
		wantRemain   int64
		wantResetNil bool
	}{
		{
			name:         "nil rate limiter returns zeroes",
			rl:           nil,
			wantStatus:   http.StatusOK,
			wantLimit:    0,
			wantUsed:     0,
			wantRemain:   0,
			wantResetNil: true,
		},
		{
			name:       "fresh rate limiter",
			rl:         ebay.NewRateLimiter(100, 10, 5000),
			wantStatus: http.StatusOK,
			wantLimit:  5000,
			wantUsed:   0,
			wantRemain: 5000,
		},
		{
			name:       "rate limiter with usage",
			rl:         ebay.NewRateLimiter(100, 10, 100),
			preCalls:   3,
			wantStatus: http.StatusOK,
			wantLimit:  100,
			wantUsed:   3,
			wantRemain: 97,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Simulate some API calls.
			if tt.rl != nil {
				for range tt.preCalls {
					require.NoError(t, tt.rl.Wait(t.Context()))
				}
			}

			h := handlers.NewQuotaHandler(tt.rl)

			_, api := humatest.New(t)
			handlers.RegisterQuotaRoutes(api, h)

			resp := api.Get("/api/v1/quota")
			require.Equal(t, tt.wantStatus, resp.Code)

			body := resp.Body.String()
			assert.Contains(t, body, `"daily_limit"`)
			assert.Contains(t, body, `"daily_used"`)
			assert.Contains(t, body, `"remaining"`)
			assert.Contains(t, body, `"reset_at"`)
		})
	}
}

func TestGetQuota_ResetAtValue(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)
	rl := ebay.NewRateLimiter(
		5, 10, 5000,
		ebay.WithRateLimiterNowFunc(func() time.Time { return now }),
	)

	h := handlers.NewQuotaHandler(rl)

	_, api := humatest.New(t)
	handlers.RegisterQuotaRoutes(api, h)

	resp := api.Get("/api/v1/quota")
	require.Equal(t, http.StatusOK, resp.Code)

	// ResetAt should be 24 hours from now.
	body := resp.Body.String()
	assert.Contains(t, body, "2025-06-16T14:30:00Z")
}
