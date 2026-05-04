package observability

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"

	"github.com/donaldgifford/server-price-tracker/internal/config"
)

func TestInit_Disabled(t *testing.T) {
	t.Parallel()

	shutdown, err := Init(context.Background(), config.OtelConfig{Enabled: false})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// noop shutdown should be safe to call multiple times.
	require.NoError(t, shutdown(context.Background()))
	require.NoError(t, shutdown(context.Background()))

	// Global tracer/meter providers stay as the default no-ops.
	tracer := otel.Tracer("test")
	require.NotNil(t, tracer)
	_, span := tracer.Start(context.Background(), "noop-span")
	span.End()

	meter := otel.Meter("test")
	require.NotNil(t, meter)
}

func TestInit_EnabledRequiresEndpoint(t *testing.T) {
	t.Parallel()

	_, err := Init(context.Background(), config.OtelConfig{Enabled: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint is required")
}

func TestInit_EnabledAgainstStubCollector(t *testing.T) {
	t.Parallel()

	// Stand up an in-process gRPC server so the OTLP exporter has somewhere
	// to dial. We don't implement the OTLP service — the exporter only
	// fails at export time, not at construction. That's enough for Init
	// to succeed and emit a single smoke span before shutdown flushes
	// (the flush will fail silently against the stub, which is fine —
	// we only verify Init wires the SDK correctly).
	lc := net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	server := grpc.NewServer()
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()

	cfg := config.OtelConfig{
		Enabled:     true,
		Endpoint:    listener.Addr().String(),
		ServiceName: "spt-test",
		Insecure:    true,
		Timeout:     2 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := Init(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Emit one span to confirm the global tracer provider was swapped in.
	tracer := otel.Tracer("smoke")
	_, span := tracer.Start(ctx, "smoke-span")
	span.End()

	// Shutdown might error against the stub (no real OTLP service) — we
	// only assert it doesn't panic and the call returns within the
	// shutdown context's deadline.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = shutdown(shutdownCtx)
}

func TestChainShutdown_AggregatesErrors(t *testing.T) {
	t.Parallel()

	calls := 0
	first := func(_ context.Context) error {
		calls++
		return nil
	}
	second := func(_ context.Context) error {
		calls++
		return assert.AnError
	}
	third := func(_ context.Context) error {
		calls++
		return nil
	}

	chained := chainShutdown(first, second, third)
	err := chained(context.Background())
	require.Error(t, err)
	assert.Equal(t, 3, calls)
}

func TestChainShutdown_AllOK(t *testing.T) {
	t.Parallel()

	calls := 0
	first := func(_ context.Context) error {
		calls++
		return nil
	}
	second := func(_ context.Context) error {
		calls++
		return nil
	}

	chained := chainShutdown(first, second)
	require.NoError(t, chained(context.Background()))
	assert.Equal(t, 2, calls)
}
