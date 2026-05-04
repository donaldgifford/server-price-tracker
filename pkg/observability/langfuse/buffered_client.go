package langfuse

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
)

// BufferedClient wraps an upstream Client with a bounded async channel
// + drain goroutine so transient Langfuse outages don't block the
// extract path. When the buffer fills, the oldest queued record is
// dropped to make room for the new one and spt_langfuse_buffer_drops_total
// increments — visibility over silent loss is the design intent.
//
// Only LogGeneration and Score writes flow through the buffer (those
// are the high-volume hot-path calls). CreateTrace / CreateDatasetItem
// / CreateDatasetRun pass straight through to the upstream client —
// they're rare and the caller wants the real success/error.
//
// Construct via NewBufferedClient + Start; tear down via Stop. Stop
// drains pending writes within its context deadline so a graceful
// shutdown doesn't lose recent telemetry.
type BufferedClient struct {
	upstream Client
	log      *slog.Logger
	jobs     chan bufferJob

	wg     sync.WaitGroup
	stopMu sync.Mutex
	stopCh chan struct{} // closed by Stop to signal the drain goroutine
}

// bufferJob is one queued write the drain goroutine handles. The
// closure form keeps job dispatch type-erased so we can support
// LogGeneration + Score (and future Client methods) without growing a
// per-method discriminator union.
type bufferJob struct {
	enqueuedAt time.Time
	op         func(context.Context, Client) error
}

// BufferedClientOption configures BufferedClient construction.
type BufferedClientOption func(*BufferedClient)

// WithBufferLogger overrides the slog logger; defaults to slog.Default.
func WithBufferLogger(l *slog.Logger) BufferedClientOption {
	return func(b *BufferedClient) {
		b.log = l
	}
}

// NewBufferedClient wraps upstream with an async buffer of the given
// capacity. Capacity must be > 0; defaults to 1000 if 0 or negative
// (matches observability.langfuse.buffer_size default).
//
// The drain goroutine does not start until Start is called. Tests that
// want synchronous behaviour can skip Start entirely and exercise
// LogGeneration / Score (they will buffer but never drain).
func NewBufferedClient(upstream Client, capacity int, opts ...BufferedClientOption) *BufferedClient {
	if capacity <= 0 {
		capacity = 1000
	}
	if upstream == nil {
		upstream = NoopClient{}
	}
	b := &BufferedClient{
		upstream: upstream,
		log:      slog.Default(),
		jobs:     make(chan bufferJob, capacity),
		stopCh:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Start spawns the drain goroutine. Safe to call once; subsequent
// calls are no-ops.
func (b *BufferedClient) Start(ctx context.Context) {
	b.wg.Add(1)
	go b.drain(ctx)
}

// Stop signals the drain goroutine to exit and blocks until it does
// (or shutdownCtx expires). Returns ctx.Err on timeout. Safe to call
// concurrently / multiple times.
func (b *BufferedClient) Stop(shutdownCtx context.Context) error {
	b.stopMu.Lock()
	select {
	case <-b.stopCh:
		// Already closed.
	default:
		close(b.stopCh)
	}
	b.stopMu.Unlock()

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-shutdownCtx.Done():
		return shutdownCtx.Err()
	}
}

// LogGeneration enqueues the record. Returns nil even on overflow —
// the caller is on the hot path and shouldn't see Langfuse-side
// pressure. Drops are visible via spt_langfuse_buffer_drops_total.
func (b *BufferedClient) LogGeneration(_ context.Context, gen *GenerationRecord) error {
	job := bufferJob{
		enqueuedAt: time.Now(),
		op: func(ctx context.Context, c Client) error {
			return c.LogGeneration(ctx, gen)
		},
	}
	b.enqueue(job)
	return nil
}

// Score enqueues the score write under the same buffer semantics as
// LogGeneration.
func (b *BufferedClient) Score(_ context.Context, traceID, name string, value float64, comment string) error {
	job := bufferJob{
		enqueuedAt: time.Now(),
		op: func(ctx context.Context, c Client) error {
			return c.Score(ctx, traceID, name, value, comment)
		},
	}
	b.enqueue(job)
	return nil
}

// CreateTrace passes straight through — low volume, caller wants the
// real ID + error.
func (b *BufferedClient) CreateTrace(
	ctx context.Context,
	name string,
	metadata map[string]string,
) (TraceHandle, error) {
	return b.upstream.CreateTrace(ctx, name, metadata)
}

// CreateDatasetItem passes straight through — low volume, called from
// the operator-run dataset-bootstrap CLI.
func (b *BufferedClient) CreateDatasetItem(ctx context.Context, datasetID string, item *DatasetItem) error {
	return b.upstream.CreateDatasetItem(ctx, datasetID, item)
}

// CreateDatasetRun passes straight through — same reason as the others.
func (b *BufferedClient) CreateDatasetRun(ctx context.Context, run *DatasetRun) error {
	return b.upstream.CreateDatasetRun(ctx, run)
}

// enqueue is the buffer overflow policy. The pattern is "try to send;
// if full, evict the oldest queued job (a non-blocking drain) and try
// once more". If the second attempt still fails (drain raced with
// another sender), increment the drop counter and move on.
func (b *BufferedClient) enqueue(job bufferJob) {
	select {
	case b.jobs <- job:
		metrics.LangfuseBufferDepth.Set(float64(len(b.jobs)))
		return
	default:
	}

	// Buffer full — drop oldest, then try once more.
	select {
	case <-b.jobs:
		metrics.LangfuseBufferDropsTotal.Inc()
	default:
	}

	select {
	case b.jobs <- job:
		metrics.LangfuseBufferDepth.Set(float64(len(b.jobs)))
	default:
		// Still full — racing senders. Drop this one too.
		metrics.LangfuseBufferDropsTotal.Inc()
	}
}

// drain is the background goroutine that ships queued jobs to the
// upstream client. Exits cleanly when stopCh closes; on parent ctx
// cancellation it stops too (so K8s pod shutdown doesn't hang).
func (b *BufferedClient) drain(ctx context.Context) {
	defer b.wg.Done()

	for {
		select {
		case <-b.stopCh:
			b.flushRemaining(ctx)
			return
		case <-ctx.Done():
			return
		case job := <-b.jobs:
			b.runJob(ctx, job)
			metrics.LangfuseBufferDepth.Set(float64(len(b.jobs)))
		}
	}
}

// flushRemaining drains any jobs still in the channel at Stop time.
// Best-effort: if the upstream is unreachable, the post will still
// fail and we move on; but we give every queued record a chance.
func (b *BufferedClient) flushRemaining(ctx context.Context) {
	for {
		select {
		case job := <-b.jobs:
			b.runJob(ctx, job)
		default:
			metrics.LangfuseBufferDepth.Set(0)
			return
		}
	}
}

func (b *BufferedClient) runJob(ctx context.Context, job bufferJob) {
	start := time.Now()
	err := job.op(ctx, b.upstream)
	metrics.LangfuseWriteDuration.Observe(time.Since(start).Seconds())

	result := "success"
	if err != nil {
		result = "error"
		// Surface at debug — the metric is the actionable signal.
		// We don't log every failure as warn to avoid log floods
		// during sustained outages.
		b.log.Debug("langfuse buffered write failed",
			"error", err,
			"queued_for", time.Since(job.enqueuedAt).String(),
		)
	}
	metrics.LangfuseWritesTotal.WithLabelValues(result).Inc()

	if err != nil && errors.Is(err, context.Canceled) {
		// Shutdown raced — no point reporting separately.
		return
	}
}

// Compile-time assertion that BufferedClient satisfies Client.
var _ Client = (*BufferedClient)(nil)
