package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
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
		body       string
		setupMock  func(*extractMocks.MockExtractor)
		wantStatus int
		wantBody   string
	}{
		{
			name: "valid request returns extraction",
			body: `{"title":"Samsung 32GB 2Rx4 PC4-2666V DDR4 ECC REG"}`,
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
			name:       "missing title returns 400",
			body:       `{}`,
			setupMock:  func(_ *extractMocks.MockExtractor) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   `"error":"title is required"`,
		},
		{
			name:       "empty title returns 400",
			body:       `{"title":""}`,
			setupMock:  func(_ *extractMocks.MockExtractor) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   `"error":"title is required"`,
		},
		{
			name: "extractor error returns 500",
			body: `{"title":"test listing"}`,
			setupMock: func(m *extractMocks.MockExtractor) {
				m.EXPECT().
					ClassifyAndExtract(mock.Anything, mock.Anything, mock.Anything).
					Return("", nil, errors.New("LLM timeout")).
					Once()
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `"error"`,
		},
		{
			name:       "invalid JSON returns 400",
			body:       `not json`,
			setupMock:  func(_ *extractMocks.MockExtractor) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   `"error":"invalid request body"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockExtractor := extractMocks.NewMockExtractor(t)
			tt.setupMock(mockExtractor)

			h := handlers.NewExtractHandler(mockExtractor)

			e := echo.New()
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/extract",
				strings.NewReader(tt.body),
			)
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := h.Extract(c)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.wantBody)
		})
	}
}
