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

// mockExtractionStatsStore is a test double for the store methods used by ExtractionStatsHandler.
type mockExtractionStatsStore struct {
	total  int
	byType map[string]int
	err    error
}

func (m *mockExtractionStatsStore) CountIncompleteExtractions(_ context.Context) (int, error) {
	return m.total, m.err
}

func (m *mockExtractionStatsStore) CountIncompleteExtractionsByType(_ context.Context) (map[string]int, error) {
	return m.byType, m.err
}

func TestExtractionStats_Success(t *testing.T) {
	t.Parallel()

	ms := &mockExtractionStatsStore{
		total:  42,
		byType: map[string]int{"ram": 38, "drive": 4},
	}

	h := handlers.NewExtractionStatsHandler(ms)

	_, api := humatest.New(t)
	handlers.RegisterExtractionStatsRoutes(api, h)

	resp := api.Get("/api/v1/extraction/stats")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"total_incomplete":42`)
	assert.Contains(t, resp.Body.String(), `"ram":38`)
	assert.Contains(t, resp.Body.String(), `"drive":4`)
}

func TestExtractionStats_Empty(t *testing.T) {
	t.Parallel()

	ms := &mockExtractionStatsStore{
		total:  0,
		byType: map[string]int{},
	}

	h := handlers.NewExtractionStatsHandler(ms)

	_, api := humatest.New(t)
	handlers.RegisterExtractionStatsRoutes(api, h)

	resp := api.Get("/api/v1/extraction/stats")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"total_incomplete":0`)
}

func TestExtractionStats_Error(t *testing.T) {
	t.Parallel()

	ms := &mockExtractionStatsStore{
		err: errors.New("db error"),
	}

	h := handlers.NewExtractionStatsHandler(ms)

	_, api := humatest.New(t)
	handlers.RegisterExtractionStatsRoutes(api, h)

	resp := api.Get("/api/v1/extraction/stats")
	require.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.Contains(t, resp.Body.String(), "failed to count incomplete extractions")
}
