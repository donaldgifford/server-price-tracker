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

// getCounterValue reads the current value of a counter metric with the given labels.
func getCounterValue(t *testing.T, method, path, status string) float64 {
	t.Helper()

	counter, err := metrics.HTTPRequestsTotal.GetMetricWithLabelValues(method, path, status)
	require.NoError(t, err)

	m := &io_prometheus_client.Metric{}
	require.NoError(t, counter.Write(m))

	return m.GetCounter().GetValue()
}

// getGaugeValue reads the current value of a Prometheus gauge.
func getGaugeValue(t *testing.T, gauge prometheus.Gauge) float64 {
	t.Helper()

	m := &io_prometheus_client.Metric{}
	require.NoError(t, gauge.(prometheus.Metric).Write(m))

	return m.GetGauge().GetValue()
}

func TestMetricsMiddleware(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		handler     echo.HandlerFunc
		wantStatus  int
		wantSkipped bool
	}{
		{
			name:   "skips /healthz from HTTP metrics",
			method: http.MethodGet,
			path:   "/healthz",
			handler: func(c echo.Context) error {
				return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
			},
			wantStatus:  http.StatusOK,
			wantSkipped: true,
		},
		{
			name:   "skips /readyz from HTTP metrics",
			method: http.MethodGet,
			path:   "/readyz",
			handler: func(c echo.Context) error {
				return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
			},
			wantStatus:  http.StatusOK,
			wantSkipped: true,
		},
		{
			name:   "skips /metrics from HTTP metrics",
			method: http.MethodGet,
			path:   "/metrics",
			handler: func(c echo.Context) error {
				return c.NoContent(http.StatusOK)
			},
			wantStatus:  http.StatusOK,
			wantSkipped: true,
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

			statusStr := strconv.Itoa(tt.wantStatus)
			counterBefore := getCounterValue(t, tt.method, tt.path, statusStr)

			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)

			if tt.wantSkipped {
				counterAfter := getCounterValue(t, tt.method, tt.path, statusStr)
				assert.Equal(t, counterBefore, counterAfter,
					"skipped path should not increment HTTP counter")
			} else {
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
			}
		})
	}
}

func TestMetricsMiddleware_HealthzGauge(t *testing.T) {
	e := echo.New()
	e.Use(mw.Metrics())

	// Register handler that returns configurable status.
	var handlerStatus int
	e.GET("/healthz", func(c echo.Context) error {
		return c.NoContent(handlerStatus)
	})

	// Success: gauge should be 1.
	handlerStatus = http.StatusOK
	req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, float64(1), getGaugeValue(t, metrics.HealthzUp))

	// Failure: gauge should be 0.
	handlerStatus = http.StatusInternalServerError
	req = httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, float64(0), getGaugeValue(t, metrics.HealthzUp))
}

func TestMetricsMiddleware_ReadyzGauge(t *testing.T) {
	e := echo.New()
	e.Use(mw.Metrics())

	var handlerStatus int
	e.GET("/readyz", func(c echo.Context) error {
		return c.NoContent(handlerStatus)
	})

	// Success: gauge should be 1.
	handlerStatus = http.StatusOK
	req := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, float64(1), getGaugeValue(t, metrics.ReadyzUp))

	// Failure: gauge should be 0.
	handlerStatus = http.StatusServiceUnavailable
	req = httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, float64(0), getGaugeValue(t, metrics.ReadyzUp))
}
