package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
)

// withRequestSession seeds a fresh Langfuse session ID on ctx for
// handlers that drive LLM-bearing pipelines (extract, ingest, judge).
// Downstream entry-point spans (extractor's classify_and_extract,
// judge worker's tick) read it via langfuse.SessionIDFromContext and
// stamp the OTel attribute on themselves — that's where Langfuse's
// OTel ingest picks it up, since the API handler runs without an
// active OTel HTTP middleware span.
//
// Per-request scope means each manual API trigger surfaces as its own
// session in the Langfuse UI — useful for ad-hoc /api/v1/extract test
// calls that shouldn't bleed into a scheduled-tick session.
func withRequestSession(ctx context.Context) context.Context {
	return langfuse.WithSessionID(ctx, uuid.NewString())
}
