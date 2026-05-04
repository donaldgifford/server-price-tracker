package extract_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	extractMocks "github.com/donaldgifford/server-price-tracker/pkg/extract/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// TestClassifyAndExtract_SpanTree asserts the LLM extract pipeline emits
// the expected nested span structure under tracetest. Verifies one of
// the IMPL-0019 Phase 2 success criteria: "tracetest integration covers
// the full span tree."
//
// Path under test: title pre-class miss + accessory pre-class miss +
// specifics pre-class miss → LLM Classify → LLM Extract. This exercises
// every span emitter in the happy path.
func TestClassifyAndExtract_SpanTree(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	mockBackend := extractMocks.NewMockLLMBackend(t)
	mockBackend.EXPECT().Name().Return("test-backend").Maybe()
	mockBackend.EXPECT().Generate(mock.Anything, mock.Anything).
		Return(extract.GenerateResponse{
			Content: "ram",
			Model:   "test-model",
			Usage:   extract.TokenUsage{PromptTokens: 50, CompletionTokens: 5, TotalTokens: 55},
		}, nil).Once()
	mockBackend.EXPECT().Generate(mock.Anything, mock.Anything).
		Return(extract.GenerateResponse{
			Content: `{"capacity_gb":32,"generation":"DDR4","speed_mhz":2400,"ecc":true,"registered":true,"condition":"used","confidence":0.9}`,
			Model:   "test-model",
			Usage:   extract.TokenUsage{PromptTokens: 200, CompletionTokens: 50, TotalTokens: 250},
		}, nil).Once()

	extractor := extract.NewLLMExtractor(mockBackend, extract.WithTracer(tp.Tracer("test")))

	ctx := context.Background()
	ct, attrs, err := extractor.ClassifyAndExtract(ctx, "32GB DDR4 ECC RAM", nil)
	require.NoError(t, err)
	assert.Equal(t, domain.ComponentRAM, ct)
	assert.NotNil(t, attrs)

	spans := recorder.Ended()
	names := spanNames(spans)

	for _, expected := range []string{
		"extract.classify_and_extract",
		"extract.preclassify_title",
		"extract.preclassify_accessory",
		"extract.preclassify_specifics",
		"extract.classify",
		"extract.extract",
		"extract.parse_json",
		"extract.normalize",
		"extract.validate",
	} {
		assert.Contains(t, names, expected, "missing expected span %q", expected)
	}

	root := findSpan(spans, "extract.classify_and_extract")
	require.NotNil(t, root)
	rootCtx := root.SpanContext()

	for _, child := range []string{
		"extract.preclassify_title",
		"extract.preclassify_accessory",
		"extract.preclassify_specifics",
		"extract.classify",
		"extract.extract",
	} {
		span := findSpan(spans, child)
		require.NotNil(t, span, "missing span %s", child)
		assert.Equal(t, rootCtx.TraceID(), span.SpanContext().TraceID(),
			"span %s should share trace with root", child)
		assert.Equal(t, rootCtx.SpanID(), span.Parent().SpanID(),
			"span %s should be a child of the root", child)
	}

	extractSpan := findSpan(spans, "extract.extract")
	require.NotNil(t, extractSpan)
	for _, child := range []string{"extract.parse_json", "extract.normalize", "extract.validate"} {
		span := findSpan(spans, child)
		require.NotNil(t, span, "missing span %s", child)
		assert.Equal(t, extractSpan.SpanContext().SpanID(), span.Parent().SpanID(),
			"span %s should be a child of extract.extract", child)
	}
}

// TestClassifyAndExtract_AccessoryShortCircuit_SkipsLLMSpans verifies
// that the accessory short-circuit doesn't emit Classify / Extract spans
// (no LLM call happens).
func TestClassifyAndExtract_AccessoryShortCircuit_SkipsLLMSpans(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	mockBackend := extractMocks.NewMockLLMBackend(t)
	mockBackend.EXPECT().Name().Return("test-backend").Maybe()
	// No Generate expectations — accessory short-circuit must skip LLM.

	extractor := extract.NewLLMExtractor(mockBackend, extract.WithTracer(tp.Tracer("test")))

	ctx := context.Background()
	ct, attrs, err := extractor.ClassifyAndExtract(ctx, "Dell PowerEdge R740 Drive Caddy", nil)
	require.NoError(t, err)
	assert.Equal(t, domain.ComponentOther, ct)
	require.NotNil(t, attrs)

	names := spanNames(recorder.Ended())
	assert.Contains(t, names, "extract.classify_and_extract")
	assert.Contains(t, names, "extract.preclassify_title")
	assert.Contains(t, names, "extract.preclassify_accessory")
	assert.NotContains(t, names, "extract.classify",
		"LLM classify must not run when accessory short-circuit fired")
	assert.NotContains(t, names, "extract.extract",
		"LLM extract must not run when accessory short-circuit fired")
}

// spanNames returns just the names of recorded spans for easier
// assertion reads.
func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.Name())
	}
	return out
}

// findSpan locates a recorded span by name, returning nil when not
// found. Use require.NotNil at the call site so the test fails with a
// helpful message instead of a nil-pointer panic.
func findSpan(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}
