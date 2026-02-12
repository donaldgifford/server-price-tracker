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
