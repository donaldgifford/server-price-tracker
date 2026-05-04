package langfuse

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPClient_RejectsMissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		endpoint  string
		publicKey string
		secretKey string
	}{
		{name: "no endpoint", publicKey: "pk", secretKey: "sk"},
		{name: "no public key", endpoint: "https://x", secretKey: "sk"},
		{name: "no secret key", endpoint: "https://x", publicKey: "pk"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewHTTPClient(tc.endpoint, tc.publicKey, tc.secretKey)
			require.Error(t, err)
		})
	}
}

func TestHTTPClient_LogGeneration_HappyPath(t *testing.T) {
	t.Parallel()

	var (
		gotPath string
		gotAuth string
		gotBody generationsAPIBody
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	c, err := NewHTTPClient(srv.URL, "pk", "sk", WithMaxRetries(0))
	require.NoError(t, err)

	gen := &GenerationRecord{
		TraceID:    "trace-1",
		Name:       "extract-llm",
		Model:      "claude-haiku",
		Prompt:     "p",
		Completion: "c",
		StartTime:  time.Now(),
		EndTime:    time.Now().Add(time.Second),
		Usage:      TokenUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120},
		CostUSD:    0.001,
		Metadata:   map[string]string{"commit_sha": "abc1234"},
		Level:      LevelDefault,
	}
	require.NoError(t, c.LogGeneration(context.Background(), gen))

	assert.Equal(t, "/api/public/generations", gotPath)
	assert.True(t, strings.HasPrefix(gotAuth, "Basic "), "Authorization header should be Basic auth")
	assert.Equal(t, "trace-1", gotBody.TraceID)
	assert.Equal(t, "claude-haiku", gotBody.Model)
	assert.Equal(t, 100, gotBody.PromptTokens)
	assert.InDelta(t, 0.001, gotBody.TotalCost, 0.0001)
	assert.Equal(t, "abc1234", gotBody.Metadata["commit_sha"])
}

func TestHTTPClient_RetriesOnServerError(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := NewHTTPClient(srv.URL, "pk", "sk", WithMaxRetries(3))
	require.NoError(t, err)

	err = c.Score(context.Background(), "trace-1", "test_score", 0.5, "")
	require.NoError(t, err)
	assert.EqualValues(t, 3, attempts.Load(),
		"client should retry until success on transient 5xx")
}

func TestHTTPClient_GivesUpAfterMaxRetries(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	c, err := NewHTTPClient(srv.URL, "pk", "sk", WithMaxRetries(2))
	require.NoError(t, err)

	err = c.Score(context.Background(), "trace-1", "test_score", 0.5, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
	assert.EqualValues(t, 3, attempts.Load(),
		"max retries=2 should produce exactly 3 attempts (initial + 2 retries)")
}

func TestHTTPClient_DoesNotRetryClientErrors(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"bad payload"}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewHTTPClient(srv.URL, "pk", "sk", WithMaxRetries(5))
	require.NoError(t, err)

	err = c.Score(context.Background(), "trace-1", "test_score", 0.5, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "bad payload")
	assert.EqualValues(t, 1, attempts.Load(),
		"4xx must not be retried — fail immediately")
}

func TestHTTPClient_CreateTraceReadsResponseID(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(traceAPIResponse{ID: "trace-server-id"})
	}))
	t.Cleanup(srv.Close)

	c, err := NewHTTPClient(srv.URL, "pk", "sk", WithMaxRetries(0))
	require.NoError(t, err)

	handle, err := c.CreateTrace(context.Background(), "judge-call", map[string]string{"k": "v"})
	require.NoError(t, err)
	assert.Equal(t, "trace-server-id", handle.TraceID)
}

func TestHTTPClient_CreateDatasetRunPostsOnePerItem(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/public/dataset-run-items", r.URL.Path)
		calls.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	c, err := NewHTTPClient(srv.URL, "pk", "sk", WithMaxRetries(0))
	require.NoError(t, err)

	run := &DatasetRun{
		DatasetID: "ds-1",
		RunName:   "regression-abc1234",
		ItemResults: []DatasetRunItem{
			{DatasetItemID: "i-1"},
			{DatasetItemID: "i-2"},
			{DatasetItemID: "i-3"},
		},
	}
	require.NoError(t, c.CreateDatasetRun(context.Background(), run))
	assert.EqualValues(t, 3, calls.Load(), "should POST one row per ItemResult")
}

func TestNoopClient_AllMethodsReturnNil(t *testing.T) {
	t.Parallel()

	var c NoopClient
	ctx := context.Background()

	require.NoError(t, c.LogGeneration(ctx, &GenerationRecord{}))
	require.NoError(t, c.Score(ctx, "trace", "name", 0.5, ""))
	handle, err := c.CreateTrace(ctx, "name", nil)
	require.NoError(t, err)
	assert.Empty(t, handle.TraceID, "noop trace must surface empty ID so callers can detect")
	require.NoError(t, c.CreateDatasetItem(ctx, "ds", &DatasetItem{}))
	require.NoError(t, c.CreateDatasetRun(ctx, &DatasetRun{}))
}
