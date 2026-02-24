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
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

type mockSystemStateProvider struct {
	state *domain.SystemState
	err   error
}

func (m *mockSystemStateProvider) GetSystemState(_ context.Context) (*domain.SystemState, error) {
	return m.state, m.err
}

func TestGetSystemState_Success(t *testing.T) {
	t.Parallel()

	state := &domain.SystemState{
		WatchesTotal:   5,
		WatchesEnabled: 3,
		ListingsTotal:  1000,
		BaselinesWarm:  42,
	}

	h := handlers.NewSystemStateHandler(&mockSystemStateProvider{state: state})

	_, api := humatest.New(t)
	handlers.RegisterSystemStateRoutes(api, h)

	resp := api.Get("/api/v1/system/state")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"watches_total":5`)
	assert.Contains(t, resp.Body.String(), `"baselines_warm":42`)
}

func TestGetSystemState_Error(t *testing.T) {
	t.Parallel()

	h := handlers.NewSystemStateHandler(&mockSystemStateProvider{err: errors.New("db error")})

	_, api := humatest.New(t)
	handlers.RegisterSystemStateRoutes(api, h)

	resp := api.Get("/api/v1/system/state")
	require.Equal(t, http.StatusInternalServerError, resp.Code)
}
