package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mw "github.com/donaldgifford/server-price-tracker/internal/api/middleware"
	"github.com/donaldgifford/server-price-tracker/internal/metrics"
)

func TestMetricsMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		handler    echo.HandlerFunc
		wantStatus int
	}{
		{
			name:   "records 200 response",
			method: http.MethodGet,
			path:   "/healthz",
			handler: func(c echo.Context) error {
				return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
			},
			wantStatus: http.StatusOK,
		},
		{
			name:   "records 404 response",
			method: http.MethodGet,
			path:   "/notfound",
			handler: func(c echo.Context) error {
				return c.NoContent(http.StatusNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:   "records POST request",
			method: http.MethodPost,
			path:   "/api/v1/ingest",
			handler: func(c echo.Context) error {
				return c.NoContent(http.StatusAccepted)
			},
			wantStatus: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			e.Use(mw.Metrics())
			e.Add(tt.method, tt.path, tt.handler)

			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)

			statusStr := strconv.Itoa(tt.wantStatus)

			// Verify the counter was incremented.
			counter, err := metrics.HTTPRequestsTotal.GetMetricWithLabelValues(
				tt.method, tt.path, statusStr,
			)
			require.NoError(t, err)

			m := &io_prometheus_client.Metric{}
			require.NoError(t, counter.Write(m))
			assert.Greater(t, m.GetCounter().GetValue(), float64(0))

			// Verify histogram was observed.
			observer, err := metrics.HTTPRequestDuration.GetMetricWithLabelValues(
				tt.method, tt.path, statusStr,
			)
			require.NoError(t, err)

			hm := &io_prometheus_client.Metric{}
			require.NoError(t, observer.(prometheus.Metric).Write(hm))
			assert.Positive(t, hm.GetHistogram().GetSampleCount())
		})
	}
}
