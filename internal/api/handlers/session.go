package handlers

import (
	"context"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
)

// withRequestSession seeds a fresh Langfuse session ID for handlers
// that drive LLM-bearing pipelines (extract, ingest). The ID is set
// on ctx so the LangfuseBackend picks it up via SessionIDFromContext,
// and attached as `langfuse.session.id` on the active OTel span so
// the OTel-derived trace gets the same session attribution at ingest.
//
// Per-request scope means each manual API trigger surfaces as its own
// session in the Langfuse UI — useful for ad-hoc /api/v1/extract test
// calls that shouldn't bleed into a scheduled-tick session.
func withRequestSession(ctx context.Context) context.Context {
	sessionID := uuid.NewString()
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("langfuse.session.id", sessionID))
	return langfuse.WithSessionID(ctx, sessionID)
}
