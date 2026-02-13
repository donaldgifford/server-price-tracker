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

// OpenAICompatBackend implements LLMBackend using the OpenAI chat completions API.
// Compatible with vLLM, text-generation-inference, LM Studio, etc.
type OpenAICompatBackend struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
}

// OpenAICompatOption configures the OpenAICompatBackend.
type OpenAICompatOption func(*OpenAICompatBackend)

// WithOpenAICompatHTTPClient overrides the default HTTP client.
func WithOpenAICompatHTTPClient(c *http.Client) OpenAICompatOption {
	return func(b *OpenAICompatBackend) {
		b.client = c
	}
}

// WithOpenAICompatAPIKey sets the API key.
func WithOpenAICompatAPIKey(key string) OpenAICompatOption {
	return func(b *OpenAICompatBackend) {
		b.apiKey = key
	}
}

// NewOpenAICompatBackend creates a new OpenAI-compatible backend.
func NewOpenAICompatBackend(
	endpoint, model string,
	opts ...OpenAICompatOption,
) *OpenAICompatBackend {
	b := &OpenAICompatBackend{
		endpoint: endpoint,
		model:    model,
		apiKey:   os.Getenv("OPENAI_API_KEY"),
		client:   &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Name returns the backend name.
func (*OpenAICompatBackend) Name() string {
	return "openai_compat"
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	ResponseFmt *openAIRespFmt  `json:"response_format,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRespFmt struct {
	Type string `json:"type"`
}

type openAIChatResponse struct {
	Choices []openAIChoice `json:"choices"`
	Model   string         `json:"model"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Generate calls the OpenAI-compatible /v1/chat/completions endpoint.
func (b *OpenAICompatBackend) Generate(
	ctx context.Context,
	req GenerateRequest,
) (GenerateResponse, error) {
	messages := []openAIMessage{
		{Role: "user", Content: req.Prompt},
	}

	if req.SystemMsg != "" {
		messages = append(
			[]openAIMessage{{Role: "system", Content: req.SystemMsg}},
			messages...,
		)
	}

	chatReq := openAIChatRequest{
		Model:     b.model,
		Messages:  messages,
		MaxTokens: req.MaxTokens,
	}

	if req.Temperature > 0 {
		chatReq.Temperature = &req.Temperature
	}

	if req.Format == FormatJSON {
		chatReq.ResponseFmt = &openAIRespFmt{Type: "json_object"}
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("marshaling request: %w", err)
	}

	url := b.endpoint + "/v1/chat/completions"

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
	if b.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
	}

	resp, err := b.client.Do(httpReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("calling openai-compatible API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return GenerateResponse{}, fmt.Errorf(
			"openai-compatible API error (status %d): %s",
			resp.StatusCode,
			string(respBody),
		)
	}

	var chatResp openAIChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return GenerateResponse{}, fmt.Errorf("parsing response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return GenerateResponse{}, fmt.Errorf("empty choices from openai-compatible API")
	}

	return GenerateResponse{
		Content: chatResp.Choices[0].Message.Content,
		Model:   chatResp.Model,
		Usage: TokenUsage{
			PromptTokens:     chatResp.Usage.PromptTokens,
			CompletionTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:      chatResp.Usage.TotalTokens,
		},
	}, nil
}
