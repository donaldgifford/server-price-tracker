package judge

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
	"unicode"
)

// promptFS embeds the judge prompt template so the binary ships
// self-contained. Examples are loaded from the same package directory
// at build time — see examples.go for the cold-start workflow.
//
//go:embed judge_prompt.tmpl
var promptFS embed.FS

// maxUntrustedTitleLen caps eBay seller-controlled title length before
// it lands in the prompt. eBay caps at 80, but defense-in-depth against
// prompt injection (a 4KB "Ignore previous instructions…" payload) —
// see INV-0001 MEDIUM-5.
const maxUntrustedTitleLen = 200

// renderPrompt assembles the per-alert judge prompt: rubric +
// few-shot examples + the alert under evaluation. The template lives
// next to this file so domain experts can edit it without touching Go.
//
// Untrusted seller-controlled fields (ListingTitle, score Reasons) are
// run through sanitizeUntrusted before render and the alert block is
// wrapped in <<<UNTRUSTED>>> delimiters so the model is told to treat
// the content as data, not instructions.
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

	safe := *ac
	safe.ListingTitle = sanitizeUntrusted(safe.ListingTitle, maxUntrustedTitleLen)
	if len(safe.Reasons) > 0 {
		cleaned := make([]string, len(safe.Reasons))
		for i, r := range safe.Reasons {
			cleaned[i] = sanitizeUntrusted(r, maxUntrustedTitleLen)
		}
		safe.Reasons = cleaned
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		Alert    AlertContext
		Examples []Example
	}{
		Alert:    safe,
		Examples: examples,
	}); err != nil {
		return "", fmt.Errorf("executing judge prompt template: %w", err)
	}
	return buf.String(), nil
}

// sanitizeUntrusted strips control characters (so newline-injected
// "system: ..." pseudo-prompts can't break out of a delimited block)
// and truncates to maxLen runes. Tab and standard whitespace stay so
// legitimate titles render naturally.
func sanitizeUntrusted(s string, maxLen int) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == ' ':
			b.WriteRune(r)
		case unicode.IsControl(r):
			// Newlines, NULs, escape sequences — drop.
			continue
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len([]rune(out)) > maxLen {
		runes := []rune(out)
		out = string(runes[:maxLen]) + "…"
	}
	return out
}
