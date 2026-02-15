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
	extractMocks "github.com/donaldgifford/server-price-tracker/pkg/extract/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestExtractHandler_Extract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       any
		setupMock  func(*extractMocks.MockExtractor)
		wantStatus int
		wantBody   string
	}{
		{
			name: "valid request returns extraction",
			body: map[string]any{
				"title": "Samsung 32GB 2Rx4 PC4-2666V DDR4 ECC REG",
			},
			setupMock: func(m *extractMocks.MockExtractor) {
				m.EXPECT().
					ClassifyAndExtract(
						mock.Anything,
						"Samsung 32GB 2Rx4 PC4-2666V DDR4 ECC REG",
						mock.Anything,
					).
					Return(domain.ComponentRAM, map[string]any{
						"manufacturer": "Samsung",
						"capacity_gb":  float64(32),
						"generation":   "DDR4",
						"ecc":          true,
						"registered":   true,
					}, nil).
					Once()
			},
			wantStatus: http.StatusOK,
			wantBody:   `"component_type":"ram"`,
		},
		{
			name:       "missing title returns 422",
			body:       map[string]any{},
			setupMock:  func(_ *extractMocks.MockExtractor) {},
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   `expected required property title to be present`,
		},
		{
			name:       "empty title returns 422",
			body:       map[string]any{"title": ""},
			setupMock:  func(_ *extractMocks.MockExtractor) {},
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   `expected length >= 1`,
		},
		{
			name: "extractor error returns 500",
			body: map[string]any{"title": "test listing"},
			setupMock: func(m *extractMocks.MockExtractor) {
				m.EXPECT().
					ClassifyAndExtract(mock.Anything, mock.Anything, mock.Anything).
					Return("", nil, errors.New("LLM timeout")).
					Once()
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `extraction failed`,
		},
		{
			name:       "invalid JSON returns 400",
			body:       strings.NewReader(`not json`),
			setupMock:  func(_ *extractMocks.MockExtractor) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockExtractor := extractMocks.NewMockExtractor(t)
			tt.setupMock(mockExtractor)

			h := handlers.NewExtractHandler(mockExtractor)

			_, api := humatest.New(t)
			handlers.RegisterExtractRoutes(api, h)

			resp := api.Post("/api/v1/extract", tt.body)
			require.Equal(t, tt.wantStatus, resp.Code)
			if tt.wantBody != "" {
				assert.Contains(t, resp.Body.String(), tt.wantBody)
			}
		})
	}
}
