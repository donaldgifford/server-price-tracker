package middleware

import (
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const requestIDHeader = "X-Request-ID"

// logSuppressPaths defines URL paths for which successful requests are
// logged only once. Failures (non-2xx) are always logged at Warn level.
var logSuppressPaths = map[string]struct{}{
	"/healthz": {},
	"/readyz":  {},
	"/metrics": {},
}

// RequestLog returns Echo middleware that logs requests with structured fields.
// It generates a request ID if none is provided and propagates it through
// the response header and echo context.
//
// For paths in logSuppressPaths, only the first successful response is logged.
// Subsequent successes are suppressed. Failures are always logged at Warn level.
func RequestLog(log *slog.Logger) echo.MiddlewareFunc {
	firstLogged := make(map[string]*sync.Once, len(logSuppressPaths))
	for p := range logSuppressPaths {
		firstLogged[p] = &sync.Once{}
	}

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

			status := c.Response().Status
			attrs := []any{
				"method", c.Request().Method,
				"path", c.Request().URL.Path,
				"status", status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", reqID,
			}

			if once, ok := firstLogged[c.Request().URL.Path]; ok {
				if status >= 200 && status < 300 {
					once.Do(func() {
						log.Info("request", attrs...)
					})
				} else {
					log.Warn("request", attrs...)
				}
			} else {
				log.Info("request", attrs...)
			}

			return err
		}
	}
}
