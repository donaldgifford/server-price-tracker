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

func TestRecovery_NoPanic(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := Recovery(logger)(func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	err := handler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, buf.String(), "no panic should produce no log output")
}

func TestRecovery_PanicString(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/panic", http.NoBody)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := Recovery(logger)(func(_ echo.Context) error {
		panic("test panic")
	})

	err := handler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "internal server error")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "panic recovered")
	assert.Contains(t, logOutput, "test panic")
	assert.Contains(t, logOutput, "path=/panic")
}

func TestRecovery_PanicError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/crash", http.NoBody)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := Recovery(logger)(func(_ echo.Context) error {
		panic(42) // panic with non-string value
	})

	err := handler(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "42")
	assert.Contains(t, logOutput, "method=POST")
}
