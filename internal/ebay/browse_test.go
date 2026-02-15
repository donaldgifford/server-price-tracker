package ebay_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	"github.com/donaldgifford/server-price-tracker/internal/ebay/mocks"
)

func TestBrowseClient_Search(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		req        ebay.SearchRequest
		handler    http.HandlerFunc
		tokenErr   error
		wantErr    bool
		errContain string
		wantItems  int
		wantMore   bool
	}{
		{
			name: "successful search with results",
			req:  ebay.SearchRequest{Query: "32GB DDR4 ECC", Limit: 10},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				assert.Equal(t, "EBAY_US", r.Header.Get("X-EBAY-C-MARKETPLACE-ID"))
				assert.Equal(t, "32GB DDR4 ECC", r.URL.Query().Get("q"))
				assert.Equal(t, "10", r.URL.Query().Get("limit"))

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"itemSummaries": [
						{"itemId": "v1|1|0", "title": "Item 1", "price": {"value": "10.00", "currency": "USD"}, "itemWebUrl": "https://ebay.com/1"},
						{"itemId": "v1|2|0", "title": "Item 2", "price": {"value": "20.00", "currency": "USD"}, "itemWebUrl": "https://ebay.com/2"}
					],
					"total": 100,
					"offset": 0,
					"limit": 10,
					"next": "https://api.ebay.com/buy/browse/v1/item_summary/search?q=test&offset=10"
				}`))
			},
			wantItems: 2,
			wantMore:  true,
		},
		{
			name: "empty results",
			req:  ebay.SearchRequest{Query: "nonexistent item xyz"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"itemSummaries": [],
					"total": 0,
					"offset": 0,
					"limit": 50
				}`))
			},
			wantItems: 0,
			wantMore:  false,
		},
		{
			name: "401 unauthorized response",
			req:  ebay.SearchRequest{Query: "test"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"errors": [{"message": "Invalid access token"}]}`))
			},
			wantErr:    true,
			errContain: "status 401",
		},
		{
			name: "429 rate limited response",
			req:  ebay.SearchRequest{Query: "test"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"errors": [{"message": "Rate limit exceeded"}]}`))
			},
			wantErr:    true,
			errContain: "status 429",
		},
		{
			name: "500 server error response",
			req:  ebay.SearchRequest{Query: "test"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:    true,
			errContain: "status 500",
		},
		{
			name:       "token provider error",
			req:        ebay.SearchRequest{Query: "test"},
			handler:    func(_ http.ResponseWriter, _ *http.Request) {},
			tokenErr:   errors.New("token fetch failed"),
			wantErr:    true,
			errContain: "getting auth token",
		},
		{
			name: "invalid JSON response",
			req:  ebay.SearchRequest{Query: "test"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("not valid json"))
			},
			wantErr:    true,
			errContain: "parsing search response",
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

			client := ebay.NewBrowseClient(
				mockTokens,
				ebay.WithBrowseURL(srv.URL),
			)

			resp, err := client.Search(context.Background(), tt.req)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Len(t, resp.Items, tt.wantItems)
			assert.Equal(t, tt.wantMore, resp.HasMore)
		})
	}
}

func TestBrowseClient_Search_RateLimited(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"itemSummaries":[],"total":0,"offset":0,"limit":50}`))
	}))
	defer srv.Close()

	mockTokens := mocks.NewMockTokenProvider(t)
	mockTokens.EXPECT().
		Token(mock.Anything).
		Return("test-token", nil).
		Maybe()

	// Rate limiter with daily limit of 1.
	rl := ebay.NewRateLimiter(100, 10, 1)
	client := ebay.NewBrowseClient(
		mockTokens,
		ebay.WithBrowseURL(srv.URL),
		ebay.WithRateLimiter(rl),
	)

	// First call succeeds.
	_, err := client.Search(context.Background(), ebay.SearchRequest{Query: "test"})
	require.NoError(t, err)

	// Second call hits daily limit.
	_, err = client.Search(context.Background(), ebay.SearchRequest{Query: "test"})
	require.ErrorIs(t, err, ebay.ErrDailyLimitReached)
	assert.Contains(t, err.Error(), "rate limit:")
}

func TestBrowseClient_Search_NoRateLimiter(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"itemSummaries":[],"total":0,"offset":0,"limit":50}`))
	}))
	defer srv.Close()

	mockTokens := mocks.NewMockTokenProvider(t)
	mockTokens.EXPECT().
		Token(mock.Anything).
		Return("test-token", nil)

	// No rate limiter — should work as before.
	client := ebay.NewBrowseClient(
		mockTokens,
		ebay.WithBrowseURL(srv.URL),
	)

	_, err := client.Search(context.Background(), ebay.SearchRequest{Query: "test"})
	require.NoError(t, err)
}

func TestBrowseClient_Search_HTMLResponse(t *testing.T) {
	t.Parallel()

	// Edge case: eBay returns HTML instead of JSON (e.g., error page or captcha).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(
			[]byte(`<!DOCTYPE html><html><body><h1>Service Unavailable</h1></body></html>`),
		)
	}))
	defer srv.Close()

	mockTokens := mocks.NewMockTokenProvider(t)
	mockTokens.EXPECT().
		Token(mock.Anything).
		Return("test-token", nil)

	client := ebay.NewBrowseClient(mockTokens, ebay.WithBrowseURL(srv.URL))
	_, err := client.Search(context.Background(), ebay.SearchRequest{Query: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing search response")
}

func TestBrowseClient_Search_QueryParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       ebay.SearchRequest
		wantQuery map[string]string
	}{
		{
			name: "basic query with defaults",
			req:  ebay.SearchRequest{Query: "DDR4 ECC"},
			wantQuery: map[string]string{
				"q":     "DDR4 ECC",
				"limit": "50",
			},
		},
		{
			name: "with category and sort",
			req: ebay.SearchRequest{
				Query:      "server ram",
				CategoryID: "170083",
				Sort:       "newlyListed",
				Limit:      25,
			},
			wantQuery: map[string]string{
				"q":            "server ram",
				"category_ids": "170083",
				"sort":         "newlyListed",
				"limit":        "25",
			},
		},
		{
			name: "with offset for pagination",
			req: ebay.SearchRequest{
				Query:  "test",
				Limit:  10,
				Offset: 20,
			},
			wantQuery: map[string]string{
				"q":      "test",
				"limit":  "10",
				"offset": "20",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					for k, v := range tt.wantQuery {
						assert.Equalf(
							t,
							v,
							r.URL.Query().Get(k),
							"query param %q", k,
						)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(
						[]byte(`{"itemSummaries":[],"total":0,"offset":0,"limit":50}`),
					)
				}),
			)
			defer srv.Close()

			mockTokens := mocks.NewMockTokenProvider(t)
			mockTokens.EXPECT().
				Token(mock.Anything).
				Return("test-token", nil)

			client := ebay.NewBrowseClient(
				mockTokens,
				ebay.WithBrowseURL(srv.URL),
			)

			_, err := client.Search(context.Background(), tt.req)
			require.NoError(t, err)
		})
	}
}
