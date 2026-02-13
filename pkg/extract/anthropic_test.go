package extract_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
)

func TestAnthropicBackend_Name(t *testing.T) {
	t.Parallel()
	b := extract.NewAnthropicBackend()
	assert.Equal(t, "anthropic", b.Name())
}

func TestAnthropicBackend_Generate(t *testing.T) {
	t.Parallel()

	successResponse := `{
		"content": [{"type": "text", "text": "ram"}],
		"model": "claude-haiku-4-20250514",
		"usage": {"input_tokens": 10, "output_tokens": 1}
	}`

	tests := []struct {
		name       string
		apiKey     string
		handler    http.HandlerFunc
		req        extract.GenerateRequest
		wantErr    bool
		wantErrMsg string
		wantResp   string
		wantUsage  int
	}{
		{
			name:   "successful generation",
			apiKey: "test-key",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
				assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(successResponse))
			},
			req: extract.GenerateRequest{
				Prompt:      "classify this",
				Temperature: 0.1,
				MaxTokens:   50,
			},
			wantResp:  "ram",
			wantUsage: 11,
		},
		{
			name:       "missing API key",
			apiKey:     "",
			handler:    func(_ http.ResponseWriter, _ *http.Request) {},
			req:        extract.GenerateRequest{Prompt: "test"},
			wantErr:    true,
			wantErrMsg: "ANTHROPIC_API_KEY",
		},
		{
			name:   "rate limited 429",
			apiKey: "test-key",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{
					"error": {"type": "rate_limit_error", "message": "rate limit exceeded"}
				}`))
			},
			req:        extract.GenerateRequest{Prompt: "test"},
			wantErr:    true,
			wantErrMsg: "rate_limit_error",
		},
		{
			name:   "server error 500",
			apiKey: "test-key",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{
					"error": {"type": "api_error", "message": "internal server error"}
				}`))
			},
			req:        extract.GenerateRequest{Prompt: "test"},
			wantErr:    true,
			wantErrMsg: "api_error",
		},
		{
			name:   "invalid JSON response",
			apiKey: "test-key",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`not json`))
			},
			req:        extract.GenerateRequest{Prompt: "test"},
			wantErr:    true,
			wantErrMsg: "parsing anthropic",
		},
		{
			name:   "empty content array",
			apiKey: "test-key",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"content":[],"model":"test","usage":{}}`))
			},
			req:        extract.GenerateRequest{Prompt: "test"},
			wantErr:    true,
			wantErrMsg: "empty response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			backend := extract.NewAnthropicBackend(
				extract.WithAnthropicEndpoint(srv.URL),
				extract.WithAnthropicHTTPClient(srv.Client()),
				extract.WithAnthropicAPIKey(tt.apiKey),
			)

			resp, err := backend.Generate(context.Background(), tt.req)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantResp, resp.Content)
			if tt.wantUsage > 0 {
				assert.Equal(t, tt.wantUsage, resp.Usage.TotalTokens)
			}
		})
	}
}
