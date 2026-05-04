// Package langfuse provides an in-house HTTP client for Langfuse
// (https://langfuse.com/), the LLM-observability backend used by
// IMPL-0019 to capture every LLMBackend.Generate call as a Langfuse
// generation linked to the active OTel trace.
//
// Three implementations satisfy the Client interface:
//
//   - HTTPClient — talks to a real Langfuse instance via the public
//     REST API, authenticated with the provisioned public+secret keys.
//   - NoopClient — every method returns nil immediately. Used when
//     observability.langfuse.enabled is false so downstream code
//     never has to branch on "is Langfuse configured".
//   - BufferedClient — wraps another Client with a bounded async
//     channel + drain goroutine so transient Langfuse outages don't
//     block the extract path. Drops oldest entries on overflow and
//     emits Prometheus metrics for buffer health.
//
// We use an in-house HTTP client (rather than a third-party SDK) to
// keep our AI/LLM dependency surface uniform with Ollama / Anthropic
// / OpenAICompat backends — see DESIGN-0016 Open Question 1.
package langfuse

import (
	"context"
	"time"
)

// Client is the surface for everything we use Langfuse for. The five
// methods correspond directly to Langfuse REST endpoints we depend on
// across IMPL-0019 phases 3-6 — no broader API coverage is needed.
//
// Methods take their request payload by pointer because GenerationRecord
// and DatasetRun are large enough to flag golangci's hugeParam check;
// pointer semantics also make it natural for callers to construct
// records once and pass them through buffer/decorator layers.
//
// Implementations must be safe for concurrent use by multiple
// goroutines: extract workers, the judge worker, and the alert UI may
// all call into the same Client instance from independent contexts.
type Client interface {
	// LogGeneration records one LLMBackend.Generate call. Phase 3
	// extract decorator drives this; Phase 5 judge worker also.
	LogGeneration(ctx context.Context, gen *GenerationRecord) error

	// Score attaches a numeric label to a trace. Used for
	// extraction_self_confidence (Phase 3), operator_dismissed
	// (Phase 4), and judge_alert_quality (Phase 5).
	Score(ctx context.Context, traceID, name string, value float64, comment string) error

	// CreateTrace creates a top-level trace record. Most extract
	// generations attach to a trace ID derived from the OTel span
	// context, so this is rarely called directly — but the judge
	// worker uses it to anchor its own LLM call to a fresh trace.
	CreateTrace(ctx context.Context, name string, metadata map[string]string) (TraceHandle, error)

	// CreateDatasetItem appends one labelled example to a Langfuse
	// dataset. Used by Phase 6's golden_classifications upload.
	CreateDatasetItem(ctx context.Context, datasetID string, item *DatasetItem) error

	// CreateDatasetRun records the result of running a prompt over a
	// dataset. Used by Phase 6 regression runner to push a
	// classify_prompt:<sha> annotation back to Langfuse.
	CreateDatasetRun(ctx context.Context, run *DatasetRun) error
}

// TraceHandle identifies a created trace; callers attach generations
// and scores via TraceID. The struct is intentionally small — we only
// use the trace_id today; metadata read-back can be added if needed.
type TraceHandle struct {
	TraceID string
}

// GenerationRecord is the payload for one LogGeneration call.
//
// All fields except Name + TraceID + Model are optional from the
// Langfuse API's perspective, but the in-house decorator populates
// every one so the recorded view in the Langfuse UI is consistent
// across LLM backends.
//
// CommitSHA is set by the decorator from internal/version so every
// generation can be grouped/compared by the prompt version that
// produced it (DESIGN-0016 Open Question 10 — best of both).
type GenerationRecord struct {
	TraceID    string
	Name       string // e.g., "classify-llm" / "extract-llm" / "judge-llm"
	Model      string
	Prompt     string
	Completion string
	StartTime  time.Time
	EndTime    time.Time
	Usage      TokenUsage
	CostUSD    float64           // 0 means "let Langfuse compute it from its model rate table"
	Metadata   map[string]string // commit SHA, component_type, etc.
	Level      Level             // DEFAULT for happy path, ERROR for failed parses/validations
	StatusMsg  string            // populated when Level == ERROR
}

// TokenUsage mirrors extract.TokenUsage; pulled into this package so
// the Langfuse client doesn't need to import pkg/extract (would create
// a dependency cycle once the decorator imports langfuse).
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// ModelCost is a per-model rate table entry in USD per million tokens.
//
// Pulled from `observability.langfuse.model_costs` in YAML. Empty map →
// CostUSD on the GenerationRecord stays at 0 and Langfuse falls back to
// its own server-side rate table. Operators only need entries for
// in-house / private models that Langfuse can't price (e.g., Ollama).
type ModelCost struct {
	InputUSDPerMillion  float64 `yaml:"input_usd_per_million"`
	OutputUSDPerMillion float64 `yaml:"output_usd_per_million"`
}

// ComputeCost converts a TokenUsage observation into USD using this
// model's rate. Tiny helper, but it isolates the per-million conversion
// so callers can't accidentally divide by 1_000 instead of 1_000_000.
func (m ModelCost) ComputeCost(usage TokenUsage) float64 {
	const tokensPerMillion = 1_000_000.0
	return float64(usage.InputTokens)/tokensPerMillion*m.InputUSDPerMillion +
		float64(usage.OutputTokens)/tokensPerMillion*m.OutputUSDPerMillion
}

// Level is the Langfuse "level" enum for generation severity.
type Level string

const (
	// LevelDefault is the normal happy-path level.
	LevelDefault Level = "DEFAULT"
	// LevelError marks generations that failed parse or validation.
	// Langfuse UI surfaces these in error filters.
	LevelError Level = "ERROR"
	// LevelWarning marks degraded-but-completed generations.
	LevelWarning Level = "WARNING"
)

// DatasetItem is one labelled example uploaded to a Langfuse dataset.
// Used by Phase 6 to ship the ~100-listing golden classification set
// that the operator-run regression script measures against.
type DatasetItem struct {
	Input          map[string]any
	ExpectedOutput map[string]any
	Metadata       map[string]string
}

// DatasetRun records the outcome of running a prompt or backend over
// the dataset. RunName is a human-friendly tag (e.g., the commit SHA
// that produced the prompt under test); ItemResults captures the
// per-item outcome for traceability.
type DatasetRun struct {
	DatasetID   string
	RunName     string
	Description string
	Metadata    map[string]string
	ItemResults []DatasetRunItem
}

// DatasetRunItem is one row of a DatasetRun: which dataset item was
// evaluated, and the actual output produced.
type DatasetRunItem struct {
	DatasetItemID string
	Output        map[string]any
	TraceID       string // optional, links the item back to its generation trace
}
