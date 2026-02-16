// Package middleware provides Echo middleware for server-price-tracker.
package middleware

import (
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
)

// metricsSkipPaths defines URL paths excluded from HTTP request metrics.
// These high-frequency operational endpoints (probes, scrapes) would
// otherwise create metric noise without actionable insight.
var metricsSkipPaths = map[string]struct{}{
	"/metrics": {},
	"/healthz": {},
	"/readyz":  {},
}

// healthGauges maps operational paths to their corresponding Prometheus gauge.
// Paths present here get a 0/1 gauge update instead of histogram/counter metrics.
var healthGauges = map[string]prometheus.Gauge{
	"/healthz": metrics.HealthzUp,
	"/readyz":  metrics.ReadyzUp,
}

// Metrics returns Echo middleware that records request duration and status.
// Operational paths (/metrics, /healthz, /readyz) are excluded from
// histogram and counter metrics. Health paths update simple up/down gauges.
func Metrics() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Path()
			if path == "" {
				path = c.Request().URL.Path
			}

			if _, skip := metricsSkipPaths[path]; skip {
				err := next(c)
				updateHealthGauge(path, c.Response().Status)
				return err
			}

			start := time.Now()

			err := next(c)

			duration := time.Since(start).Seconds()
			status := strconv.Itoa(c.Response().Status)
			method := c.Request().Method

			metrics.HTTPRequestDuration.
				WithLabelValues(method, path, status).
				Observe(duration)
			metrics.HTTPRequestsTotal.
				WithLabelValues(method, path, status).
				Inc()

			return err
		}
	}
}

// updateHealthGauge sets the gauge for a health path to 1 (success) or 0 (failure).
func updateHealthGauge(path string, status int) {
	gauge, ok := healthGauges[path]
	if !ok {
		return
	}

	if status >= 200 && status < 300 {
		gauge.Set(1)
	} else {
		gauge.Set(0)
	}
}
