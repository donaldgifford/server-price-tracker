//go:build integration

package extract_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
)

// TestOllamaBackend_Integration requires a running Ollama instance.
// Run with: go test -tags=integration -run TestOllamaBackend_Integration ./pkg/extract/...
//
// Required environment variables:
//   - OLLAMA_ENDPOINT: Ollama endpoint (default: http://localhost:11434)
//   - OLLAMA_MODEL: Model to use (default: mistral)
func TestOllamaBackend_Integration(t *testing.T) {
	endpoint := os.Getenv("OLLAMA_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}

	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "mistral"
	}

	backend := extract.NewOllamaBackend(endpoint, model)

	resp, err := backend.Generate(context.Background(), extract.GenerateRequest{
		Prompt:      "Classify this eBay listing into exactly one type: ram, drive, server, cpu, nic, other. Title: Samsung 32GB DDR4 ECC REG. Respond with only the type.",
		Temperature: 0.1,
		MaxTokens:   10,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Content)
	assert.NotEmpty(t, resp.Model)

	// Real Ollama responses include prompt_eval_count and eval_count; verify
	// our parser surfaces them as TokenUsage so /metrics emits non-zero values.
	assert.Positive(t, resp.Usage.PromptTokens, "real Ollama response must populate PromptTokens")
	assert.Positive(t, resp.Usage.CompletionTokens, "real Ollama response must populate CompletionTokens")
	assert.Equal(t, resp.Usage.PromptTokens+resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}
