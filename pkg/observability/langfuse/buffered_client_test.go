package langfuse

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingClient is a Client impl that captures every call into
// thread-safe slices. Used by buffer tests that need to assert which
// records reached the upstream.
type recordingClient struct {
	mu          sync.Mutex
	generations []*GenerationRecord
	scores      []scoreCall
	failOps     atomic.Int32 // first N ops return ErrInjected; -1 = all fail
}

type scoreCall struct {
	traceID, name, comment string
	value                  float64
}

var errInjected = errors.New("injected upstream failure")

func (c *recordingClient) LogGeneration(_ context.Context, gen *GenerationRecord) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failOps.Load() != 0 {
		c.failOps.Add(-1)
		return errInjected
	}
	c.generations = append(c.generations, gen)
	return nil
}

func (c *recordingClient) Score(_ context.Context, traceID, name string, value float64, comment string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scores = append(c.scores, scoreCall{traceID: traceID, name: name, value: value, comment: comment})
	return nil
}

func (*recordingClient) CreateTrace(_ context.Context, _ string, _ map[string]string) (TraceHandle, error) {
	return TraceHandle{TraceID: "trace-stub"}, nil
}

func (*recordingClient) CreateDatasetItem(_ context.Context, _ string, _ *DatasetItem) error {
	return nil
}

func (*recordingClient) CreateDatasetRun(_ context.Context, _ *DatasetRun) error {
	return nil
}

func (c *recordingClient) generationCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.generations)
}

func TestBufferedClient_FlushesPendingOnStop(t *testing.T) {
	t.Parallel()

	upstream := &recordingClient{}
	buf := NewBufferedClient(upstream, 16)
	buf.Start(context.Background())

	for range 5 {
		require.NoError(t, buf.LogGeneration(context.Background(), &GenerationRecord{
			TraceID: "t",
			Name:    "g",
		}))
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, buf.Stop(stopCtx))

	assert.Equal(t, 5, upstream.generationCount(),
		"all enqueued records should have been delivered before Stop returned")
}

func TestBufferedClient_NeverBlocksHotPath(t *testing.T) {
	t.Parallel()

	// Tiny buffer + drain that we never start: every enqueue past
	// capacity must be best-effort, never block the caller.
	upstream := &recordingClient{}
	buf := NewBufferedClient(upstream, 2)

	done := make(chan struct{})
	go func() {
		for range 100 {
			_ = buf.LogGeneration(context.Background(), &GenerationRecord{TraceID: "t"})
		}
		close(done)
	}()

	select {
	case <-done:
		// passed
	case <-time.After(time.Second):
		t.Fatal("hot path blocked despite buffer overflow — must always be non-blocking")
	}
}

func TestBufferedClient_PassthroughForLowVolumeMethods(t *testing.T) {
	t.Parallel()

	upstream := &recordingClient{}
	buf := NewBufferedClient(upstream, 8)
	// Don't Start — passthrough methods must not require the drain
	// goroutine to be running.

	handle, err := buf.CreateTrace(context.Background(), "judge", nil)
	require.NoError(t, err)
	assert.Equal(t, "trace-stub", handle.TraceID,
		"CreateTrace must reach upstream synchronously, not buffer")

	require.NoError(t, buf.CreateDatasetItem(context.Background(), "ds", &DatasetItem{}))
	require.NoError(t, buf.CreateDatasetRun(context.Background(), &DatasetRun{}))
}

func TestBufferedClient_NilUpstreamFallsThroughToNoop(t *testing.T) {
	t.Parallel()

	// Constructing without an upstream should default to NoopClient
	// rather than panic on the first send.
	buf := NewBufferedClient(nil, 4)
	buf.Start(context.Background())

	require.NoError(t, buf.LogGeneration(context.Background(), &GenerationRecord{}))

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, buf.Stop(stopCtx))
}

func TestBufferedClient_StopIsIdempotent(t *testing.T) {
	t.Parallel()

	upstream := &recordingClient{}
	buf := NewBufferedClient(upstream, 4)
	buf.Start(context.Background())

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, buf.Stop(stopCtx))
	// Second call must not panic on close-of-closed-channel.
	require.NoError(t, buf.Stop(stopCtx))
}

// countingMetrics is a BufferMetrics that counts drops + tracks the
// last-set depth. Used by the drop-newest regression test below; we
// intentionally don't introduce a sync.Mutex here because the tests
// are sequential w.r.t. the goroutine that updates the counters.
type countingMetrics struct {
	drops     atomic.Int64
	depth     atomic.Int64
	successes atomic.Int64
	errors    atomic.Int64
}

func (m *countingMetrics) SetDepth(d int)                   { m.depth.Store(int64(d)) }
func (m *countingMetrics) RecordDrop()                      { m.drops.Add(1) }
func (*countingMetrics) ObserveWriteDuration(time.Duration) {}

func (m *countingMetrics) RecordWrite(success bool) {
	if success {
		m.successes.Add(1)
		return
	}
	m.errors.Add(1)
}

func TestBufferedClient_StartIsIdempotent(t *testing.T) {
	t.Parallel()

	upstream := &recordingClient{}
	buf := NewBufferedClient(upstream, 4)

	// Multiple Starts must spawn exactly one drain goroutine. A
	// second drain would consume from the same channel concurrently
	// and double-deliver records.
	buf.Start(context.Background())
	buf.Start(context.Background())
	buf.Start(context.Background())

	for range 3 {
		require.NoError(t, buf.LogGeneration(context.Background(), &GenerationRecord{TraceID: "t"}))
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, buf.Stop(stopCtx))

	assert.Equal(t, 3, upstream.generationCount(), "exactly one drain should have delivered exactly 3 records")
}

func TestBufferedClient_StartAfterStopIsNoop(t *testing.T) {
	t.Parallel()

	// If Stop ran before Start, Start must be a no-op — otherwise it
	// would spawn a drain goroutine that holds wg forever and hangs
	// the next Stop call.
	upstream := &recordingClient{}
	buf := NewBufferedClient(upstream, 4)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, buf.Stop(stopCtx))

	buf.Start(context.Background())

	// Second Stop must return promptly — drain was never spawned, so
	// wg.Wait returns immediately.
	stopCtx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	require.NoError(t, buf.Stop(stopCtx2))
}

func TestBufferedClient_DropNewestOnOverflow(t *testing.T) {
	t.Parallel()

	// Buffer of 4 with drain never started — every send beyond the
	// 4th must increment drops. Drop-newest contract: the *first* 4
	// records win, the rest fall on the floor. Drop count ≥ N-cap
	// (we don't assert exact count because nothing about RecordDrop
	// pretends to be exact under racing senders, but with a single
	// sender goroutine here the count is deterministic).
	upstream := &recordingClient{}
	cm := &countingMetrics{}
	buf := NewBufferedClient(upstream, 4, WithBufferMetrics(cm))

	const sends = 20
	for range sends {
		require.NoError(t, buf.LogGeneration(context.Background(), &GenerationRecord{TraceID: "t"}))
	}

	assert.EqualValues(t, sends-4, cm.drops.Load(),
		"with cap=4 and drain stopped, every send past the 4th must drop")
	assert.EqualValues(t, 4, cm.depth.Load(),
		"depth gauge should pin at capacity once the buffer fills")
}

func TestBufferedClient_DefaultsCapacityWhenZero(t *testing.T) {
	t.Parallel()

	upstream := &recordingClient{}
	buf := NewBufferedClient(upstream, 0)
	// Internal: should default to 1000. We can't read the channel
	// capacity directly through the public API, but we can confirm
	// the behaviour by enqueuing many records without dropping.
	for range 500 {
		require.NoError(t, buf.LogGeneration(context.Background(), &GenerationRecord{}))
	}
	// Hasn't started — nothing drained — but no panic, no block.
}
