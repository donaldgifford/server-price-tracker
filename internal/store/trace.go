package store

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// traceIDFromContext returns the W3C trace ID (32-char hex) of the active
// span on ctx, or "" when no valid span is in context. Used by writes
// that persist trace IDs onto domain rows (extraction_queue.trace_id,
// alerts.trace_id) so the alert review UI can deep-link back to the
// originating trace (DESIGN-0016 / IMPL-0019 Phase 2).
//
// Safe to call regardless of whether OTel is enabled: when disabled,
// SpanFromContext returns a no-op span whose SpanContext is invalid,
// and we return "" — the SQL queries use NULLIF to coerce that to
// NULL on the column.
func traceIDFromContext(ctx context.Context) string {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
