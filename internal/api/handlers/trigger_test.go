package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockIngester implements Ingester for testing.
type mockIngester struct {
	err    error
	called bool
}

func (m *mockIngester) RunIngestion(_ context.Context) error {
	m.called = true
	return m.err
}

// mockRefresher implements BaselineRefresher for testing.
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
	h := NewIngestHandler(ing)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", http.NoBody)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.Ingest(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, ing.called)
	assert.Contains(t, rec.Body.String(), "ingestion completed")
}

func TestIngestHandler_Error(t *testing.T) {
	t.Parallel()

	ing := &mockIngester{err: errors.New("eBay API down")}
	h := NewIngestHandler(ing)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", http.NoBody)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.Ingest(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "ingestion failed")
}

func TestBaselineRefreshHandler_Success(t *testing.T) {
	t.Parallel()

	r := &mockRefresher{}
	h := NewBaselineRefreshHandler(r)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/baselines/refresh", http.NoBody)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.Refresh(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, r.called)
	assert.Contains(t, rec.Body.String(), "baseline refresh completed")
}

func TestBaselineRefreshHandler_Error(t *testing.T) {
	t.Parallel()

	r := &mockRefresher{err: errors.New("db connection lost")}
	h := NewBaselineRefreshHandler(r)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/baselines/refresh", http.NoBody)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.Refresh(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "baseline refresh failed")
}
