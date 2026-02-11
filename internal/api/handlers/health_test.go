package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	"github.com/donaldgifford/server-price-tracker/internal/store/mocks"
)

func TestHealthz(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "returns 200 ok",
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := mocks.NewMockStore(t)
			h := handlers.NewHealthHandler(mockStore)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := h.Healthz(c)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}

func TestReadyz(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "returns 200 when store ping succeeds",
			pingErr:    nil,
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ready"}`,
		},
		{
			name:       "returns 503 when store ping fails",
			pingErr:    errors.New("connection refused"),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"unavailable"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := mocks.NewMockStore(t)
			mockStore.EXPECT().Ping(mock.Anything).Return(tt.pingErr)

			h := handlers.NewHealthHandler(mockStore)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := h.Readyz(c)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
		})
	}
}
