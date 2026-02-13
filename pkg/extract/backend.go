// Package extract provides LLM-based attribute extraction for eBay listings,
// abstracted behind interfaces for testability.
package extract

import (
	"context"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// FormatJSON is the format string for requesting JSON mode from LLM backends.
const FormatJSON = "json"

// GenerateRequest defines the input for an LLM generation call.
type GenerateRequest struct {
	Prompt      string
	SystemMsg   string
	Format      string // FormatJSON for JSON mode
	Temperature float64
	MaxTokens   int
}

// TokenUsage tracks LLM token consumption.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// GenerateResponse holds the result of an LLM generation call.
type GenerateResponse struct {
	Content string
	Model   string
	Usage   TokenUsage
}

// LLMBackend defines the interface for LLM text generation.
type LLMBackend interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
	Name() string
}

// Extractor defines the interface for classifying and extracting
// structured attributes from eBay listing titles.
type Extractor interface {
	Classify(ctx context.Context, title string) (domain.ComponentType, error)
	Extract(
		ctx context.Context,
		componentType domain.ComponentType,
		title string,
		itemSpecifics map[string]string,
	) (map[string]any, error)
	ClassifyAndExtract(
		ctx context.Context,
		title string,
		itemSpecifics map[string]string,
	) (domain.ComponentType, map[string]any, error)
}
