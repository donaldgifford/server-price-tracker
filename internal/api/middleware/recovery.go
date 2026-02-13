package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime"

	"github.com/labstack/echo/v4"
)

// Recovery returns Echo middleware that recovers from panics, logs the stack
// trace, and returns a 500 Internal Server Error to the client.
func Recovery(log *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			defer func() {
				if r := recover(); r != nil {
					buf := make([]byte, 4096)
					n := runtime.Stack(buf, false)

					log.Error("panic recovered",
						"error", fmt.Sprint(r),
						"method", c.Request().Method,
						"path", c.Request().URL.Path,
						"stack", string(buf[:n]),
					)

					err = c.JSON(http.StatusInternalServerError, map[string]string{
						"error": "internal server error",
					})
				}
			}()
			return next(c)
		}
	}
}
