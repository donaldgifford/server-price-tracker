package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	defaultAnthropicURL     = "https://api.anthropic.com/v1/messages"
	defaultAnthropicModel   = "claude-haiku-4-20250514"
	defaultAnthropicVersion = "2023-06-01"
)

// AnthropicBackend implements LLMBackend using the Anthropic Messages API.
type AnthropicBackend struct {
	apiKey     string
	model      string
	endpoint   string
	apiVersion string
	client     *http.Client
}

// AnthropicOption configures the AnthropicBackend.
type AnthropicOption func(*AnthropicBackend)

// WithAnthropicEndpoint overrides the default API endpoint.
func WithAnthropicEndpoint(url string) AnthropicOption {
	return func(b *AnthropicBackend) {
		b.endpoint = url
	}
}

// WithAnthropicModel overrides the default model.
func WithAnthropicModel(model string) AnthropicOption {
	return func(b *AnthropicBackend) {
		b.model = model
	}
}

// WithAnthropicAPIKey overrides the API key (instead of reading from env).
func WithAnthropicAPIKey(key string) AnthropicOption {
	return func(b *AnthropicBackend) {
		b.apiKey = key
	}
}

// WithAnthropicHTTPClient overrides the default HTTP client.
func WithAnthropicHTTPClient(c *http.Client) AnthropicOption {
	return func(b *AnthropicBackend) {
		b.client = c
	}
}

// NewAnthropicBackend creates a new Anthropic Claude API backend.
// The API key is read from the ANTHROPIC_API_KEY environment variable
// if not provided via options.
func NewAnthropicBackend(opts ...AnthropicOption) *AnthropicBackend {
	b := &AnthropicBackend{
		apiKey:     os.Getenv("ANTHROPIC_API_KEY"),
		model:      defaultAnthropicModel,
		endpoint:   defaultAnthropicURL,
		apiVersion: defaultAnthropicVersion,
		client:     &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Name returns the backend name.
func (*AnthropicBackend) Name() string {
	return "anthropic"
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature *float64           `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []anthropicContent `json:"content"`
	Model   string             `json:"model"`
	Usage   anthropicUsage     `json:"usage"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Generate calls the Anthropic Messages API.
func (b *AnthropicBackend) Generate(
	ctx context.Context,
	req GenerateRequest,
) (GenerateResponse, error) {
	if b.apiKey == "" {
		return GenerateResponse{}, fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 512
	}

	anthropicReq := anthropicRequest{
		Model:     b.model,
		MaxTokens: maxTokens,
		System:    req.SystemMsg,
		Messages: []anthropicMessage{
			{Role: "user", Content: req.Prompt},
		},
	}

	if req.Temperature > 0 {
		anthropicReq.Temperature = &req.Temperature
	}

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		b.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("creating HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", b.apiKey)
	httpReq.Header.Set("anthropic-version", b.apiVersion)

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("calling anthropic API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr anthropicError
		if jsonErr := json.Unmarshal(respBody, &apiErr); jsonErr == nil &&
			apiErr.Error.Message != "" {
			return GenerateResponse{}, fmt.Errorf(
				"anthropic API error (status %d): %s: %s",
				resp.StatusCode,
				apiErr.Error.Type,
				apiErr.Error.Message,
			)
		}
		return GenerateResponse{}, fmt.Errorf(
			"anthropic API error (status %d): %s",
			resp.StatusCode,
			string(respBody),
		)
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return GenerateResponse{}, fmt.Errorf("parsing anthropic response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return GenerateResponse{}, fmt.Errorf("empty response from anthropic")
	}

	return GenerateResponse{
		Content: apiResp.Content[0].Text,
		Model:   apiResp.Model,
		Usage: TokenUsage{
			PromptTokens:     apiResp.Usage.InputTokens,
			CompletionTokens: apiResp.Usage.OutputTokens,
			TotalTokens:      apiResp.Usage.InputTokens + apiResp.Usage.OutputTokens,
		},
	}, nil
}
