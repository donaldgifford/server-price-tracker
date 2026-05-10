package langfuse

import "context"

// sessionIDKey is the unexported context key for the Langfuse session
// ID. Using a private struct as the key (rather than a string) avoids
// collisions with other packages that store data in the same context.
type sessionIDKey struct{}

// WithSessionID returns a copy of ctx carrying id as the Langfuse
// session identifier. Downstream LangfuseBackend reads it via
// SessionIDFromContext and attaches it to the generation event body
// so all generations produced under the same logical run group under
// one session in the Langfuse UI.
//
// Empty id is a no-op — callers that don't have a session ID should
// not pollute the context.
func WithSessionID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDKey{}, id)
}

// SessionIDFromContext returns the Langfuse session ID stored on ctx,
// or "" when none is set. The empty-string return is intentional: it
// lets callers `omitempty` the field rather than branching on
// presence.
func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(sessionIDKey{}).(string); ok {
		return v
	}
	return ""
}
