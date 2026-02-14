// Package logger provides centralized slog.Logger construction with
// configurable level and output format (text or JSON).
package logger

import (
	"io"
	"log/slog"
	"os"
)

// New creates a *slog.Logger configured with the given level and format.
// Level: "debug", "info", "warn", "error" (default: "info").
// Format: "json" or "text" (default: "text").
// Output goes to stderr.
func New(level, format string) *slog.Logger {
	return NewWithWriter(os.Stderr, level, format)
}

// NewWithWriter creates a *slog.Logger writing to w.
// Useful for testing or redirecting output.
func NewWithWriter(w io.Writer, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: ParseLevel(level)}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	return slog.New(handler)
}

// ParseLevel converts a level string to slog.Level.
// Recognized values: "debug", "warn", "error". Everything else returns LevelInfo.
func ParseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
