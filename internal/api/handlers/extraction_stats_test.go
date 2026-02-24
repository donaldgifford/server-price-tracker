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

type mockExtractionStatsStore struct {
	state *domain.SystemState
	err   error
}

func (m *mockExtractionStatsStore) GetSystemState(_ context.Context) (*domain.SystemState, error) {
	return m.state, m.err
}

func TestExtractionStats_Success(t *testing.T) {
	t.Parallel()

	ms := &mockExtractionStatsStore{
		state: &domain.SystemState{ListingsIncompleteExtraction: 42},
	}

	_, api := humatest.New(t)
	handlers.RegisterExtractionStatsRoutes(api, handlers.NewExtractionStatsHandler(ms))

	resp := api.Get("/api/v1/extraction/stats")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"total_incomplete":42`)
}

func TestExtractionStats_Empty(t *testing.T) {
	t.Parallel()

	ms := &mockExtractionStatsStore{
		state: &domain.SystemState{ListingsIncompleteExtraction: 0},
	}

	_, api := humatest.New(t)
	handlers.RegisterExtractionStatsRoutes(api, handlers.NewExtractionStatsHandler(ms))

	resp := api.Get("/api/v1/extraction/stats")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"total_incomplete":0`)
}

func TestExtractionStats_Error(t *testing.T) {
	t.Parallel()

	ms := &mockExtractionStatsStore{err: errors.New("db error")}

	_, api := humatest.New(t)
	handlers.RegisterExtractionStatsRoutes(api, handlers.NewExtractionStatsHandler(ms))

	resp := api.Get("/api/v1/extraction/stats")
	require.Equal(t, http.StatusInternalServerError, resp.Code)
}
