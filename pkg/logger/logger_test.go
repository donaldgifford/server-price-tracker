package logger_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/pkg/logger"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "info", input: "info", want: slog.LevelInfo},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
		{name: "empty defaults to info", input: "", want: slog.LevelInfo},
		{name: "unknown defaults to info", input: "trace", want: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := logger.ParseLevel(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	l := logger.New("info", "text")
	require.NotNil(t, l)
}

func TestNewWithWriter_TextFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := logger.NewWithWriter(&buf, "info", "text")
	l.Info("hello")

	output := buf.String()
	assert.Contains(t, output, "level=INFO")
	assert.Contains(t, output, "hello")
}

func TestNewWithWriter_JSONFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := logger.NewWithWriter(&buf, "info", "json")
	l.Info("hello")

	output := buf.String()
	assert.Contains(t, output, `"level":"INFO"`)
	assert.Contains(t, output, `"msg":"hello"`)
}

func TestNewWithWriter_LevelFiltering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		level      string
		logFunc    func(*slog.Logger)
		wantOutput bool
	}{
		{
			name:       "debug message visible at debug level",
			level:      "debug",
			logFunc:    func(l *slog.Logger) { l.Debug("test") },
			wantOutput: true,
		},
		{
			name:       "debug message suppressed at info level",
			level:      "info",
			logFunc:    func(l *slog.Logger) { l.Debug("test") },
			wantOutput: false,
		},
		{
			name:       "info message visible at info level",
			level:      "info",
			logFunc:    func(l *slog.Logger) { l.Info("test") },
			wantOutput: true,
		},
		{
			name:       "info message suppressed at warn level",
			level:      "warn",
			logFunc:    func(l *slog.Logger) { l.Info("test") },
			wantOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			l := logger.NewWithWriter(&buf, tt.level, "text")
			tt.logFunc(l)

			if tt.wantOutput {
				assert.NotEmpty(t, buf.String())
			} else {
				assert.Empty(t, buf.String())
			}
		})
	}
}
