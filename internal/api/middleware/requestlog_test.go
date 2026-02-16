package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		method        string
		path          string
		status        int
		providedReqID string
		wantLogFields []string
	}{
		{
			name:   "logs GET request with generated ID",
			method: http.MethodGet,
			path:   "/api/v1/watches",
			status: http.StatusOK,
			wantLogFields: []string{
				"method=GET",
				"path=/api/v1/watches",
				"status=200",
				"duration_ms=",
				"request_id=",
			},
		},
		{
			name:   "logs POST request",
			method: http.MethodPost,
			path:   "/api/v1/watches",
			status: http.StatusCreated,
			wantLogFields: []string{
				"method=POST",
				"status=201",
			},
		},
		{
			name:          "uses provided request ID",
			method:        http.MethodGet,
			path:          "/test",
			status:        http.StatusOK,
			providedReqID: "custom-req-id-123",
			wantLogFields: []string{
				"request_id=custom-req-id-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, nil))

			e := echo.New()
			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			if tt.providedReqID != "" {
				req.Header.Set(requestIDHeader, tt.providedReqID)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			handler := RequestLog(logger)(func(c echo.Context) error {
				return c.NoContent(tt.status)
			})

			err := handler(c)
			require.NoError(t, err)

			logOutput := buf.String()
			for _, field := range tt.wantLogFields {
				assert.Contains(t, logOutput, field)
			}

			// Response should have the request ID header.
			respID := rec.Header().Get(requestIDHeader)
			assert.NotEmpty(t, respID)

			if tt.providedReqID != "" {
				assert.Equal(t, tt.providedReqID, respID)
			}

			// Context should have request_id.
			assert.NotEmpty(t, c.Get("request_id"))
		})
	}
}

func TestRequestLog_HealthzFirstSuccessLogged(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	e := echo.New()
	mw := RequestLog(logger)

	handler := mw(func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	// First request: should be logged.
	req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	rec := httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(req, rec)))

	assert.Contains(t, buf.String(), "path=/healthz")
	assert.Contains(t, buf.String(), "status=200")

	firstLogLen := buf.Len()

	// Second request: should NOT produce additional log output.
	req = httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	rec = httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(req, rec)))

	assert.Equal(t, firstLogLen, buf.Len(),
		"second successful healthz should not produce log output")

	// Third request: also suppressed.
	req = httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	rec = httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(req, rec)))

	assert.Equal(t, firstLogLen, buf.Len(),
		"third successful healthz should not produce log output")
}

func TestRequestLog_HealthzFailureAlwaysLogged(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	e := echo.New()
	mw := RequestLog(logger)

	handler := mw(func(c echo.Context) error {
		return c.NoContent(http.StatusServiceUnavailable)
	})

	// First failure: logged.
	req := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)
	rec := httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(req, rec)))

	assert.Contains(t, buf.String(), "path=/readyz")
	assert.Contains(t, buf.String(), "status=503")
	assert.Contains(t, buf.String(), "level=WARN")

	firstLogLen := buf.Len()

	// Second failure: also logged (failures are never suppressed).
	req = httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)
	rec = httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(req, rec)))

	assert.Greater(t, buf.Len(), firstLogLen,
		"failed readyz should always be logged")
}

func TestRequestLog_ReadyzFirstSuccessThenFailure(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	e := echo.New()
	mw := RequestLog(logger)

	callCount := 0
	handler := mw(func(c echo.Context) error {
		callCount++
		if callCount <= 2 {
			return c.NoContent(http.StatusOK)
		}
		return c.NoContent(http.StatusServiceUnavailable)
	})

	// First success: logged.
	req := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)
	rec := httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(req, rec)))

	assert.Contains(t, buf.String(), "status=200")

	firstLen := buf.Len()

	// Second success: suppressed.
	req = httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)
	rec = httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(req, rec)))

	assert.Equal(t, firstLen, buf.Len(),
		"second successful readyz should be suppressed")

	// Third call (failure): logged at WARN.
	req = httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)
	rec = httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(req, rec)))

	assert.Greater(t, buf.Len(), firstLen,
		"failure after suppressed successes should still be logged")
	assert.Contains(t, buf.String(), "status=503")
	assert.Contains(t, buf.String(), "level=WARN")
}

func TestRequestLog_NonHealthPathAlwaysLogged(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	e := echo.New()
	mw := RequestLog(logger)

	handler := mw(func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	// First request.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/watches", http.NoBody)
	rec := httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(req, rec)))

	firstLen := buf.Len()
	assert.Positive(t, firstLen)

	// Second request: should ALSO be logged.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/watches", http.NoBody)
	rec = httptest.NewRecorder()
	require.NoError(t, handler(e.NewContext(req, rec)))

	assert.Greater(t, buf.Len(), firstLen,
		"non-health paths should always be logged")
}
