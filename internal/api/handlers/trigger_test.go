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

// mockIngester implements handlers.Ingester for testing.
type mockIngester struct {
	err    error
	called bool
}

func (m *mockIngester) RunIngestion(_ context.Context) error {
	m.called = true
	return m.err
}

// mockRefresher implements handlers.BaselineRefresher for testing.
type mockRefresher struct {
	err    error
	called bool
}

func (m *mockRefresher) RunBaselineRefresh(_ context.Context) error {
	m.called = true
	return m.err
}

func TestIngestHandler_Success(t *testing.T) {
	t.Parallel()

	ing := &mockIngester{}
	ingestH := handlers.NewIngestHandler(ing)
	baselineH := handlers.NewBaselineRefreshHandler(&mockRefresher{})

	_, api := humatest.New(t)
	handlers.RegisterTriggerRoutes(api, ingestH, baselineH)

	resp := api.Post("/api/v1/ingest")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.True(t, ing.called)
	assert.Contains(t, resp.Body.String(), "ingestion completed")
}

func TestIngestHandler_Error(t *testing.T) {
	t.Parallel()

	ing := &mockIngester{err: errors.New("eBay API down")}
	ingestH := handlers.NewIngestHandler(ing)
	baselineH := handlers.NewBaselineRefreshHandler(&mockRefresher{})

	_, api := humatest.New(t)
	handlers.RegisterTriggerRoutes(api, ingestH, baselineH)

	resp := api.Post("/api/v1/ingest")
	require.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.Contains(t, resp.Body.String(), "ingestion failed")
}

func TestBaselineRefreshHandler_Success(t *testing.T) {
	t.Parallel()

	r := &mockRefresher{}
	ingestH := handlers.NewIngestHandler(&mockIngester{})
	baselineH := handlers.NewBaselineRefreshHandler(r)

	_, api := humatest.New(t)
	handlers.RegisterTriggerRoutes(api, ingestH, baselineH)

	resp := api.Post("/api/v1/baselines/refresh")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.True(t, r.called)
	assert.Contains(t, resp.Body.String(), "baseline refresh completed")
}

func TestBaselineRefreshHandler_Error(t *testing.T) {
	t.Parallel()

	r := &mockRefresher{err: errors.New("db connection lost")}
	ingestH := handlers.NewIngestHandler(&mockIngester{})
	baselineH := handlers.NewBaselineRefreshHandler(r)

	_, api := humatest.New(t)
	handlers.RegisterTriggerRoutes(api, ingestH, baselineH)

	resp := api.Post("/api/v1/baselines/refresh")
	require.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.Contains(t, resp.Body.String(), "baseline refresh failed")
}
