package handlers_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	ebayMocks "github.com/donaldgifford/server-price-tracker/internal/ebay/mocks"
)

func TestSearchHandler_Search(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       any
		setupMock  func(*ebayMocks.MockEbayClient)
		wantStatus int
		wantBody   string
	}{
		{
			name: "valid request returns listings",
			body: map[string]any{
				"query": "32GB DDR4 ECC",
				"limit": 5,
			},
			setupMock: func(m *ebayMocks.MockEbayClient) {
				m.EXPECT().
					Search(mock.Anything, mock.MatchedBy(func(r ebay.SearchRequest) bool {
						return r.Query == "32GB DDR4 ECC" && r.Limit == 5
					})).
					Return(&ebay.SearchResponse{
						Items: []ebay.ItemSummary{
							{
								ItemID:     "v1|1|0",
								Title:      "Samsung 32GB DDR4",
								Price:      ebay.ItemPrice{Value: "45.00", Currency: "USD"},
								ItemWebURL: "https://ebay.com/1",
							},
						},
						Total:   1,
						HasMore: false,
					}, nil).Once()
			},
			wantStatus: http.StatusOK,
			wantBody:   `"total":1`,
		},
		{
			name:       "missing query returns 422",
			body:       map[string]any{"limit": 5},
			setupMock:  func(_ *ebayMocks.MockEbayClient) {},
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   `expected required property query to be present`,
		},
		{
			name:       "empty query returns 422",
			body:       map[string]any{"query": ""},
			setupMock:  func(_ *ebayMocks.MockEbayClient) {},
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   `expected length >= 1`,
		},
		{
			name: "eBay client error returns 502",
			body: map[string]any{"query": "test"},
			setupMock: func(m *ebayMocks.MockEbayClient) {
				m.EXPECT().
					Search(mock.Anything, mock.Anything).
					Return(nil, errors.New("connection refused")).
					Once()
			},
			wantStatus: http.StatusBadGateway,
			wantBody:   `eBay API error`,
		},
		{
			name:       "invalid JSON returns 400",
			body:       strings.NewReader(`not json`),
			setupMock:  func(_ *ebayMocks.MockEbayClient) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockClient := ebayMocks.NewMockEbayClient(t)
			tt.setupMock(mockClient)

			h := handlers.NewSearchHandler(mockClient)

			_, api := humatest.New(t)
			handlers.RegisterSearchRoutes(api, h)

			resp := api.Post("/api/v1/search", tt.body)
			require.Equal(t, tt.wantStatus, resp.Code)
			if tt.wantBody != "" {
				assert.Contains(t, resp.Body.String(), tt.wantBody)
			}
		})
	}
}
