package ebay_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	"github.com/donaldgifford/server-price-tracker/internal/ebay/mocks"
)

// productionResponse mirrors the actual eBay Analytics API production
// response captured on 2026-02-16.
const productionResponse = `{
	"rateLimits": [{
		"apiContext": "buy",
		"apiName": "Browse",
		"apiVersion": "v1",
		"resources": [
			{
				"name": "buy.browse",
				"rates": [{
					"count": 110,
					"limit": 5000,
					"remaining": 4890,
					"reset": "2026-02-17T08:00:00.000Z",
					"timeWindow": 86400
				}]
			},
			{
				"name": "buy.browse.item.bulk",
				"rates": [{
					"count": 0,
					"limit": 5000,
					"remaining": 5000,
					"reset": "2026-02-17T08:00:00.000Z",
					"timeWindow": 86400
				}]
			}
		]
	}]
}`

func TestAnalyticsClient_GetBrowseQuota(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		tokenErr   error
		wantErr    bool
		errContain string
		wantQuota  *ebay.QuotaState
	}{
		{
			name: "successful response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				assert.Equal(t, "buy", r.URL.Query().Get("api_context"))
				assert.Equal(t, "browse", r.URL.Query().Get("api_name"))

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(productionResponse))
			},
			wantQuota: &ebay.QuotaState{
				Count:      110,
				Limit:      5000,
				Remaining:  4890,
				ResetAt:    time.Date(2026, 2, 17, 8, 0, 0, 0, time.UTC),
				TimeWindow: 86400 * time.Second,
			},
		},
		{
			name: "resource not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"rateLimits": [{
						"apiContext": "buy",
						"apiName": "Browse",
						"apiVersion": "v1",
						"resources": [{
							"name": "buy.browse.item.bulk",
							"rates": [{"count": 0, "limit": 5000, "remaining": 5000, "reset": "2026-02-17T08:00:00.000Z", "timeWindow": 86400}]
						}]
					}]
				}`))
			},
			wantErr:    true,
			errContain: `"buy.browse" not found`,
		},
		{
			name: "empty rate limits",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"rateLimits": []}`))
			},
			wantErr:    true,
			errContain: `"buy.browse" not found`,
		},
		{
			name: "empty rates array",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"rateLimits": [{
						"apiContext": "buy",
						"apiName": "Browse",
						"apiVersion": "v1",
						"resources": [{"name": "buy.browse", "rates": []}]
					}]
				}`))
			},
			wantErr:    true,
			errContain: "no rates found",
		},
		{
			name: "401 unauthorized",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"errors": [{"message": "Invalid access token"}]}`))
			},
			wantErr:    true,
			errContain: "status 401",
		},
		{
			name: "500 server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:    true,
			errContain: "status 500",
		},
		{
			name:       "token provider error",
			handler:    func(_ http.ResponseWriter, _ *http.Request) {},
			tokenErr:   assert.AnError,
			wantErr:    true,
			errContain: "getting auth token",
		},
		{
			name: "invalid JSON response",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("not valid json"))
			},
			wantErr:    true,
			errContain: "parsing analytics response",
		},
		{
			name: "malformed reset timestamp",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"rateLimits": [{
						"apiContext": "buy",
						"apiName": "Browse",
						"apiVersion": "v1",
						"resources": [{
							"name": "buy.browse",
							"rates": [{"count": 10, "limit": 5000, "remaining": 4990, "reset": "not-a-time", "timeWindow": 86400}]
						}]
					}]
				}`))
			},
			wantErr:    true,
			errContain: "parsing reset time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			mockTokens := mocks.NewMockTokenProvider(t)
			if tt.tokenErr != nil {
				mockTokens.EXPECT().
					Token(mock.Anything).
					Return("", tt.tokenErr)
			} else {
				mockTokens.EXPECT().
					Token(mock.Anything).
					Return("test-token", nil)
			}

			client := ebay.NewAnalyticsClient(
				mockTokens,
				ebay.WithAnalyticsURL(srv.URL),
			)

			quota, err := client.GetBrowseQuota(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, quota)
			assert.Equal(t, tt.wantQuota.Count, quota.Count)
			assert.Equal(t, tt.wantQuota.Limit, quota.Limit)
			assert.Equal(t, tt.wantQuota.Remaining, quota.Remaining)
			assert.True(t, tt.wantQuota.ResetAt.Equal(quota.ResetAt), "ResetAt mismatch: want %v, got %v", tt.wantQuota.ResetAt, quota.ResetAt)
			assert.Equal(t, tt.wantQuota.TimeWindow, quota.TimeWindow)
		})
	}
}
