package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
)

// mockReExtractor is a test double for ReExtractor.
type mockReExtractor struct {
	count int
	err   error
}

func (m *mockReExtractor) RunReExtraction(
	_ context.Context,
	_ string,
	_ int,
) (int, error) {
	return m.count, m.err
}

func TestReExtract_Success(t *testing.T) {
	t.Parallel()

	h := handlers.NewReExtractHandler(&mockReExtractor{count: 42})

	_, api := humatest.New(t)
	handlers.RegisterReExtractRoutes(api, h)

	resp := api.Post("/api/v1/reextract", strings.NewReader(`{"component_type":"ram","limit":50}`))
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"re_extracted":42`)
}

func TestReExtract_Error(t *testing.T) {
	t.Parallel()

	h := handlers.NewReExtractHandler(&mockReExtractor{err: errors.New("extraction failed")})

	_, api := humatest.New(t)
	handlers.RegisterReExtractRoutes(api, h)

	resp := api.Post("/api/v1/reextract", strings.NewReader(`{"component_type":"ram"}`))
	require.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.Contains(t, resp.Body.String(), "re-extraction failed")
}

func TestReExtract_EmptyBody(t *testing.T) {
	t.Parallel()

	h := handlers.NewReExtractHandler(&mockReExtractor{count: 10})

	_, api := humatest.New(t)
	handlers.RegisterReExtractRoutes(api, h)

	resp := api.Post("/api/v1/reextract", strings.NewReader(`{}`))
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"re_extracted":10`)
}
