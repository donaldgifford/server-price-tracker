package langfuse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const (
	defaultTimeout    = 10 * time.Second
	defaultMaxRetries = 3
	// retryBaseDelay is the first backoff between attempts. Subsequent
	// attempts double the delay (50ms -> 100ms -> 200ms by default).
	retryBaseDelay = 50 * time.Millisecond
	// maxResponseBytes caps a 2xx response body before JSON decode so a
	// misbehaving Langfuse endpoint can't OOM the process with a multi-
	// GB body. The expected payloads (TraceHandle.id, etc.) are <1KB;
	// 1MiB is generous defense in depth. See INV-0001 MEDIUM-4.
	maxResponseBytes = 1 << 20
)

// HTTPClient talks to a Langfuse instance via the public REST API.
// Authenticated with the provisioned public+secret keys via HTTP Basic
// auth, retries 5xx + transient transport errors with exponential
// backoff, and logs request shape via slog at debug level.
//
// Construct via NewHTTPClient; safe for concurrent use.
type HTTPClient struct {
	endpoint   string
	publicKey  string
	secretKey  string
	httpClient *http.Client
	maxRetries int
}

// HTTPClientOption configures HTTPClient construction.
type HTTPClientOption func(*HTTPClient)

// WithHTTPClient overrides the underlying *http.Client. Tests use this
// to point at httptest.Server.
func WithHTTPClient(c *http.Client) HTTPClientOption {
	return func(h *HTTPClient) {
		h.httpClient = c
	}
}

// WithMaxRetries overrides the retry budget for transient failures.
// Zero disables retries (useful for tests).
func WithMaxRetries(n int) HTTPClientOption {
	return func(h *HTTPClient) {
		h.maxRetries = n
	}
}

// NewHTTPClient constructs a new Langfuse HTTPClient pointing at
// endpoint with the supplied keys. Returns an error if endpoint or
// either key is empty — fail fast at construction beats failing
// silently at every write.
func NewHTTPClient(endpoint, publicKey, secretKey string, opts ...HTTPClientOption) (*HTTPClient, error) {
	if endpoint == "" {
		return nil, errors.New("langfuse: endpoint is required")
	}
	if publicKey == "" || secretKey == "" {
		return nil, errors.New("langfuse: public_key and secret_key are required")
	}

	c := &HTTPClient{
		endpoint:   endpoint,
		publicKey:  publicKey,
		secretKey:  secretKey,
		httpClient: &http.Client{Timeout: defaultTimeout},
		maxRetries: defaultMaxRetries,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Compile-time assertion that HTTPClient satisfies Client.
var _ Client = (*HTTPClient)(nil)

// LogGeneration ships one generation as a generation-create event on the
// ingestion API. The legacy /api/public/generations endpoint is retained
// by Langfuse v3 only as a partial-compat shim — it accepts the request
// but drops input/output, leaving prompts invisible in the UI. The
// canonical path is the batch ingestion endpoint with a per-event body.
func (c *HTTPClient) LogGeneration(ctx context.Context, gen *GenerationRecord) error {
	body := generationCreateBody{
		ID:            uuid.NewString(),
		TraceID:       gen.TraceID,
		Name:          gen.Name,
		Model:         gen.Model,
		Input:         gen.Prompt,
		Output:        gen.Completion,
		StartTime:     gen.StartTime,
		EndTime:       gen.EndTime,
		Metadata:      gen.Metadata,
		Level:         string(gen.Level),
		StatusMessage: gen.StatusMsg,
		Usage: &ingestionUsage{
			Input:  gen.Usage.InputTokens,
			Output: gen.Usage.OutputTokens,
			Total:  gen.Usage.TotalTokens,
			Unit:   "TOKENS",
		},
	}
	if gen.CostUSD > 0 {
		body.Usage.TotalCost = gen.CostUSD
	}
	return c.ingest(ctx, ingestionEventGenerationCreate, body)
}

// Score posts a numeric score as a score-create event on the ingestion
// API.
func (c *HTTPClient) Score(ctx context.Context, traceID, name string, value float64, comment string) error {
	body := scoreCreateBody{
		ID:      uuid.NewString(),
		TraceID: traceID,
		Name:    name,
		Value:   value,
		Comment: comment,
	}
	return c.ingest(ctx, ingestionEventScoreCreate, body)
}

// CreateTrace creates a trace via a trace-create ingestion event. The
// trace ID is generated client-side (the ingestion API doesn't return
// server-assigned IDs) and surfaced via TraceHandle.
func (c *HTTPClient) CreateTrace(
	ctx context.Context,
	name string,
	metadata map[string]string,
) (TraceHandle, error) {
	traceID := uuid.NewString()
	body := traceCreateBody{
		ID:       traceID,
		Name:     name,
		Metadata: metadata,
	}
	if err := c.ingest(ctx, ingestionEventTraceCreate, body); err != nil {
		return TraceHandle{}, err
	}
	return TraceHandle{TraceID: traceID}, nil
}

// CreateDatasetItem posts to /api/public/dataset-items. When item.ID
// is non-empty it's threaded through as the canonical item ID for
// idempotent upserts.
func (c *HTTPClient) CreateDatasetItem(ctx context.Context, datasetID string, item *DatasetItem) error {
	body := datasetItemAPIBody{
		ID:             item.ID,
		DatasetID:      datasetID,
		Input:          item.Input,
		ExpectedOutput: item.ExpectedOutput,
		Metadata:       item.Metadata,
	}
	return c.post(ctx, "/api/public/dataset-items", body)
}

// CreateDatasetRun posts to /api/public/dataset-run-items in a single
// batched call. Each ItemResult becomes a separate run item.
func (c *HTTPClient) CreateDatasetRun(ctx context.Context, run *DatasetRun) error {
	for i := range run.ItemResults {
		item := &run.ItemResults[i]
		body := datasetRunItemAPIBody{
			RunName:       run.RunName,
			DatasetItemID: item.DatasetItemID,
			TraceID:       item.TraceID,
			Output:        item.Output,
			Metadata:      run.Metadata,
		}
		if err := c.post(ctx, "/api/public/dataset-run-items", body); err != nil {
			return fmt.Errorf("posting dataset run item %s: %w", item.DatasetItemID, err)
		}
	}
	return nil
}

// ingest wraps body in a single-event ingestion batch and POSTs to
// /api/public/ingestion. The ingestion API returns a 207-style envelope
// with per-event successes and errors — a 2xx HTTP status alone doesn't
// mean the event was accepted, so we surface body-level errors as a
// non-retryable client failure.
func (c *HTTPClient) ingest(ctx context.Context, eventType string, body any) error {
	envelope := ingestionRequest{
		Batch: []ingestionEvent{{
			ID:        uuid.NewString(),
			Type:      eventType,
			Timestamp: time.Now().UTC(),
			Body:      body,
		}},
	}
	var resp ingestionResponse
	if err := c.postWithResponse(ctx, "/api/public/ingestion", envelope, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		first := resp.Errors[0]
		return fmt.Errorf("langfuse ingestion rejected event %s (status %d): %s",
			first.ID, first.Status, first.Message)
	}
	return nil
}

// post executes a JSON POST against path with retry on 5xx and
// transient transport errors. Discards the response body.
func (c *HTTPClient) post(ctx context.Context, path string, body any) error {
	return c.postWithResponse(ctx, path, body, nil)
}

// postWithResponse is post() that JSON-decodes a 2xx response body
// into out (when out != nil). Used by CreateTrace which needs the
// server-assigned ID.
func (c *HTTPClient) postWithResponse(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshalling langfuse %s payload: %w", path, err)
	}

	url := c.endpoint + path
	delay := retryBaseDelay

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		err = c.attemptOnce(ctx, url, payload, out)
		if err == nil {
			return nil
		}
		if !isRetryable(err) || attempt == c.maxRetries {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}
	return err
}

func (c *HTTPClient) attemptOnce(ctx context.Context, url string, payload []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building langfuse request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.publicKey, c.secretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return retryableError{err: fmt.Errorf("langfuse transport: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		drain(resp.Body)
		return retryableError{err: fmt.Errorf("langfuse server status %d", resp.StatusCode)}
	}
	if resp.StatusCode >= 400 {
		// 4xx is a client error — non-retryable. Surface the body for
		// diagnosis since most cases mean malformed payload or auth.
		body := readErrorBody(resp.Body)
		return fmt.Errorf("langfuse client status %d: %s", resp.StatusCode, body)
	}

	if out == nil {
		drain(resp.Body)
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("decoding langfuse response: %w", err)
	}
	return nil
}

// drain discards the remaining response body so the underlying TCP
// connection can be reused. Errors are intentionally ignored — we
// already have whatever signal we need from the status code; a body
// read error doesn't change the caller's outcome.
func drain(r io.Reader) {
	if _, err := io.Copy(io.Discard, r); err != nil {
		return
	}
}

// readErrorBody reads up to 16 KiB of the response body for inclusion
// in a non-retryable error message. Returns "" if the read fails;
// surfacing read errors here would mask the actual HTTP status.
func readErrorBody(r io.Reader) string {
	buf, err := io.ReadAll(io.LimitReader(r, 1<<14))
	if err != nil {
		return ""
	}
	return string(buf)
}

// retryableError marks transient failures so post() knows to back off
// and retry rather than surface immediately.
type retryableError struct{ err error }

func (e retryableError) Error() string { return e.err.Error() }
func (e retryableError) Unwrap() error { return e.err }

func isRetryable(err error) bool {
	var re retryableError
	return errors.As(err, &re)
}

// Ingestion API event types accepted by /api/public/ingestion. Langfuse
// v3 deprecated the standalone POST endpoints (/api/public/generations,
// /api/public/scores, /api/public/traces) — they linger as partial-compat
// shims that drop input/output. The ingestion batch envelope is canonical.
const (
	ingestionEventGenerationCreate = "generation-create"
	ingestionEventScoreCreate      = "score-create"
	ingestionEventTraceCreate      = "trace-create"
)

// ingestionRequest is the batch envelope POSTed to /api/public/ingestion.
type ingestionRequest struct {
	Batch []ingestionEvent `json:"batch"`
}

// ingestionEvent wraps one observation/trace/score body. ID is the
// per-event identifier (separate from the body's own observation ID);
// Type discriminates the body shape.
type ingestionEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Body      any       `json:"body"`
}

// ingestionResponse mirrors the ingestion API's per-event success/error
// envelope. A 2xx HTTP status with non-empty Errors means the request
// reached Langfuse but the event itself was rejected.
type ingestionResponse struct {
	Successes []ingestionSuccess `json:"successes"`
	Errors    []ingestionError   `json:"errors"`
}

type ingestionSuccess struct {
	ID     string `json:"id"`
	Status int    `json:"status"`
}

type ingestionError struct {
	ID      string `json:"id"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// generationCreateBody is the body of a generation-create ingestion
// event. Token usage lives nested under `usage` per the v3 schema —
// flat promptTokens/completionTokens fields on the legacy endpoint were
// the reason input/output rendered as null in the UI.
type generationCreateBody struct {
	ID            string            `json:"id"`
	TraceID       string            `json:"traceId,omitempty"`
	Name          string            `json:"name,omitempty"`
	Model         string            `json:"model,omitempty"`
	Input         string            `json:"input,omitempty"`
	Output        string            `json:"output,omitempty"`
	StartTime     time.Time         `json:"startTime,omitzero"`
	EndTime       time.Time         `json:"endTime,omitzero"`
	Usage         *ingestionUsage   `json:"usage,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Level         string            `json:"level,omitempty"`
	StatusMessage string            `json:"statusMessage,omitempty"`
}

// ingestionUsage is the v3 nested usage object on a generation event.
type ingestionUsage struct {
	Input     int     `json:"input,omitempty"`
	Output    int     `json:"output,omitempty"`
	Total     int     `json:"total,omitempty"`
	Unit      string  `json:"unit,omitempty"`
	TotalCost float64 `json:"totalCost,omitempty"`
}

type scoreCreateBody struct {
	ID      string  `json:"id"`
	TraceID string  `json:"traceId"`
	Name    string  `json:"name"`
	Value   float64 `json:"value"`
	Comment string  `json:"comment,omitempty"`
}

type traceCreateBody struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// datasetItemAPIBody and datasetRunItemAPIBody are kept on their
// dedicated /api/public/dataset-items and /api/public/dataset-run-items
// endpoints — those are part of the Datasets API surface and are not
// deprecated alongside the standalone observability endpoints.

type datasetItemAPIBody struct {
	ID             string            `json:"id,omitempty"`
	DatasetID      string            `json:"datasetId"`
	Input          map[string]any    `json:"input,omitempty"`
	ExpectedOutput map[string]any    `json:"expectedOutput,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type datasetRunItemAPIBody struct {
	RunName       string            `json:"runName"`
	DatasetItemID string            `json:"datasetItemId"`
	TraceID       string            `json:"traceId,omitempty"`
	Output        map[string]any    `json:"output,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}
