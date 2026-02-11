package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaBackend implements LLMBackend using the Ollama /api/generate endpoint.
type OllamaBackend struct {
	endpoint string
	model    string
	client   *http.Client
}

// OllamaOption configures the OllamaBackend.
type OllamaOption func(*OllamaBackend)

// WithOllamaHTTPClient overrides the default HTTP client.
func WithOllamaHTTPClient(c *http.Client) OllamaOption {
	return func(b *OllamaBackend) {
		b.client = c
	}
}

// NewOllamaBackend creates a new Ollama LLM backend.
func NewOllamaBackend(endpoint, model string, opts ...OllamaOption) *OllamaBackend {
	b := &OllamaBackend{
		endpoint: endpoint,
		model:    model,
		client:   &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Name returns the backend name.
func (*OllamaBackend) Name() string {
	return "ollama"
}

type ollamaRequest struct {
	Model      string         `json:"model"`
	Prompt     string         `json:"prompt"`
	System     string         `json:"system,omitempty"`
	Format     string         `json:"format,omitempty"`
	Stream     bool           `json:"stream"`
	Options    *ollamaOptions `json:"options,omitempty"`
	NumPredict int            `json:"num_predict,omitempty"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature"`
}

type ollamaResponse struct {
	Model    string `json:"model"`
	Response string `json:"response"`
}

// Generate calls the Ollama /api/generate endpoint.
func (b *OllamaBackend) Generate(
	ctx context.Context,
	req GenerateRequest,
) (GenerateResponse, error) {
	ollamaReq := ollamaRequest{
		Model:      b.model,
		Prompt:     req.Prompt,
		System:     req.SystemMsg,
		Stream:     false,
		NumPredict: req.MaxTokens,
	}

	if req.Format == FormatJSON {
		ollamaReq.Format = FormatJSON
	}

	if req.Temperature > 0 {
		ollamaReq.Options = &ollamaOptions{Temperature: req.Temperature}
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("marshaling request: %w", err)
	}

	url := b.endpoint + "/api/generate"

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url,
		bytes.NewReader(body),
	)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("creating HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("calling ollama: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return GenerateResponse{}, fmt.Errorf(
			"ollama error (status %d): %s",
			resp.StatusCode,
			string(respBody),
		)
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return GenerateResponse{}, fmt.Errorf("parsing ollama response: %w", err)
	}

	return GenerateResponse{
		Content: ollamaResp.Response,
		Model:   ollamaResp.Model,
	}, nil
}
