package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// extractorTracerName is the OTel tracer registered for span emission
// from the extract pipeline. Returns the global no-op tracer when OTel
// is disabled — calling tracer.Start on it produces a span that records
// nothing, so the wrapper code stays branch-free.
const extractorTracerName = "github.com/donaldgifford/server-price-tracker/pkg/extract"

// Direction label values for ExtractionTokensTotal.
const (
	directionInput  = "input"
	directionOutput = "output"
)

// LLMExtractor implements the Extractor interface using an LLM backend.
type LLMExtractor struct {
	backend     LLMBackend
	backendName string // cached at construction; backend identity is stable
	log         *slog.Logger
	tracer      trace.Tracer    // no-op when OTel is disabled
	langfuse    langfuse.Client // NoopClient when Langfuse is disabled
	temperature float64
	maxTokens   int
}

// LLMExtractorOption configures the LLMExtractor.
type LLMExtractorOption func(*LLMExtractor)

// WithTemperature sets the LLM temperature for extraction.
func WithTemperature(t float64) LLMExtractorOption {
	return func(e *LLMExtractor) {
		e.temperature = t
	}
}

// WithMaxTokens sets the max tokens for LLM responses.
func WithMaxTokens(n int) LLMExtractorOption {
	return func(e *LLMExtractor) {
		e.maxTokens = n
	}
}

// WithLogger sets a custom logger for extraction diagnostics.
func WithLogger(l *slog.Logger) LLMExtractorOption {
	return func(e *LLMExtractor) {
		e.log = l
	}
}

// WithTracer overrides the OTel tracer used by extraction spans.
// Production callers don't need this — NewLLMExtractor pulls
// otel.Tracer(extractorTracerName) which returns a no-op tracer when
// OTel is disabled and the real one once observability.Init has run.
// Tests use this hook to point the extractor at a tracetest-recorded
// TracerProvider (DESIGN-0016 / IMPL-0019 Phase 2).
func WithTracer(t trace.Tracer) LLMExtractorOption {
	return func(e *LLMExtractor) {
		e.tracer = t
	}
}

// WithLangfuseClient supplies the Langfuse client used to post per-
// extraction scores (e.g., extraction_self_confidence). NoopClient is
// the default — passing the real client lights up the score writes.
func WithLangfuseClient(c langfuse.Client) LLMExtractorOption {
	return func(e *LLMExtractor) {
		if c == nil {
			c = langfuse.NoopClient{}
		}
		e.langfuse = c
	}
}

// NewLLMExtractor creates a new LLMExtractor.
func NewLLMExtractor(backend LLMBackend, opts ...LLMExtractorOption) *LLMExtractor {
	e := &LLMExtractor{
		backend:     backend,
		backendName: backend.Name(),
		log:         slog.Default(),
		tracer:      otel.Tracer(extractorTracerName),
		langfuse:    langfuse.NoopClient{},
		temperature: 0.1,
		maxTokens:   512,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// stripJSONFences removes ```json or ``` markdown code fences that some LLM
// backends (notably Anthropic) wrap JSON responses in despite explicit
// "no markdown" instructions in the prompt. Returns the inner content
// trimmed of surrounding whitespace; passes bare JSON through unchanged.
func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// recordTokens emits LLM token telemetry for a successful Generate response.
// Called before JSON parse / validation so the metric reflects billed tokens,
// not just tokens that produced useful output.
func (e *LLMExtractor) recordTokens(resp GenerateResponse) {
	metrics.ExtractionTokensTotal.
		WithLabelValues(e.backendName, resp.Model, directionInput).
		Add(float64(resp.Usage.PromptTokens))
	metrics.ExtractionTokensTotal.
		WithLabelValues(e.backendName, resp.Model, directionOutput).
		Add(float64(resp.Usage.CompletionTokens))
	metrics.ExtractionTokensPerRequest.
		WithLabelValues(e.backendName, resp.Model).
		Observe(float64(resp.Usage.TotalTokens))
}

var validComponentTypes = map[string]domain.ComponentType{
	"ram":         domain.ComponentRAM,
	"drive":       domain.ComponentDrive,
	"server":      domain.ComponentServer,
	"cpu":         domain.ComponentCPU,
	"nic":         domain.ComponentNIC,
	"gpu":         domain.ComponentGPU,
	"workstation": domain.ComponentWorkstation,
	"desktop":     domain.ComponentDesktop,
	"other":       domain.ComponentOther,
}

// Classify determines the component type from a listing title.
func (e *LLMExtractor) Classify(
	ctx context.Context,
	title string,
) (domain.ComponentType, error) {
	ctx, span := e.tracer.Start(ctx, "extract.classify",
		trace.WithAttributes(
			attribute.String("spt.backend", e.backendName),
		),
	)
	defer span.End()

	prompt, err := RenderClassifyPrompt(title)
	if err != nil {
		return "", recordSpanError(span, fmt.Errorf("rendering classify prompt: %w", err))
	}

	resp, err := e.backend.Generate(ctx, GenerateRequest{
		Prompt:      prompt,
		Temperature: e.temperature,
		MaxTokens:   50,
	})
	if err != nil {
		return "", recordSpanError(span, fmt.Errorf("calling LLM for classification: %w", err))
	}
	e.recordTokens(resp)
	span.SetAttributes(
		attribute.String("spt.llm.model", resp.Model),
		attribute.Int("spt.llm.tokens.input", resp.Usage.PromptTokens),
		attribute.Int("spt.llm.tokens.output", resp.Usage.CompletionTokens),
	)

	raw := strings.TrimSpace(strings.ToLower(resp.Content))
	e.log.Debug("classify LLM response", "title", title, "raw_response", resp.Content, "parsed", raw)

	ct, ok := validComponentTypes[raw]
	if !ok {
		e.log.Warn("classify returned invalid component type", "title", title, "raw_response", resp.Content, "parsed", raw)
		return "", recordSpanError(span, fmt.Errorf("invalid component type %q from LLM", raw))
	}

	span.SetAttributes(attribute.String("spt.component.type", string(ct)))
	return ct, nil
}

// Extract extracts structured attributes from a listing title using the LLM.
func (e *LLMExtractor) Extract(
	ctx context.Context,
	componentType domain.ComponentType,
	title string,
	itemSpecifics map[string]string,
) (map[string]any, error) {
	ctx, span := e.tracer.Start(ctx, "extract.extract",
		trace.WithAttributes(
			attribute.String("spt.backend", e.backendName),
			attribute.String("spt.component.type", string(componentType)),
		),
	)
	defer span.End()

	prompt, err := RenderExtractPrompt(componentType, title, itemSpecifics)
	if err != nil {
		return nil, recordSpanError(span, fmt.Errorf("rendering extract prompt: %w", err))
	}

	resp, err := e.backend.Generate(ctx, GenerateRequest{
		Prompt:      prompt,
		Format:      FormatJSON,
		Temperature: e.temperature,
		MaxTokens:   e.maxTokens,
	})
	if err != nil {
		return nil, recordSpanError(span, fmt.Errorf("calling LLM for extraction: %w", err))
	}
	e.recordTokens(resp)
	span.SetAttributes(
		attribute.String("spt.llm.model", resp.Model),
		attribute.Int("spt.llm.tokens.input", resp.Usage.PromptTokens),
		attribute.Int("spt.llm.tokens.output", resp.Usage.CompletionTokens),
	)

	e.log.Debug("extract LLM response", "component_type", componentType, "title", title, "raw_response", resp.Content)

	content := stripJSONFences(resp.Content)

	attrs, err := parseExtractAttrs(ctx, e.tracer, content)
	if err != nil {
		e.log.Warn("extract JSON parse failed",
			"component_type", componentType, "title", title, "raw_response", resp.Content, "error", err)
		return nil, recordSpanError(span, err)
	}

	normalizeWithSpan(ctx, e.tracer, componentType, title, attrs)

	if err := validateWithSpan(ctx, e.tracer, componentType, attrs); err != nil {
		e.log.Warn("extract validation failed",
			"component_type", componentType, "title", title, "raw_response", resp.Content, "error", err)
		return nil, recordSpanError(span, err)
	}

	if conf, ok := attrs["confidence"].(float64); ok {
		span.SetAttributes(attribute.Float64("spt.extraction.confidence", conf))
	}
	e.autoScoreConfidence(ctx, attrs)
	return attrs, nil
}

// autoScoreConfidence posts the extraction_self_confidence score to
// Langfuse on the active extract trace. Three preconditions: an OTel
// trace ID must exist (no anchor otherwise), the Langfuse client must
// not be the no-op (avoid pointless work), and attrs must carry a
// numeric confidence value (LLM didn't return one → nothing to score).
//
// Score writes are best-effort — the buffered client absorbs transient
// outages, and any returned error is logged at debug only. Telemetry
// failures never propagate to the extraction caller.
func (e *LLMExtractor) autoScoreConfidence(ctx context.Context, attrs map[string]any) {
	conf, ok := attrs["confidence"].(float64)
	if !ok {
		return
	}
	traceID := traceIDFromContext(ctx)
	if traceID == "" {
		return
	}
	if err := e.langfuse.Score(ctx, traceID, "extraction_self_confidence", conf, ""); err != nil {
		e.log.Debug("langfuse Score failed (dropped)", "error", err)
	}
}

// parseExtractAttrs is the JSON parse step lifted into its own span so the
// trace shows the boundary between LLM response and parsed map. Errors
// are wrapped to preserve the JSON-parse-failed signal upstream.
func parseExtractAttrs(ctx context.Context, tracer trace.Tracer, content string) (map[string]any, error) {
	_, span := tracer.Start(ctx, "extract.parse_json")
	defer span.End()

	var attrs map[string]any
	if err := json.Unmarshal([]byte(content), &attrs); err != nil {
		return nil, recordSpanError(span, fmt.Errorf("parsing LLM JSON response: %w", err))
	}
	return attrs, nil
}

// normalizeWithSpan wraps NormalizeExtraction in a span so the trace
// distinguishes normalisation effort (including PC4 recovery, capacity
// fixups) from raw LLM output.
func normalizeWithSpan(
	ctx context.Context,
	tracer trace.Tracer,
	componentType domain.ComponentType,
	title string,
	attrs map[string]any,
) {
	_, span := tracer.Start(ctx, "extract.normalize",
		trace.WithAttributes(attribute.String("spt.component.type", string(componentType))),
	)
	defer span.End()
	NormalizeExtraction(componentType, title, attrs)
}

// validateWithSpan wraps ValidateExtraction; non-nil errors are recorded
// on the span so Clickhouse queries can filter validation failures.
func validateWithSpan(
	ctx context.Context,
	tracer trace.Tracer,
	componentType domain.ComponentType,
	attrs map[string]any,
) error {
	_, span := tracer.Start(ctx, "extract.validate",
		trace.WithAttributes(attribute.String("spt.component.type", string(componentType))),
	)
	defer span.End()
	if err := ValidateExtraction(componentType, attrs); err != nil {
		return recordSpanError(span, fmt.Errorf("validating extraction: %w", err))
	}
	return nil
}

// recordSpanError marks the span as errored and returns the error
// unchanged so callers can chain `return recordSpanError(span, err)`.
func recordSpanError(span trace.Span, err error) error {
	if err == nil {
		return nil
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// accessoryShortCircuitConfidence is the extraction_confidence assigned to
// listings routed to ComponentOther by the regex pre-classifier. Held below
// 1.0 because a regex match is qualitatively different from a confident LLM
// answer; downstream consumers can still distinguish regex hits from genuine
// LLM-confident extractions if they care.
const accessoryShortCircuitConfidence = 0.95

// ClassifyAndExtract classifies the title and then extracts attributes.
//
// Pre-class short-circuits run before the LLM, in priority order:
//  1. DetectSystemTypeFromTitle — chassis token (ThinkStation, EliteDesk,
//     HP Z\d, Precision T-pattern, Pro Max) plus a system-completeness
//     signal (RAM/storage/CPU model/cores) routes to workstation/desktop.
//     Runs first so it overrides both the accessory short-circuit and
//     the LLM. Catches the "EliteDesk + Power Cable" + "ThinkStation
//     P920 ... Gold 6148" failure modes from dev validation.
//  2. IsAccessoryOnly — bare server-part accessories (backplanes,
//     caddies, rails, etc.) route to ComponentOther without the LLM.
//     See DESIGN-0011.
//  3. DetectSystemTypeFromSpecifics — item specifics (`Most Suitable For`,
//     `Series`, `Product Line`) route to workstation/desktop without
//     the LLM classifier. See DESIGN-0015 Open Question 1.
//  4. LLM Classify → Extract.
func (e *LLMExtractor) ClassifyAndExtract(
	ctx context.Context,
	title string,
	itemSpecifics map[string]string,
) (domain.ComponentType, map[string]any, error) {
	ctx, span := e.tracer.Start(ctx, "extract.classify_and_extract")
	defer span.End()

	start := time.Now()
	defer func() {
		recordExtractionDuration(ctx, time.Since(start).Seconds())
	}()

	if ct := preclassifyTitle(ctx, e.tracer, title); ct != "" {
		e.log.Info("system pre-class short-circuit (title)",
			"title", title, "component_type", ct, "system_title_short_circuit", true)
		span.SetAttributes(
			attribute.String("spt.preclass", "title"),
			attribute.String("spt.component.type", string(ct)),
		)
		attrs, err := e.Extract(ctx, ct, title, itemSpecifics)
		if err != nil {
			return ct, nil, recordSpanError(span, fmt.Errorf("extracting after title pre-class: %w", err))
		}
		return ct, attrs, nil
	}

	if preclassifyAccessory(ctx, e.tracer, title) {
		e.log.Info("accessory short-circuit",
			"title", title, "accessory_short_circuit", true)
		span.SetAttributes(
			attribute.String("spt.preclass", "accessory"),
			attribute.String("spt.component.type", string(domain.ComponentOther)),
		)
		return domain.ComponentOther, map[string]any{
			"confidence": accessoryShortCircuitConfidence,
		}, nil
	}

	if ct := preclassifySpecifics(ctx, e.tracer, itemSpecifics); ct != "" {
		e.log.Info("system pre-class short-circuit (specifics)",
			"title", title, "component_type", ct, "system_specs_short_circuit", true)
		span.SetAttributes(
			attribute.String("spt.preclass", "specifics"),
			attribute.String("spt.component.type", string(ct)),
		)
		attrs, err := e.Extract(ctx, ct, title, itemSpecifics)
		if err != nil {
			return ct, nil, recordSpanError(span, fmt.Errorf("extracting after system pre-class: %w", err))
		}
		return ct, attrs, nil
	}

	span.SetAttributes(attribute.String("spt.preclass", "llm"))
	ct, err := e.Classify(ctx, title)
	if err != nil {
		return "", nil, recordSpanError(span, fmt.Errorf("classifying: %w", err))
	}

	attrs, err := e.Extract(ctx, ct, title, itemSpecifics)
	if err != nil {
		return ct, nil, recordSpanError(span, fmt.Errorf("extracting: %w", err))
	}

	span.SetAttributes(attribute.String("spt.component.type", string(ct)))
	e.log.Debug("classify and extract complete",
		"title", title, "component_type", ct, "attribute_count", len(attrs))

	return ct, attrs, nil
}

// preclassifyTitle wraps DetectSystemTypeFromTitle in a span so the
// trace records whether the title pre-class hit (and which type).
func preclassifyTitle(ctx context.Context, tracer trace.Tracer, title string) domain.ComponentType {
	_, span := tracer.Start(ctx, "extract.preclassify_title")
	defer span.End()
	ct := DetectSystemTypeFromTitle(title)
	span.SetAttributes(attribute.Bool("spt.preclass.matched", ct != ""))
	return ct
}

// preclassifyAccessory wraps IsAccessoryOnly in a span so the trace
// records whether the accessory short-circuit fired.
func preclassifyAccessory(ctx context.Context, tracer trace.Tracer, title string) bool {
	_, span := tracer.Start(ctx, "extract.preclassify_accessory")
	defer span.End()
	matched := IsAccessoryOnly(title)
	span.SetAttributes(attribute.Bool("spt.preclass.matched", matched))
	return matched
}

// preclassifySpecifics wraps DetectSystemTypeFromSpecifics in a span.
func preclassifySpecifics(
	ctx context.Context,
	tracer trace.Tracer,
	specifics map[string]string,
) domain.ComponentType {
	_, span := tracer.Start(ctx, "extract.preclassify_specifics")
	defer span.End()
	ct := DetectSystemTypeFromSpecifics(specifics)
	span.SetAttributes(attribute.Bool("spt.preclass.matched", ct != ""))
	return ct
}
