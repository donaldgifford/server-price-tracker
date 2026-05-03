package extract

import (
	"regexp"
	"strings"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// systemVendorAliases maps common LLM spellings of workstation/desktop
// vendors to a canonical lowercase token. Keys are lowercase + trimmed
// before lookup; unknown values fall through unchanged so we don't
// corrupt vendors we haven't seen yet.
var systemVendorAliases = map[string]string{
	"dell":                       "dell",
	"dell inc.":                  "dell",
	"dell inc":                   "dell",
	"dell technologies":          "dell",
	"hp":                         "hp",
	"hewlett-packard":            "hp",
	"hewlett packard":            "hp",
	"hewlett packard enterprise": "hp",
	"hpe":                        "hp",
	"lenovo":                     "lenovo",
	"ibm":                        "lenovo",
	"ibm/lenovo":                 "lenovo",
	"apple":                      "apple",
}

// systemLineAliases collapses spelling variants of a system product line
// to a canonical form. Per Open Question 5, "Pro Max" is kept distinct
// from "precision" — buyers cross-shop, but Dell catalogs them
// separately and the post-rebrand pricing curve should remain separable.
var systemLineAliases = map[string]string{
	"precision":       "precision",
	"dell precision":  "precision",
	"precision tower": "precision",
	"precision-tower": "precision",
	"thinkstation":    "thinkstation",
	"think station":   "thinkstation",
	"z by hp":         "z-by-hp",
	"z-by-hp":         "z-by-hp",
	"hp z":            "z-by-hp",
	"hp z-series":     "z-by-hp",
	"z series":        "z-by-hp",
	"z-series":        "z-by-hp",
	"pro max":         "pro-max",
	"pro-max":         "pro-max",
	"promax":          "pro-max",
	"optiplex":        "optiplex",
	"dell optiplex":   "optiplex",
	"thinkcentre":     "thinkcentre",
	"think centre":    "thinkcentre",
	"think center":    "thinkcentre",
	"elitedesk":       "elitedesk",
	"elite desk":      "elitedesk",
	"hp elitedesk":    "elitedesk",
	"prodesk":         "prodesk",
	"pro desk":        "prodesk",
	"hp prodesk":      "prodesk",
	"pro":             "pro",
	"dell pro":        "pro",
}

// systemLineInferenceRules infer the canonical line from a model SKU
// when the LLM left the line field blank. Patterns operate on the
// canonical (lowercase, vendor-prefix-stripped) model produced by
// canonicalizeSystemModel. Conservative — ambiguous numeric SKUs
// don't match here so we leave line empty rather than mis-infer.
var systemLineInferenceRules = []struct {
	pattern *regexp.Regexp
	line    string
}{
	{regexp.MustCompile(`^t\d{4}$`), "precision"},
	{regexp.MustCompile(`^p\d{3}$`), "thinkstation"},
	{regexp.MustCompile(`^z\d+\s+g\d+$`), "z-by-hp"},
	{regexp.MustCompile(`^z\d+g\d+$`), "z-by-hp"},
	{regexp.MustCompile(`^m\d{3}[a-z]?$`), "thinkcentre"},
	{regexp.MustCompile(`^optiplex`), "optiplex"},
	{regexp.MustCompile(`^elitedesk`), "elitedesk"},
	{regexp.MustCompile(`^prodesk`), "prodesk"},
}

// systemBrandPrefixRe strips a leading vendor prefix from the model
// field. The LLM frequently embeds the brand into the model field
// ("Dell T7920", "HP Z8 G4") despite vendor being its own column.
var systemBrandPrefixRe = regexp.MustCompile(`^(dell|hp|hpe|lenovo|ibm)[_\s-]+`)

// CanonicalizeSystemVendor collapses common spellings of a workstation
// or desktop vendor to a canonical lowercase token.
func CanonicalizeSystemVendor(s string) string {
	key := strings.ToLower(strings.TrimSpace(s))
	if key == "" {
		return ""
	}
	if canonical, ok := systemVendorAliases[key]; ok {
		return canonical
	}
	return strings.ReplaceAll(key, " ", "-")
}

// CanonicalizeSystemLine collapses common spellings of a workstation /
// desktop product line to a canonical lowercase token.
func CanonicalizeSystemLine(s string) string {
	key := strings.ToLower(strings.TrimSpace(s))
	if key == "" {
		return ""
	}
	if canonical, ok := systemLineAliases[key]; ok {
		return canonical
	}
	return strings.ReplaceAll(key, " ", "-")
}

// CanonicalizeSystemModel collapses common spellings of a workstation /
// desktop model SKU to a canonical lowercase token.
//
// Mutations:
//   - lowercase + trim
//   - strip leading vendor prefix (dell_t7920 → t7920, "HP Z8 G4" → "z8 g4")
func CanonicalizeSystemModel(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return ""
	}
	return systemBrandPrefixRe.ReplaceAllString(lower, "")
}

// InferSystemLineFromModel returns the canonical line token for a
// model SKU when the prefix matches a high-confidence pattern.
// Returns "" for unknown / ambiguous models.
func InferSystemLineFromModel(model string) string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return ""
	}
	for _, rule := range systemLineInferenceRules {
		if rule.pattern.MatchString(trimmed) {
			return rule.line
		}
	}
	return ""
}

// NormalizeSystemExtraction repairs common LLM mistakes for workstation
// and desktop attributes before validation. Mutates attrs in place:
//
//  1. Vendor canonicalisation — collapse spelling variants to lowercase
//     canonical tokens (Dell Inc. → dell, Hewlett-Packard → hp).
//  2. Model canonicalisation — strip leading vendor prefix and
//     lowercase ("Dell T7920" → t7920).
//  3. Line resolution — if LLM gave a line, canonicalise it; otherwise
//     try to infer from the canonical model.
//
// componentType is accepted for future per-type divergence (e.g.,
// desktop-specific normalisations) but currently both share the same
// pipeline — Open Question 4.
func NormalizeSystemExtraction(componentType domain.ComponentType, attrs map[string]any) {
	_ = componentType // reserved for future per-type divergence
	canonicalizeSystemVendorInPlace(attrs)
	canonicalizeSystemModelInPlace(attrs)
	resolveSystemLine(attrs)
}

func canonicalizeSystemVendorInPlace(attrs map[string]any) {
	vendor, ok := attrString(attrs, "vendor")
	if !ok {
		return
	}
	if canonical := CanonicalizeSystemVendor(vendor); canonical != "" {
		attrs["vendor"] = canonical
	}
}

func canonicalizeSystemModelInPlace(attrs map[string]any) {
	model, ok := attrString(attrs, "model")
	if !ok {
		return
	}
	if canonical := CanonicalizeSystemModel(model); canonical != "" {
		attrs["model"] = canonical
	}
}

// resolveSystemLine canonicalises the LLM-supplied line when present,
// or infers it from the canonical model SKU when the LLM left it empty.
// Inference is conservative — unknown SKUs leave the field blank so the
// product key gets an honest "unknown" segment.
func resolveSystemLine(attrs map[string]any) {
	if line, ok := attrString(attrs, "line"); ok && line != "" {
		if canonical := CanonicalizeSystemLine(line); canonical != "" {
			attrs["line"] = canonical
			return
		}
	}

	model, _ := attrString(attrs, "model")
	if inferred := InferSystemLineFromModel(model); inferred != "" {
		attrs["line"] = inferred
	}
}
