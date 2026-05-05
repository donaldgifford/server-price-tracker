package judge

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// examplesJSON is the operator-curated few-shot example set built by
// `tools/judge-bootstrap`. Hardcoded for v1 (DESIGN-0016 Open Q9 — we
// keep examples in-tree so they're versioned alongside the prompt and
// the regression set). v2 may switch to Langfuse-fetched examples.
//
//go:embed examples.json
var examplesJSON []byte

// Example is one labelled alert used in the few-shot prompt. Label is
// the human-readable bucket ("deal" / "noise" / "edge") so the prompt
// renderer can group examples by quality category.
type Example struct {
	Label   string       `json:"label"`
	Alert   AlertContext `json:"alert"`
	Verdict Verdict      `json:"verdict"`
}

// LoadExamples returns the embedded operator-curated examples. Caller
// pays the JSON-decode cost once at startup; the slice is then read-only
// and shared across goroutines.
//
// Returns an empty slice (not nil, not error) when the embedded file is
// the zero-row stub — operators bootstrap their first 30 labels by
// running tools/judge-bootstrap, which overwrites this file in place.
func LoadExamples() ([]Example, error) {
	var examples []Example
	if len(examplesJSON) == 0 {
		return examples, nil
	}
	if err := json.Unmarshal(examplesJSON, &examples); err != nil {
		return nil, fmt.Errorf("parsing embedded judge examples: %w", err)
	}
	return examples, nil
}
