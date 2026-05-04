package langfuse

import "context"

// NoopClient satisfies Client without doing any I/O. Used when
// observability.langfuse.enabled is false so the rest of the
// application calls into a Client interface unconditionally — no hot-
// path branches on "is Langfuse configured".
//
// Every method is safe for concurrent use and returns nil immediately.
type NoopClient struct{}

// Compile-time assertion that NoopClient satisfies Client. If a new
// method is added to Client, this line will fail to build until
// NoopClient grows the matching no-op stub.
var _ Client = NoopClient{}

// LogGeneration is a no-op.
func (NoopClient) LogGeneration(_ context.Context, _ *GenerationRecord) error {
	return nil
}

// Score is a no-op.
func (NoopClient) Score(_ context.Context, _, _ string, _ float64, _ string) error {
	return nil
}

// CreateTrace returns a zero-value TraceHandle and nil error. The
// empty TraceID signals to callers that no real trace was created;
// downstream code should treat it as "Langfuse disabled".
func (NoopClient) CreateTrace(_ context.Context, _ string, _ map[string]string) (TraceHandle, error) {
	return TraceHandle{}, nil
}

// CreateDatasetItem is a no-op.
func (NoopClient) CreateDatasetItem(_ context.Context, _ string, _ *DatasetItem) error {
	return nil
}

// CreateDatasetRun is a no-op.
func (NoopClient) CreateDatasetRun(_ context.Context, _ *DatasetRun) error {
	return nil
}
