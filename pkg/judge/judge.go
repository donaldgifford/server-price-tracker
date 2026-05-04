// Package judge implements the LLM-as-judge worker that grades fired
// alerts retrospectively against an operator-curated rubric (see
// IMPL-0019 Phase 5 / DESIGN-0016).
//
// Two distinct surfaces:
//
//   - Judge — the single-evaluation contract. Takes a structured
//     AlertContext and returns a Verdict. Implementations: LLMJudge
//     (production, calls an LLMBackend) and any test fakes.
//   - Worker — the async cron-driven loop that pulls candidate alerts
//     from the store, fans them out through Judge, persists the
//     Verdicts back to Postgres, and writes a Score on the
//     corresponding Langfuse trace. Lives in a separate file so the
//     orchestration stays separable from the prompt logic.
//
// The package intentionally has no Postgres or Langfuse imports —
// callers wire those via interface arguments so unit tests can run
// without external infrastructure.
package judge

import (
	"context"
	"errors"
	"time"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// Judge produces a quality verdict for one fired alert. Implementations
// must be safe for concurrent use — the worker may fan multiple alerts
// out in parallel within a single tick.
//
// AlertContext is passed by pointer so the (~200-byte) struct stays
// off the call stack — the receiver must treat it as read-only.
type Judge interface {
	EvaluateAlert(ctx context.Context, ac *AlertContext) (Verdict, error)
}

// AlertContext is the structured input the judge prompt is rendered
// from. Pulled out as a struct (rather than a domain.Alert pointer)
// because the judge needs derived fields the alert table doesn't carry
// — baseline percentiles, watch-relative threshold, etc. — and using a
// dedicated DTO keeps the prompt input stable across schema changes.
type AlertContext struct {
	AlertID       string
	WatchName     string
	ComponentType domain.ComponentType
	ListingTitle  string
	Condition     domain.Condition
	PriceUSD      float64
	BaselineP25   float64
	BaselineP50   float64
	BaselineP75   float64
	SampleSize    int
	Score         int
	Threshold     int
	Reasons       []string // ScoreBreakdown.Reasons — why the scorer said yes
	TraceID       string   // empty when alert predates trace propagation
	CreatedAt     time.Time
}

// Verdict is the judge's output. Score is 0.0-1.0 where 1.0 means
// "this alert was almost certainly worth notifying on" and 0.0 means
// "the operator is going to dismiss this immediately." Reason is a
// short free-text justification surfaced both in the alert review UI
// (tooltip) and in Langfuse (Score.comment).
type Verdict struct {
	Score   float64
	Reason  string
	Tokens  TokenUsage
	Model   string
	CostUSD float64
}

// TokenUsage mirrors langfuse.TokenUsage / extract.TokenUsage; pulled
// in here so the judge package can remain free of langfuse imports
// (cyclic — langfuse needs to call judge from the buffered client
// during dataset runs in Phase 6).
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
}

// ErrJudgeBudgetExhausted is returned when the worker hits the
// configured daily budget cap before completing the batch. Surfaced
// distinctly so the caller (cron / HTTP backfill) can emit the right
// metric counter and skip cleanly without logging an actual error.
var ErrJudgeBudgetExhausted = errors.New("judge daily budget exhausted")
