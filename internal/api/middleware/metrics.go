// Package middleware provides Echo middleware for server-price-tracker.
package middleware

import (
	"strconv"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
)

// Metrics returns Echo middleware that records request duration and status.
func Metrics() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			err := next(c)

			duration := time.Since(start).Seconds()
			status := strconv.Itoa(c.Response().Status)
			method := c.Request().Method
			path := c.Path()

			// Use the registered route path for labels, not the actual URL,
			// to avoid high-cardinality label values.
			if path == "" {
				path = c.Request().URL.Path
			}

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
