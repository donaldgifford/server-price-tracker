package extract

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// extractorMeterName is the OTel meter registered for the extract
// pipeline. Returns the global no-op meter when observability is
// disabled — calling Record on a no-op histogram is a no-op, so callers
// stay branch-free.
const extractorMeterName = "github.com/donaldgifford/server-price-tracker/pkg/extract"

// extractionDurationHistogram is the lazily-registered OTel histogram
// recording the wall-clock duration of ClassifyAndExtract calls. Lives
// in OTel-meter-land (separate from the Prometheus
// `spt_extraction_duration_seconds` histogram under
// `internal/metrics`) so dashboards backed by the OTel pipeline
// (Tempo / Clickhouse-derived metrics) can see latency without scraping
// Prometheus. Registration is lazy because observability.Init runs at
// startup and may not have set the global MeterProvider when the
// package is imported.
var extractionDurationHistogram = sync.OnceValue(func() metric.Float64Histogram {
	h, err := otel.Meter(extractorMeterName).Float64Histogram(
		"spt.extraction.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Wall-clock duration of pkg/extract.ClassifyAndExtract calls."),
	)
	if err != nil {
		// Float64Histogram only errors on validation problems (e.g., an
		// empty instrument name) — our name is a hardcoded constant, so
		// the no-op fallback is defensive, not load-bearing. The noop
		// constructor itself cannot fail.
		nh, nerr := noop.NewMeterProvider().Meter(extractorMeterName).Float64Histogram("spt.extraction.duration")
		if nerr != nil {
			panic(fmt.Sprintf("noop histogram registration failed: %v", nerr))
		}
		return nh
	}
	return h
})

// recordExtractionDuration records `seconds` on the lazy histogram. Safe
// to call when OTel is disabled — the no-op histogram swallows the
// observation.
func recordExtractionDuration(ctx context.Context, seconds float64, attrs ...metric.RecordOption) {
	extractionDurationHistogram().Record(ctx, seconds, attrs...)
}
