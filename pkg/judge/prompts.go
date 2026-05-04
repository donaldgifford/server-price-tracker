package judge

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

// promptFS embeds the judge prompt template so the binary ships
// self-contained. Examples are loaded from the same package directory
// at build time — see examples.go for the cold-start workflow.
//
//go:embed judge_prompt.tmpl
var promptFS embed.FS

// renderPrompt assembles the per-alert judge prompt: rubric +
// few-shot examples + the alert under evaluation. The template lives
// next to this file so domain experts can edit it without touching Go.
//
// Returns (prompt, error). The error is non-nil only on internal
// template malformation — callers should treat it as a panic-class
// bug, not a user-facing failure.
func renderPrompt(ac *AlertContext, examples []Example) (string, error) {
	tmplBytes, err := promptFS.ReadFile("judge_prompt.tmpl")
	if err != nil {
		return "", fmt.Errorf("reading embedded judge prompt template: %w", err)
	}
	tmpl, err := template.New("judge").Parse(string(tmplBytes))
	if err != nil {
		return "", fmt.Errorf("parsing judge prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		Alert    AlertContext
		Examples []Example
	}{
		Alert:    *ac,
		Examples: examples,
	}); err != nil {
		return "", fmt.Errorf("executing judge prompt template: %w", err)
	}
	return buf.String(), nil
}
