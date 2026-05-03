package extract

import (
	"slices"
	"strings"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// workstationSeriesTokens are substrings that, when present in
// `Series` or `Product Line`, identify a workstation chassis. Lowercase.
var workstationSeriesTokens = []string{
	"thinkstation",
	"z by hp",
	"z-by-hp",
	"hp z",
	"dell precision",
	"precision tower",
	"precision",
	"pro max",
}

// desktopSeriesTokens identify a non-workstation desktop chassis line.
// "pro" alone is too generic — handled separately below with a form-
// factor co-token requirement.
var desktopSeriesTokens = []string{
	"optiplex",
	"thinkcentre",
	"elitedesk",
	"prodesk",
}

// desktopFormFactorTokens identify desktop chassis form factors.
// Used both directly and as a co-token gate for the ambiguous "Pro" line.
var desktopFormFactorTokens = []string{
	"tower",
	"desktop",
	"sff",
	"micro",
	"mini tower",
	"small form factor",
}

// DetectSystemTypeFromSpecifics inspects eBay item specifics and returns
// ComponentWorkstation or ComponentDesktop when a high-confidence match
// is found, or empty string when the LLM should classify. Mirrors the
// IsAccessoryOnly short-circuit shape — caller routes to the bypass when
// the return value is non-empty.
//
// Keys are matched case-insensitively. Matching priority:
//  1. `Most Suitable For: Workstation` is the strongest signal.
//  2. `Series` / `Product Line` containing a workstation token wins
//     before desktop tokens (a "ThinkStation P620" might also have a
//     ThinkCentre-shaped model number).
//  3. `Series` / `Product Line` containing a desktop token.
//  4. Generic `Pro` line with a desktop form factor co-token (avoids
//     false positives on "Dell ProSupport" etc — see Open Question 6).
//  5. Empty string otherwise — defer to the LLM classifier.
func DetectSystemTypeFromSpecifics(specs map[string]string) domain.ComponentType {
	if len(specs) == 0 {
		return ""
	}
	norm := normaliseSpecs(specs)

	if v := norm["most suitable for"]; strings.Contains(v, "workstation") {
		return domain.ComponentWorkstation
	}

	combined := norm["series"] + " " + norm["product line"]
	combined = strings.TrimSpace(combined)

	if combined != "" {
		if containsAny(combined, workstationSeriesTokens) {
			return domain.ComponentWorkstation
		}
		if containsAny(combined, desktopSeriesTokens) {
			return domain.ComponentDesktop
		}
	}

	// Ambiguous "Pro" line — only counts when paired with a desktop
	// form-factor co-token from another item-specifics field.
	if containsToken(combined, "pro") {
		formFactor := norm["form factor"] + " " + norm["type"]
		if containsAny(formFactor, desktopFormFactorTokens) {
			return domain.ComponentDesktop
		}
	}

	return ""
}

// normaliseSpecs lowercases keys and values of an item-specifics map.
// eBay's data is inconsistently cased across listings; the matcher
// works on a normalised view to keep the lookup logic readable.
func normaliseSpecs(specs map[string]string) map[string]string {
	out := make(map[string]string, len(specs))
	for k, v := range specs {
		out[strings.ToLower(strings.TrimSpace(k))] = strings.ToLower(strings.TrimSpace(v))
	}
	return out
}

// containsAny reports whether haystack contains any of needles.
func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// containsToken reports whether haystack contains needle as a whole
// token (separated by whitespace). Avoids "pro" matching "prosupport".
func containsToken(haystack, needle string) bool {
	return slices.Contains(strings.Fields(haystack), needle)
}
