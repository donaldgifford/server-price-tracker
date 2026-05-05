package langfuse

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// BufferMetrics is the metrics surface BufferedClient needs to emit
// observability signals. The langfuse package can't import
// internal/metrics directly (would break the pkg/ → internal/ boundary
// for external tools), so callers inject an adapter that wraps the
// global Prometheus collectors. Tests use the no-op default.
type BufferMetrics interface {
	SetDepth(depth int)
	RecordDrop()
	RecordWrite(success bool)
	ObserveWriteDuration(d time.Duration)
}

// noopBufferMetrics is the default when no adapter is supplied.
type noopBufferMetrics struct{}

func (noopBufferMetrics) SetDepth(int)                       {}
func (noopBufferMetrics) RecordDrop()                        {}
func (noopBufferMetrics) RecordWrite(bool)                   {}
func (noopBufferMetrics) ObserveWriteDuration(time.Duration) {}

// BufferedClient wraps an upstream Client with a bounded async channel
// + drain goroutine so transient Langfuse outages don't block the
// extract path. When the buffer fills, the new record is dropped
// (drop-newest) and spt_langfuse_buffer_drops_total increments —
// visibility over silent loss is the design intent. Drop-newest is
// what every Go observability library converges on (otel-go,
// prometheus client, …) because it's race-free under concurrent
// senders without needing a mutex on the hot path.
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
	metrics  BufferMetrics
	jobs     chan bufferJob

	startOnce sync.Once
	wg        sync.WaitGroup
	stopMu    sync.Mutex
	stopCh    chan struct{} // closed by Stop to signal the drain goroutine

	// shutdownCtx is the context Stop hands to the drain so
	// flushRemaining can issue HTTP calls under the shutdown deadline
	// even if the parent ctx (passed to Start) is already canceled.
	// Guarded by stopMu — written by Stop, read by drain after stopCh
	// fires.
	shutdownCtx context.Context //nolint:containedctx // Stop deadline must reach drain
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

// WithBufferMetrics injects the metrics adapter. Required for the
// production wiring; tests fall through to the no-op default.
func WithBufferMetrics(m BufferMetrics) BufferedClientOption {
	return func(b *BufferedClient) {
		if m != nil {
			b.metrics = m
		}
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
		metrics:  noopBufferMetrics{},
		jobs:     make(chan bufferJob, capacity),
		stopCh:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Start spawns the drain goroutine. Safe to call concurrently and to
// call multiple times — startOnce ensures exactly one drain. If Stop
// already ran, Start is a no-op (we never spawn a goroutine that
// would hold wg past a completed Wait).
func (b *BufferedClient) Start(ctx context.Context) {
	b.startOnce.Do(func() {
		select {
		case <-b.stopCh:
			// Stop already ran — never spawn the drain. Otherwise
			// the next Stop call would hang on wg.Wait forever
			// because this goroutine would hold wg with no signal
			// to exit.
			return
		default:
		}
		b.wg.Add(1)
		go b.drain(ctx)
	})
}

// Stop signals the drain goroutine to exit and blocks until it does
// (or shutdownCtx expires). Returns ctx.Err on timeout. Safe to call
// concurrently / multiple times.
//
// shutdownCtx is also handed to the drain so flushRemaining issues
// HTTP calls under this deadline. The typical K8s SIGTERM path
// cancels the parent ctx (the one passed to Start) before calling
// Stop — without this thread-through, every flush would return
// context.Canceled immediately and the "give every queued record a
// chance" promise would break exactly when it matters.
func (b *BufferedClient) Stop(shutdownCtx context.Context) error {
	b.stopMu.Lock()
	b.shutdownCtx = shutdownCtx
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

// enqueue is the buffer overflow policy. Drop-newest: try to send;
// if the buffer is full, drop the new record and increment the drop
// counter. Race-free under concurrent senders (no mutex needed) and
// matches the standard contract operators expect from "buffered" —
// see INV-0001 HIGH-3 for why we moved away from drop-oldest.
func (b *BufferedClient) enqueue(job bufferJob) {
	select {
	case b.jobs <- job:
		b.metrics.SetDepth(len(b.jobs))
	default:
		b.metrics.RecordDrop()
	}
}

// drain is the background goroutine that ships queued jobs to the
// upstream client. Lifecycle is driven exclusively by stopCh — the
// parent ctx (passed at Start) is used for individual upstream
// calls but not for drain exit. Tying drain exit to parent ctx
// cancellation was a footgun: a K8s SIGTERM that cancels root ctx
// before Stop runs would skip flushRemaining and lose records.
//
// On stopCh-fired shutdown we flush under the Stop-supplied
// shutdownCtx (set by Stop before close(stopCh)) so HTTP calls
// have a working deadline even if the parent ctx is already canceled.
func (b *BufferedClient) drain(ctx context.Context) {
	defer b.wg.Done()

	for {
		select {
		case <-b.stopCh:
			b.stopMu.Lock()
			fctx := b.shutdownCtx //nolint:contextcheck // shutdownCtx intentionally separate from drain's parent ctx (see Stop)
			b.stopMu.Unlock()
			if fctx == nil {
				// Stop closed stopCh without taking the mutex path
				// (shouldn't happen, but defensive). Fall back to
				// parent ctx — at worst flush no-ops on canceled.
				fctx = ctx
			}
			b.flushRemaining(fctx)
			return
		case job := <-b.jobs:
			b.runJob(ctx, job)
			b.metrics.SetDepth(len(b.jobs))
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
			b.metrics.SetDepth(0)
			return
		}
	}
}

func (b *BufferedClient) runJob(ctx context.Context, job bufferJob) {
	start := time.Now()
	err := job.op(ctx, b.upstream)
	b.metrics.ObserveWriteDuration(time.Since(start))

	if err != nil {
		// Surface at debug — the metric is the actionable signal.
		// We don't log every failure as warn to avoid log floods
		// during sustained outages.
		b.log.Debug("langfuse buffered write failed",
			"error", err,
			"queued_for", time.Since(job.enqueuedAt).String(),
		)
	}
	b.metrics.RecordWrite(err == nil)
}

// Compile-time assertion that BufferedClient satisfies Client.
var _ Client = (*BufferedClient)(nil)
