// Package observability provides OpenTelemetry SDK initialisation for the
// server-price-tracker binary. Init returns a no-op tracer + meter when
// observability is disabled in config, so calling code never has to branch
// on "is OTel enabled" — it just calls otel.Tracer(...) / otel.Meter(...).
//
// This is the IMPL-0019 Phase 1 foundation. Phase 2+ adds the spans.
package observability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"

	"github.com/donaldgifford/server-price-tracker/internal/config"
	"github.com/donaldgifford/server-price-tracker/internal/version"
)

// ShutdownFunc releases all OTel resources (flushes pending spans/metrics
// and tears down exporters). Safe to call when Init returned a no-op
// shutdown — it returns nil immediately.
type ShutdownFunc func(context.Context) error

// Init configures the global OTel TracerProvider + MeterProvider from
// cfg. When cfg.Enabled is false, Init returns a no-op ShutdownFunc and
// nil error — the global providers stay as their default no-op
// implementations (otel.Tracer / otel.Meter return spans/instruments
// that record nothing).
//
// When enabled, Init wires:
//   - An OTLP/gRPC trace exporter against cfg.Endpoint with insecure
//     toggle controlled by cfg.Insecure.
//   - sdktrace.AlwaysSample(): the app emits 100% of spans. Sampling
//     decisions live in the Collector's tail_sampling processor — see
//     IMPL-0019 Phase 1 / DESIGN-0016 Open Question 5.
//   - An OTLP/gRPC metric exporter on the same endpoint (60s interval).
//   - W3C TraceContext + Baggage propagators globally.
//   - A resource carrying service.name / service.version /
//     service.instance.id (commit SHA).
//
// The returned ShutdownFunc must be deferred by the caller; it flushes
// any pending exports before tearing down providers.
func Init(ctx context.Context, cfg config.OtelConfig) (ShutdownFunc, error) {
	if !cfg.Enabled {
		return noopShutdown, nil
	}

	if cfg.Endpoint == "" {
		return nil, errors.New("observability.otel.endpoint is required when otel is enabled")
	}

	res, err := buildResource(ctx, cfg.ServiceName)
	if err != nil {
		return nil, fmt.Errorf("building otel resource: %w", err)
	}

	traceShutdown, err := initTracerProvider(ctx, cfg, res)
	if err != nil {
		return nil, fmt.Errorf("initialising tracer provider: %w", err)
	}

	meterShutdown, err := initMeterProvider(ctx, cfg, res)
	if err != nil {
		// Tear the trace exporter down so we don't leak it; surface
		// any teardown error alongside the meter-init failure.
		shutdownErr := traceShutdown(ctx)
		return nil, errors.Join(
			fmt.Errorf("initialising meter provider: %w", err),
			shutdownErr,
		)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return chainShutdown(traceShutdown, meterShutdown), nil
}

func buildResource(ctx context.Context, serviceName string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version.Semver),
			semconv.ServiceInstanceID(version.CommitSHA),
		),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcessRuntimeName(),
	)
}

func initTracerProvider(
	ctx context.Context,
	cfg config.OtelConfig,
	res *resource.Resource,
) (ShutdownFunc, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithTimeout(cfg.Timeout),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	if err != nil {
		return nil, fmt.Errorf("creating otlp trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

func initMeterProvider(
	ctx context.Context,
	cfg config.OtelConfig,
	res *resource.Resource,
) (ShutdownFunc, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
		otlpmetricgrpc.WithTimeout(cfg.Timeout),
	}
	if cfg.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}

	exporter, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating otlp metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(
			exporter,
			sdkmetric.WithInterval(60*time.Second),
		)),
	)
	otel.SetMeterProvider(mp)

	return mp.Shutdown, nil
}

func chainShutdown(fns ...ShutdownFunc) ShutdownFunc {
	return func(ctx context.Context) error {
		var errs []error
		for _, fn := range fns {
			if err := fn(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}

func noopShutdown(_ context.Context) error { return nil }
