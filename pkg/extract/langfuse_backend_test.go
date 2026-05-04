package extract_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	extractMocks "github.com/donaldgifford/server-price-tracker/pkg/extract/mocks"
	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
)

// fakeLangfuseClient records every Client call into thread-safe slices
// so tests can assert call shape without spinning up a real Langfuse.
type fakeLangfuseClient struct {
	generations []*langfuse.GenerationRecord
}

func (c *fakeLangfuseClient) LogGeneration(_ context.Context, gen *langfuse.GenerationRecord) error {
	c.generations = append(c.generations, gen)
	return nil
}

func (*fakeLangfuseClient) Score(_ context.Context, _, _ string, _ float64, _ string) error {
	return nil
}

func (*fakeLangfuseClient) CreateTrace(_ context.Context, _ string, _ map[string]string) (langfuse.TraceHandle, error) {
	return langfuse.TraceHandle{}, nil
}

func (*fakeLangfuseClient) CreateDatasetItem(_ context.Context, _ string, _ *langfuse.DatasetItem) error {
	return nil
}

func (*fakeLangfuseClient) CreateDatasetRun(_ context.Context, _ *langfuse.DatasetRun) error {
	return nil
}

// TestLangfuseBackend_RecordsGenerationOnSuccess covers the happy path:
// Generate succeeds, LogGeneration is called exactly once with the
// expected fields, and the inner response is returned unchanged.
func TestLangfuseBackend_RecordsGenerationOnSuccess(t *testing.T) {
	t.Parallel()

	mockBackend := extractMocks.NewMockLLMBackend(t)
	mockBackend.EXPECT().Name().Return("test-backend")
	mockBackend.EXPECT().Generate(mock.Anything, mock.Anything).
		Return(extract.GenerateResponse{
			Content: "ram",
			Model:   "test-model",
			Usage:   extract.TokenUsage{PromptTokens: 50, CompletionTokens: 5, TotalTokens: 55},
		}, nil).Once()

	lf := &fakeLangfuseClient{}
	dec := extract.NewLangfuseBackend(mockBackend, lf)

	// Need a real OTel trace context for the decorator to record —
	// the trace ID lookup uses trace.SpanFromContext.
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()

	resp, err := dec.Generate(ctx, extract.GenerateRequest{
		Prompt:      "what is this?",
		Format:      extract.FormatJSON,
		MaxTokens:   100,
		Temperature: 0.5,
	})
	require.NoError(t, err)
	assert.Equal(t, "ram", resp.Content)
	assert.Equal(t, "test-model", resp.Model)

	require.Len(t, lf.generations, 1, "exactly one Langfuse generation must be recorded")
	gen := lf.generations[0]
	assert.NotEmpty(t, gen.TraceID, "trace ID should be populated from the active span")
	assert.Equal(t, "test-backend", gen.Name, "generation name defaults to backend name")
	assert.Equal(t, "test-model", gen.Model)
	assert.Equal(t, "what is this?", gen.Prompt)
	assert.Equal(t, "ram", gen.Completion)
	assert.Equal(t, 50, gen.Usage.InputTokens)
	assert.Equal(t, 5, gen.Usage.OutputTokens)
	assert.Equal(t, langfuse.LevelDefault, gen.Level)
	assert.Equal(t, "json", gen.Metadata["format"])
	assert.Equal(t, "100", gen.Metadata["max_tokens"])
	assert.Equal(t, "0.5", gen.Metadata["temperature"])
	assert.NotEmpty(t, gen.Metadata["commit_sha"])
}

// TestLangfuseBackend_RecordsErrorOnFailedGenerate covers the error
// branch: inner Generate fails, the decorator still records a
// generation tagged ERROR with the error message in StatusMsg.
func TestLangfuseBackend_RecordsErrorOnFailedGenerate(t *testing.T) {
	t.Parallel()

	mockBackend := extractMocks.NewMockLLMBackend(t)
	mockBackend.EXPECT().Name().Return("test-backend")
	mockBackend.EXPECT().Generate(mock.Anything, mock.Anything).
		Return(extract.GenerateResponse{}, errors.New("upstream rate limited")).Once()

	lf := &fakeLangfuseClient{}
	dec := extract.NewLangfuseBackend(mockBackend, lf)

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()

	_, err := dec.Generate(ctx, extract.GenerateRequest{Prompt: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")

	require.Len(t, lf.generations, 1)
	gen := lf.generations[0]
	assert.Equal(t, langfuse.LevelError, gen.Level)
	assert.Contains(t, gen.StatusMsg, "rate limited")
}

// TestLangfuseBackend_SkipsLogWithoutActiveTrace verifies that when
// there's no OTel span on the context (observability.otel disabled),
// the decorator silently skips the Langfuse write — there's no trace
// to anchor the generation to.
func TestLangfuseBackend_SkipsLogWithoutActiveTrace(t *testing.T) {
	t.Parallel()

	mockBackend := extractMocks.NewMockLLMBackend(t)
	mockBackend.EXPECT().Name().Return("test-backend")
	mockBackend.EXPECT().Generate(mock.Anything, mock.Anything).
		Return(extract.GenerateResponse{Content: "ok", Model: "m"}, nil).Once()

	lf := &fakeLangfuseClient{}
	dec := extract.NewLangfuseBackend(mockBackend, lf)

	// Plain context — no span.
	resp, err := dec.Generate(context.Background(), extract.GenerateRequest{Prompt: "x"})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Content)

	assert.Empty(t, lf.generations,
		"no Langfuse generation should be recorded when there's no active OTel trace")
}

// TestLangfuseBackend_NilClientFallsThroughToNoop verifies that
// passing nil for the Langfuse client doesn't panic — it gets
// substituted with NoopClient at construction.
func TestLangfuseBackend_NilClientFallsThroughToNoop(t *testing.T) {
	t.Parallel()

	mockBackend := extractMocks.NewMockLLMBackend(t)
	mockBackend.EXPECT().Name().Return("test-backend")
	mockBackend.EXPECT().Generate(mock.Anything, mock.Anything).
		Return(extract.GenerateResponse{Content: "ok"}, nil).Once()

	dec := extract.NewLangfuseBackend(mockBackend, nil)

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()

	_, err := dec.Generate(ctx, extract.GenerateRequest{Prompt: "x"})
	require.NoError(t, err, "nil Langfuse client must not panic")
}

// TestLangfuseBackend_NameOverride covers WithLangfuseGenerationName.
func TestLangfuseBackend_NameOverride(t *testing.T) {
	t.Parallel()

	mockBackend := extractMocks.NewMockLLMBackend(t)
	mockBackend.EXPECT().Name().Return("ollama")
	mockBackend.EXPECT().Generate(mock.Anything, mock.Anything).
		Return(extract.GenerateResponse{Content: "ok"}, nil).Once()

	lf := &fakeLangfuseClient{}
	dec := extract.NewLangfuseBackend(mockBackend, lf, extract.WithLangfuseGenerationName("judge-llm"))

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()

	_, err := dec.Generate(ctx, extract.GenerateRequest{Prompt: "x"})
	require.NoError(t, err)

	require.Len(t, lf.generations, 1)
	assert.Equal(t, "judge-llm", lf.generations[0].Name,
		"WithLangfuseGenerationName should override the default name")
	// Backend Name() still returns the underlying backend's name —
	// the decorator's Name proxies through.
	assert.Equal(t, "ollama", dec.Name())
}
