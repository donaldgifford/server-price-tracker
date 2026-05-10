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

func TestHTTPClient_LogGeneration_PostsIngestionEnvelope(t *testing.T) {
	t.Parallel()

	var (
		gotPath string
		gotAuth string
		gotReq  ingestionRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_ = json.NewEncoder(w).Encode(ingestionResponse{
			Successes: []ingestionSuccess{{ID: "ok", Status: 201}},
		})
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

	assert.Equal(t, "/api/public/ingestion", gotPath)
	assert.True(t, strings.HasPrefix(gotAuth, "Basic "), "Authorization header should be Basic auth")
	require.Len(t, gotReq.Batch, 1)
	evt := gotReq.Batch[0]
	assert.Equal(t, ingestionEventGenerationCreate, evt.Type)
	assert.NotEmpty(t, evt.ID, "event id must be populated for ingestion")

	// evt.Body decodes as map[string]any — the test asserts on the wire
	// shape (the operator-visible JSON), not on the struct round-trip.
	bodyMap, ok := evt.Body.(map[string]any)
	require.True(t, ok, "body should decode as JSON object")
	assert.Equal(t, "trace-1", bodyMap["traceId"])
	assert.Equal(t, "claude-haiku", bodyMap["model"])
	assert.Equal(t, "p", bodyMap["input"], "input must populate — the bug this PR fixes")
	assert.Equal(t, "c", bodyMap["output"], "output must populate — the bug this PR fixes")
	assert.NotEmpty(t, bodyMap["id"], "observation id must be populated")

	usage, ok := bodyMap["usage"].(map[string]any)
	require.True(t, ok, "usage must be a nested object in v3 ingestion API")
	assert.EqualValues(t, 100, usage["input"])
	assert.EqualValues(t, 20, usage["output"])
	assert.EqualValues(t, 120, usage["total"])
	assert.Equal(t, "TOKENS", usage["unit"])
	assert.InDelta(t, 0.001, usage["totalCost"], 0.0001)

	meta, ok := bodyMap["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "abc1234", meta["commit_sha"])
}

func TestHTTPClient_LogGeneration_SurfacesPerEventErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 200 OK with body-level rejection — the exact case Langfuse
		// uses for malformed events. Without explicit error parsing the
		// caller would think the write succeeded.
		_ = json.NewEncoder(w).Encode(ingestionResponse{
			Errors: []ingestionError{{
				ID:      "evt-1",
				Status:  400,
				Message: "missing required field traceId",
			}},
		})
	}))
	t.Cleanup(srv.Close)

	c, err := NewHTTPClient(srv.URL, "pk", "sk", WithMaxRetries(0))
	require.NoError(t, err)

	err = c.LogGeneration(context.Background(), &GenerationRecord{
		TraceID: "t", Name: "n", Model: "m", Prompt: "p", Completion: "c",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required field traceId")
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
		_ = json.NewEncoder(w).Encode(ingestionResponse{
			Successes: []ingestionSuccess{{ID: "ok", Status: 201}},
		})
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

func TestHTTPClient_CreateTraceReturnsClientGeneratedID(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotReq ingestionRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_ = json.NewEncoder(w).Encode(ingestionResponse{
			Successes: []ingestionSuccess{{ID: "ok", Status: 201}},
		})
	}))
	t.Cleanup(srv.Close)

	c, err := NewHTTPClient(srv.URL, "pk", "sk", WithMaxRetries(0))
	require.NoError(t, err)

	handle, err := c.CreateTrace(context.Background(), "judge-call", map[string]string{"k": "v"})
	require.NoError(t, err)
	assert.NotEmpty(t, handle.TraceID, "trace id must be client-generated under ingestion API")

	assert.Equal(t, "/api/public/ingestion", gotPath)
	require.Len(t, gotReq.Batch, 1)
	evt := gotReq.Batch[0]
	assert.Equal(t, ingestionEventTraceCreate, evt.Type)
	bodyMap, ok := evt.Body.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, handle.TraceID, bodyMap["id"], "trace body id must match the handle returned to the caller")
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
