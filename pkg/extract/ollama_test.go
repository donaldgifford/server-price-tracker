package extract_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
)

func TestOllamaBackend_Name(t *testing.T) {
	t.Parallel()
	b := extract.NewOllamaBackend("http://localhost:11434", "mistral")
	assert.Equal(t, "ollama", b.Name())
}

func TestOllamaBackend_Generate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		req        extract.GenerateRequest
		wantErr    bool
		wantErrMsg string
		wantResp   string
	}{
		{
			name: "successful generation",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"model":"mistral","response":"ram"}`))
			},
			req: extract.GenerateRequest{
				Prompt:      "classify this",
				Temperature: 0.1,
				MaxTokens:   50,
			},
			wantResp: "ram",
		},
		{
			name: "json format passed through",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"model":"mistral","response":"{\"key\":\"val\"}"}`))
			},
			req: extract.GenerateRequest{
				Prompt:      "extract",
				Format:      "json",
				Temperature: 0.1,
				MaxTokens:   512,
			},
			wantResp: `{"key":"val"}`,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`internal error`))
			},
			req:        extract.GenerateRequest{Prompt: "test"},
			wantErr:    true,
			wantErrMsg: "ollama error (status 500)",
		},
		{
			name: "invalid JSON response",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`not json`))
			},
			req:        extract.GenerateRequest{Prompt: "test"},
			wantErr:    true,
			wantErrMsg: "parsing ollama",
		},
		{
			name: "timeout",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				time.Sleep(200 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			},
			req:        extract.GenerateRequest{Prompt: "test"},
			wantErr:    true,
			wantErrMsg: "calling ollama",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			clientTimeout := 5 * time.Second
			if tt.name == "timeout" {
				clientTimeout = 50 * time.Millisecond
			}

			backend := extract.NewOllamaBackend(
				srv.URL,
				"mistral",
				extract.WithOllamaHTTPClient(&http.Client{Timeout: clientTimeout}),
			)

			resp, err := backend.Generate(context.Background(), tt.req)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantResp, resp.Content)
			assert.Equal(t, "mistral", resp.Model)
		})
	}
}
