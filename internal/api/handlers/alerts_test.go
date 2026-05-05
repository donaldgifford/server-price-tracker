package handlers_test

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	storemocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func ptrString(s string) *string { return &s }

// TestGetAlertTrace_HappyPath: store returns an alert with a populated
// trace_id, langfuse endpoint configured → 200 with the expected URL.
func TestGetAlertTrace_HappyPath(t *testing.T) {
	t.Parallel()

	s := storemocks.NewMockStore(t)
	s.EXPECT().
		GetAlertDetail(mock.Anything, "alert-1").
		Return(&domain.AlertDetail{
			Alert: domain.Alert{ID: "alert-1", TraceID: ptrString("abc123")},
		}, nil).
		Once()

	_, api := humatest.New(t)
	h := handlers.NewAlertsAPIHandler(s, "https://langfuse.example.com")
	handlers.RegisterAlertsAPIRoutes(api, h)

	resp := api.Get("/api/v1/alerts/alert-1/trace")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `"trace_url":"https://langfuse.example.com/trace/abc123"`)
}

// TestGetAlertTrace_LangfuseDisabled: empty endpoint config → 404 even
// when the alert exists. There's nothing to deep-link to so the
// endpoint pretends it doesn't exist.
func TestGetAlertTrace_LangfuseDisabled(t *testing.T) {
	t.Parallel()

	s := storemocks.NewMockStore(t)
	// No GetAlertDetail expectation — handler must short-circuit before
	// hitting the store.

	_, api := humatest.New(t)
	h := handlers.NewAlertsAPIHandler(s, "")
	handlers.RegisterAlertsAPIRoutes(api, h)

	resp := api.Get("/api/v1/alerts/alert-1/trace")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

// TestGetAlertTrace_AlertNotFound: store returns ErrNoRows → 404.
func TestGetAlertTrace_AlertNotFound(t *testing.T) {
	t.Parallel()

	s := storemocks.NewMockStore(t)
	s.EXPECT().
		GetAlertDetail(mock.Anything, "alert-missing").
		Return(nil, pgx.ErrNoRows).
		Once()

	_, api := humatest.New(t)
	h := handlers.NewAlertsAPIHandler(s, "https://langfuse.example.com")
	handlers.RegisterAlertsAPIRoutes(api, h)

	resp := api.Get("/api/v1/alerts/alert-missing/trace")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

// TestGetAlertTrace_AlertHasNoTraceID: alert exists but trace_id is
// NULL (pre-IMPL-0019 alert that predates trace propagation) → 404.
func TestGetAlertTrace_AlertHasNoTraceID(t *testing.T) {
	t.Parallel()

	s := storemocks.NewMockStore(t)
	s.EXPECT().
		GetAlertDetail(mock.Anything, "alert-old").
		Return(&domain.AlertDetail{
			Alert: domain.Alert{ID: "alert-old", TraceID: nil},
		}, nil).
		Once()

	_, api := humatest.New(t)
	h := handlers.NewAlertsAPIHandler(s, "https://langfuse.example.com")
	handlers.RegisterAlertsAPIRoutes(api, h)

	resp := api.Get("/api/v1/alerts/alert-old/trace")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}
