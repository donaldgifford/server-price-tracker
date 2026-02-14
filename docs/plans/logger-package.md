# Plan: Create `pkg/logger` Package and Centralize Logger Construction

## Context

Logger construction is currently inline in `serve.go` (lines 49-52) with level parsing in
local `parseSlogLevel` (lines 322-333). The `LoggingConfig.Format` field (`text`/`json`) is
defined in config but never wired — all output is hardcoded to `slog.NewTextHandler`. As more
packages adopt logging (extraction pipeline just added it), we need a single place that owns
"how to build a logger" so level/format settings stay consistent without each call site
reimplementing the same construction logic.

## Approach

Create a small `pkg/logger` package with a `New` constructor that takes the config's
level and format strings and returns a configured `*slog.Logger`. Move `parseSlogLevel` there
as an exported `ParseLevel`. Wire the `format` field so `json` config actually produces JSON
output. Update `serve.go` to call `logger.New()` instead of inline construction. The mock
server tool gets updated too for consistency.

## Changes

### 1. Create `pkg/logger/logger.go`

```go
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
```

### 2. Create `pkg/logger/logger_test.go`

Table-driven tests for:
- `ParseLevel` — all four valid inputs + unknown defaults to info
- `New` — returns non-nil logger (smoke test)
- `NewWithWriter` — text format writes `level=` style output; json format writes `{"level":` style output
- `NewWithWriter` — debug messages visible at debug level, suppressed at info level

### 3. Update `cmd/server-price-tracker/cmd/serve.go`

- Replace inline `slog.New(slog.NewTextHandler(...))` with:
  ```go
  slogger := sptlog.New(cfg.Logging.Level, cfg.Logging.Format)
  ```
- Delete `parseSlogLevel()` function — now in `pkg/logger`
- Keep `parseLogLevel()` (charmbracelet) — it's a different logger type
- Add import for `sptlog "github.com/donaldgifford/server-price-tracker/pkg/logger"`

### 4. Update `tools/mock-server/main.go`

- Replace inline `slog.New(slog.NewTextHandler(...))` with:
  ```go
  logger := sptlog.NewWithWriter(os.Stdout, "debug", "text")
  ```

## Files

| File | Action |
|------|--------|
| `pkg/logger/logger.go` | **New** — `New`, `NewWithWriter`, `ParseLevel` |
| `pkg/logger/logger_test.go` | **New** — table-driven tests |
| `cmd/server-price-tracker/cmd/serve.go` | Replace inline construction, delete `parseSlogLevel` |
| `tools/mock-server/main.go` | Replace inline construction |
| `docs/plans/logger-package.md` | **New** — archived copy of this plan |

## Verification

```bash
go test ./pkg/logger/...    # new package tests
make test                   # all unit tests still pass
make lint                   # no lint issues
make build                  # binary builds
```
