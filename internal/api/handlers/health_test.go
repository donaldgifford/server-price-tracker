package handlers_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
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

			_, api := humatest.New(t)
			handlers.RegisterHealthRoutes(api, h)

			resp := api.Get("/healthz")
			require.Equal(t, tt.wantStatus, resp.Code)
			assert.JSONEq(t, tt.wantBody, resp.Body.String())
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
			wantBody:   `"database unavailable"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := mocks.NewMockStore(t)
			mockStore.EXPECT().Ping(mock.Anything).Return(tt.pingErr)

			h := handlers.NewHealthHandler(mockStore)

			_, api := humatest.New(t)
			handlers.RegisterHealthRoutes(api, h)

			resp := api.Get("/readyz")
			require.Equal(t, tt.wantStatus, resp.Code)
			assert.Contains(t, resp.Body.String(), tt.wantBody)
		})
	}
}
