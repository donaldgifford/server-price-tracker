package middleware

import (
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const requestIDHeader = "X-Request-ID"

// RequestLog returns Echo middleware that logs requests with structured fields.
// It generates a request ID if none is provided and propagates it through
// the response header and echo context.
func RequestLog(log *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			reqID := c.Request().Header.Get(requestIDHeader)
			if reqID == "" {
				reqID = uuid.NewString()
			}

			c.Set("request_id", reqID)
			c.Response().Header().Set(requestIDHeader, reqID)

			err := next(c)

			log.Info("request",
				"method", c.Request().Method,
				"path", c.Request().URL.Path,
				"status", c.Response().Status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", reqID,
			)

			return err
		}
	}
}
