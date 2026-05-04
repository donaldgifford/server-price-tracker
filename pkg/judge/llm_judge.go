package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
)

// LLMJudge evaluates alerts via an LLMBackend. The backend is the same
// abstraction used by the extract pipeline so the judge can swap
// between Anthropic / Ollama / OpenAI-compat without bespoke wiring.
//
// Construction-time wiring:
//   - backend: any extract.LLMBackend, typically the same one extract
//     uses so a Haiku upgrade in extract config auto-applies here.
//   - examples: operator-curated few-shot set; pass nil for the empty
//     stub during bootstrap.
//   - costs: optional langfuse.ModelCost table for budget tracking;
//     judge cost is reported in Verdict.CostUSD.
type LLMJudge struct {
	backend  extract.LLMBackend
	examples []Example
	costs    map[string]langfuse.ModelCost
}

// LLMJudgeOption configures an LLMJudge.
type LLMJudgeOption func(*LLMJudge)

// WithExamples replaces the embedded examples with caller-supplied
// ones. Used by tests and by the cold-start bootstrap tool to evaluate
// new candidate examples before committing them.
func WithExamples(examples []Example) LLMJudgeOption {
	return func(j *LLMJudge) {
		j.examples = examples
	}
}

// WithModelCosts wires a per-model rate table so Verdict.CostUSD is
// computed locally. Empty map → CostUSD stays 0 and the worker's
// daily-budget query treats this judge as free (operators only need
// rates for non-Anthropic / non-OpenAI backends).
func WithModelCosts(costs map[string]langfuse.ModelCost) LLMJudgeOption {
	return func(j *LLMJudge) {
		j.costs = costs
	}
}

// NewLLMJudge constructs a Judge backed by an LLMBackend. When
// examples are nil the embedded examples.json is loaded — empty slice
// is a valid state during bootstrap; the prompt renders cleanly with
// zero few-shot examples (just the rubric).
func NewLLMJudge(backend extract.LLMBackend, opts ...LLMJudgeOption) (*LLMJudge, error) {
	embedded, err := LoadExamples()
	if err != nil {
		return nil, fmt.Errorf("loading embedded judge examples: %w", err)
	}
	j := &LLMJudge{backend: backend, examples: embedded}
	for _, opt := range opts {
		opt(j)
	}
	return j, nil
}

// EvaluateAlert renders the prompt, calls the LLM, parses the JSON
// verdict, and returns it. Errors at any stage propagate so the
// worker can decide to retry / skip the alert.
//
// Cost is computed from the response Usage and the configured model
// cost table; an unknown model leaves Verdict.CostUSD at 0 (matches
// the LangfuseBackend fallback semantics).
func (j *LLMJudge) EvaluateAlert(ctx context.Context, ac *AlertContext) (Verdict, error) {
	prompt, err := renderPrompt(ac, j.examples)
	if err != nil {
		return Verdict{}, fmt.Errorf("rendering judge prompt: %w", err)
	}

	resp, err := j.backend.Generate(ctx, extract.GenerateRequest{
		Prompt:      prompt,
		Format:      extract.FormatJSON,
		MaxTokens:   256,
		Temperature: 0.1,
	})
	if err != nil {
		return Verdict{}, fmt.Errorf("calling judge LLM: %w", err)
	}

	parsed, err := parseVerdict(resp.Content)
	if err != nil {
		return Verdict{}, fmt.Errorf("parsing judge response: %w (raw=%q)", err, resp.Content)
	}

	v := Verdict{
		Score:  parsed.Score,
		Reason: parsed.Reason,
		Tokens: TokenUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
		Model: resp.Model,
	}
	if cost, ok := j.costs[resp.Model]; ok {
		v.CostUSD = cost.ComputeCost(langfuse.TokenUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		})
	}
	return v, nil
}

// verdictPayload is the wire shape of the judge LLM's response. Kept
// minimal — we only persist score and reason; everything else is
// derived from the Generate response.
type verdictPayload struct {
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// parseVerdict de-fences the response (Anthropic wraps JSON in
// ```json``` despite explicit instructions otherwise — same workaround
// extract uses) and decodes the verdict shape. Range-checks Score so
// a hallucinated 1.5 doesn't poison the budget calculation downstream.
func parseVerdict(raw string) (verdictPayload, error) {
	s := stripJSONFences(raw)
	var p verdictPayload
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return verdictPayload{}, fmt.Errorf("unmarshal verdict JSON: %w", err)
	}
	if p.Score < 0 || p.Score > 1 {
		return verdictPayload{}, fmt.Errorf("verdict score %.4f out of range [0.0, 1.0]", p.Score)
	}
	return p, nil
}

// stripJSONFences mirrors extract.stripJSONFences — duplicated rather
// than exported because the extract one is a private helper and we
// don't want pkg/judge depending on extract's internals beyond the
// LLMBackend interface.
func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
