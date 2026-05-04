package engine

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// engineMeterName is the OTel meter registered for the engine package.
// Returns the global no-op meter when observability is disabled.
const engineMeterName = "github.com/donaldgifford/server-price-tracker/internal/engine"

// alertEvalDurationHistogram is the lazily-registered OTel histogram
// recording the wall-clock duration of evaluateAlert calls (per
// listing × watch). Lazy registration is needed because
// observability.Init runs at startup and may not have set the global
// MeterProvider when this package is imported.
var alertEvalDurationHistogram = sync.OnceValue(func() metric.Float64Histogram {
	h, err := otel.Meter(engineMeterName).Float64Histogram(
		"spt.alert.eval.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Wall-clock duration of internal/engine.evaluateAlert calls."),
	)
	if err != nil {
		nh, nerr := noop.NewMeterProvider().Meter(engineMeterName).Float64Histogram("spt.alert.eval.duration")
		if nerr != nil {
			panic(fmt.Sprintf("noop histogram registration failed: %v", nerr))
		}
		return nh
	}
	return h
})

// recordAlertEvalDuration records `seconds` on the lazy histogram. Safe
// to call when OTel is disabled — the no-op histogram swallows the
// observation.
func recordAlertEvalDuration(ctx context.Context, seconds float64, attrs ...metric.RecordOption) {
	alertEvalDurationHistogram().Record(ctx, seconds, attrs...)
}
