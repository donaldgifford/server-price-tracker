package extract_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
)

func TestOpenAICompatBackend_Name(t *testing.T) {
	t.Parallel()
	b := extract.NewOpenAICompatBackend("http://localhost:8000", "mistral")
	assert.Equal(t, "openai_compat", b.Name())
}

func TestOpenAICompatBackend_Generate(t *testing.T) {
	t.Parallel()

	successResponse := `{
		"choices": [{"message": {"role": "assistant", "content": "ram"}}],
		"model": "mistral",
		"usage": {"prompt_tokens": 10, "completion_tokens": 1, "total_tokens": 11}
	}`

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		req        extract.GenerateRequest
		apiKey     string
		wantErr    bool
		wantErrMsg string
		wantResp   string
		wantUsage  int
	}{
		{
			name: "successful generation",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Contains(t, r.URL.Path, "/v1/chat/completions")
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
			name: "system message prepended",
			handler: func(w http.ResponseWriter, r *http.Request) {
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				msgs := req["messages"].([]any)
				assert.Len(t, msgs, 2)
				first := msgs[0].(map[string]any)
				assert.Equal(t, "system", first["role"])
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(successResponse))
			},
			req: extract.GenerateRequest{
				Prompt:    "extract",
				SystemMsg: "You are a helpful assistant",
			},
			wantResp: "ram",
		},
		{
			name: "json format sets response_format",
			handler: func(w http.ResponseWriter, r *http.Request) {
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				rf := req["response_format"].(map[string]any)
				assert.Equal(t, "json_object", rf["type"])
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(successResponse))
			},
			req: extract.GenerateRequest{
				Prompt: "extract",
				Format: "json",
			},
			wantResp: "ram",
		},
		{
			name:   "auth header sent when key provided",
			apiKey: "sk-test-key",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "Bearer sk-test-key", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(successResponse))
			},
			req:      extract.GenerateRequest{Prompt: "test"},
			wantResp: "ram",
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"internal"}`))
			},
			req:        extract.GenerateRequest{Prompt: "test"},
			wantErr:    true,
			wantErrMsg: "openai-compatible API error (status 500)",
		},
		{
			name: "empty choices",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"choices":[],"model":"test","usage":{}}`))
			},
			req:        extract.GenerateRequest{Prompt: "test"},
			wantErr:    true,
			wantErrMsg: "empty choices",
		},
		{
			name: "invalid JSON response",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`not json`))
			},
			req:        extract.GenerateRequest{Prompt: "test"},
			wantErr:    true,
			wantErrMsg: "parsing response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			opts := []extract.OpenAICompatOption{
				extract.WithOpenAICompatHTTPClient(srv.Client()),
			}
			if tt.apiKey != "" {
				opts = append(opts, extract.WithOpenAICompatAPIKey(tt.apiKey))
			}

			backend := extract.NewOpenAICompatBackend(srv.URL, "mistral", opts...)
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
