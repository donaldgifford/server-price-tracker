package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

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

// NewLLMExtractor creates a new LLMExtractor.
func NewLLMExtractor(backend LLMBackend, opts ...LLMExtractorOption) *LLMExtractor {
	e := &LLMExtractor{
		backend:     backend,
		backendName: backend.Name(),
		log:         slog.Default(),
		temperature: 0.1,
		maxTokens:   512,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
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
	"ram":    domain.ComponentRAM,
	"drive":  domain.ComponentDrive,
	"server": domain.ComponentServer,
	"cpu":    domain.ComponentCPU,
	"nic":    domain.ComponentNIC,
	"other":  domain.ComponentOther,
}

// Classify determines the component type from a listing title.
func (e *LLMExtractor) Classify(
	ctx context.Context,
	title string,
) (domain.ComponentType, error) {
	prompt, err := RenderClassifyPrompt(title)
	if err != nil {
		return "", fmt.Errorf("rendering classify prompt: %w", err)
	}

	resp, err := e.backend.Generate(ctx, GenerateRequest{
		Prompt:      prompt,
		Temperature: e.temperature,
		MaxTokens:   50,
	})
	if err != nil {
		return "", fmt.Errorf("calling LLM for classification: %w", err)
	}
	e.recordTokens(resp)

	raw := strings.TrimSpace(strings.ToLower(resp.Content))
	e.log.Debug("classify LLM response", "title", title, "raw_response", resp.Content, "parsed", raw)

	ct, ok := validComponentTypes[raw]
	if !ok {
		e.log.Warn("classify returned invalid component type", "title", title, "raw_response", resp.Content, "parsed", raw)
		return "", fmt.Errorf("invalid component type %q from LLM", raw)
	}

	return ct, nil
}

// Extract extracts structured attributes from a listing title using the LLM.
func (e *LLMExtractor) Extract(
	ctx context.Context,
	componentType domain.ComponentType,
	title string,
	itemSpecifics map[string]string,
) (map[string]any, error) {
	prompt, err := RenderExtractPrompt(componentType, title, itemSpecifics)
	if err != nil {
		return nil, fmt.Errorf("rendering extract prompt: %w", err)
	}

	resp, err := e.backend.Generate(ctx, GenerateRequest{
		Prompt:      prompt,
		Format:      FormatJSON,
		Temperature: e.temperature,
		MaxTokens:   e.maxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("calling LLM for extraction: %w", err)
	}
	e.recordTokens(resp)

	e.log.Debug("extract LLM response", "component_type", componentType, "title", title, "raw_response", resp.Content)

	var attrs map[string]any
	if err := json.Unmarshal([]byte(resp.Content), &attrs); err != nil {
		e.log.Warn("extract JSON parse failed",
			"component_type", componentType, "title", title, "raw_response", resp.Content, "error", err)
		return nil, fmt.Errorf("parsing LLM JSON response: %w", err)
	}

	NormalizeExtraction(componentType, title, attrs)

	if err := ValidateExtraction(componentType, attrs); err != nil {
		e.log.Warn("extract validation failed",
			"component_type", componentType, "title", title, "raw_response", resp.Content, "error", err)
		return nil, fmt.Errorf("validating extraction: %w", err)
	}

	return attrs, nil
}

// ClassifyAndExtract classifies the title and then extracts attributes.
func (e *LLMExtractor) ClassifyAndExtract(
	ctx context.Context,
	title string,
	itemSpecifics map[string]string,
) (domain.ComponentType, map[string]any, error) {
	ct, err := e.Classify(ctx, title)
	if err != nil {
		return "", nil, fmt.Errorf("classifying: %w", err)
	}

	attrs, err := e.Extract(ctx, ct, title, itemSpecifics)
	if err != nil {
		return ct, nil, fmt.Errorf("extracting: %w", err)
	}

	e.log.Debug("classify and extract complete",
		"title", title, "component_type", ct, "attribute_count", len(attrs))

	return ct, attrs, nil
}
