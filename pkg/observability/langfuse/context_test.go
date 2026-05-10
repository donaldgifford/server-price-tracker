package langfuse

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionIDFromContext_DefaultsEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, SessionIDFromContext(context.Background()),
		"missing session id must return empty string so callers can omitempty")
}

func TestWithSessionID_RoundTrips(t *testing.T) {
	t.Parallel()

	ctx := WithSessionID(context.Background(), "tick-2026-05-09T17:53:13Z")
	assert.Equal(t, "tick-2026-05-09T17:53:13Z", SessionIDFromContext(ctx))
}

func TestWithSessionID_EmptyIsNoOp(t *testing.T) {
	t.Parallel()

	// Empty session id must not pollute the context — callers
	// fall back to "no session" rather than write a zero-value entry
	// that future SessionIDFromContext calls would surface as set.
	ctx := WithSessionID(context.Background(), "")
	assert.Empty(t, SessionIDFromContext(ctx))
}

func TestWithSessionID_OverridesPriorValue(t *testing.T) {
	t.Parallel()

	ctx := WithSessionID(context.Background(), "first")
	ctx = WithSessionID(ctx, "second")
	assert.Equal(t, "second", SessionIDFromContext(ctx),
		"nested session scope should override the outer (matches context.WithValue semantics)")
}
