package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
)

// fakeRescorer satisfies handlers.Rescorer for handler-level tests.
// Real scoring + alert evaluation logic is exercised in the engine
// package; here we only verify the handler's HTTP plumbing.
type fakeRescorer struct {
	scored int
	err    error
	calls  int
}

func (f *fakeRescorer) RescoreAll(_ context.Context) (int, error) {
	f.calls++
	return f.scored, f.err
}

func TestRescoreHandler_Rescore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rescorer   *fakeRescorer
		wantStatus int
		wantBody   string
	}{
		{
			name:       "successful rescore",
			rescorer:   &fakeRescorer{scored: 1},
			wantStatus: http.StatusOK,
			wantBody:   `"scored":1`,
		},
		{
			name:       "no listings to rescore",
			rescorer:   &fakeRescorer{scored: 0},
			wantStatus: http.StatusOK,
			wantBody:   `"scored":0`,
		},
		{
			name:       "engine error returns 500",
			rescorer:   &fakeRescorer{err: errors.New("db down")},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `rescore failed`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := handlers.NewRescoreHandler(tt.rescorer)

			_, api := humatest.New(t)
			handlers.RegisterRescoreRoutes(api, h)

			resp := api.Post("/api/v1/rescore")
			require.Equal(t, tt.wantStatus, resp.Code)
			assert.Contains(t, resp.Body.String(), tt.wantBody)
			assert.Equal(t, 1, tt.rescorer.calls, "RescoreAll should be invoked exactly once")
		})
	}
}
