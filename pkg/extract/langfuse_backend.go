package extract

import (
	"context"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/donaldgifford/server-price-tracker/internal/version"
	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
)

// LangfuseBackend decorates an LLMBackend so every Generate call is
// recorded as a Langfuse generation under the active OTel trace.
//
// The decorator is transparent on the hot path:
//   - Inner Generate runs first and its response is returned to the
//     caller unchanged.
//   - The Langfuse write is fire-and-forget — pushed onto the
//     buffered client, which absorbs transient outages without
//     blocking extract latency.
//   - Errors from the inner Generate are still recorded as Langfuse
//     generations with Level=ERROR so the operator can investigate
//     failed LLM calls in the same UI as successful ones.
//
// Construct via NewLangfuseBackend; pass NoopClient (or nil) when
// observability.langfuse.enabled is false.
type LangfuseBackend struct {
	inner LLMBackend
	lf    langfuse.Client
	name  string // span/generation name; e.g., "classify-llm" / "extract-llm"
}

// LangfuseBackendOption configures the decorator.
type LangfuseBackendOption func(*LangfuseBackend)

// WithLangfuseGenerationName overrides the default generation Name
// (the inner backend's Name()). Useful when one extractor handles
// both classify and extract calls — the Phase 5 judge worker does
// this with WithLangfuseGenerationName("judge-llm").
func WithLangfuseGenerationName(name string) LangfuseBackendOption {
	return func(b *LangfuseBackend) {
		b.name = name
	}
}

// NewLangfuseBackend wraps inner with a Langfuse-recording decorator.
// When lf is nil it's treated as langfuse.NoopClient — caller doesn't
// have to branch on "is observability enabled".
func NewLangfuseBackend(inner LLMBackend, lf langfuse.Client, opts ...LangfuseBackendOption) *LangfuseBackend {
	if lf == nil {
		lf = langfuse.NoopClient{}
	}
	b := &LangfuseBackend{
		inner: inner,
		lf:    lf,
		name:  inner.Name(),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Name proxies to the inner backend's name so existing callers
// (recordTokens, slog fields, etc.) see the underlying backend
// identity, not the decorator.
func (b *LangfuseBackend) Name() string {
	return b.inner.Name()
}

// Generate runs the inner backend and records the call as a Langfuse
// generation. The Langfuse write is best-effort; failures never
// propagate to the caller.
func (b *LangfuseBackend) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	start := time.Now()
	resp, err := b.inner.Generate(ctx, req)
	end := time.Now()

	traceID := traceIDFromContext(ctx)
	if traceID == "" {
		// No active trace — Langfuse generation can't anchor to a
		// trace, and creating one synchronously would defeat the
		// async-buffer design. Skip the write.
		return resp, err
	}

	gen := buildGenerationRecord(traceID, b.name, req, resp, start, end, err)
	// Buffered client returns nil for non-blocking enqueue; HTTP
	// client errors are not fatal — the inner Generate already
	// succeeded or failed and we never want to mask its outcome.
	if logErr := b.lf.LogGeneration(ctx, gen); logErr != nil {
		// Drop on the floor: telemetry write failures are not the
		// caller's problem. The buffered drain goroutine emits the
		// metric counter that operators alert on.
		_ = logErr
	}

	return resp, err
}

// buildGenerationRecord assembles the Langfuse payload from the
// extract request + response + outcome. Pulled out as a free function
// so it's table-test-able without setting up a backend.
func buildGenerationRecord(
	traceID, name string,
	req GenerateRequest,
	resp GenerateResponse,
	start, end time.Time,
	callErr error,
) *langfuse.GenerationRecord {
	gen := &langfuse.GenerationRecord{
		TraceID:    traceID,
		Name:       name,
		Model:      resp.Model,
		Prompt:     req.Prompt,
		Completion: resp.Content,
		StartTime:  start,
		EndTime:    end,
		Usage: langfuse.TokenUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
		Metadata: map[string]string{
			"commit_sha":  version.CommitSHA,
			"semver":      version.Semver,
			"format":      req.Format,
			"max_tokens":  strconv.Itoa(req.MaxTokens),
			"temperature": strconv.FormatFloat(req.Temperature, 'f', -1, 64),
		},
		Level: langfuse.LevelDefault,
	}
	if callErr != nil {
		gen.Level = langfuse.LevelError
		gen.StatusMsg = callErr.Error()
	}
	return gen
}

// traceIDFromContext extracts the W3C trace ID from the active OTel
// span on ctx, or returns "" when no valid span is in context. Mirrors
// the helpers in internal/store and internal/engine — duplicated here
// to keep pkg/extract free of internal/* imports.
func traceIDFromContext(ctx context.Context) string {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
