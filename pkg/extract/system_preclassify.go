package extract

import (
	"regexp"
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

// workstationTitlePrimaries are strong workstation chassis tokens used
// for title-based pre-classification. More conservative than the
// preclassify primaryComponentPatterns workstation list — every entry
// here is unambiguous (no "workstation" keyword alone, no isolated
// "pro max" without "dell").
var workstationTitlePrimaries = []*regexp.Regexp{
	regexp.MustCompile(`\bprecision\s+t?\d{4}\b`),
	regexp.MustCompile(`\bdell\s+pro\s+max\b`),
	regexp.MustCompile(`\bthinkstation\b`),
	regexp.MustCompile(`\bhp\s+z\d\b`),
}

// desktopTitlePrimaries are strong desktop chassis tokens used for
// title-based pre-classification.
var desktopTitlePrimaries = []*regexp.Regexp{
	regexp.MustCompile(`\boptiplex\b`),
	regexp.MustCompile(`\belitedesk\b`),
	regexp.MustCompile(`\bthinkcentre\b`),
	regexp.MustCompile(`\bprodesk\b`),
}

// systemCompletenessSignals indicate that a listing describes a
// complete computer system rather than a part. The title-based
// pre-class only fires when at least one signal matches alongside
// a chassis primary — this prevents "ThinkStation P920 motherboard"
// or "HP Z8 power cable" from short-circuiting to workstation when
// they're really part listings.
var systemCompletenessSignals = []*regexp.Regexp{
	regexp.MustCompile(`\b\d+\s*cores?\b`),                    // "40 Core", "12 cores"
	regexp.MustCompile(`\b\d+\.\d+\s*ghz\b`),                  // 2.40GHz
	regexp.MustCompile(`\bgold\s+\d{4}\b`),                    // Gold 6148
	regexp.MustCompile(`\bsilver\s+\d{4}\b`),                  // Silver 4110
	regexp.MustCompile(`\bplatinum\s+\d{4}\b`),                // Platinum 8160
	regexp.MustCompile(`\b(xeon|epyc|threadripper)\b`),        // CPU family
	regexp.MustCompile(`\bi[3579][\s-]?\d{4,5}[a-z]?\b`),      // i5-6500, i7 10700T
	regexp.MustCompile(`\bryzen\s+\d\b`),                      // Ryzen 5
	regexp.MustCompile(`\bwin(dows)?\s*1[01]\b`),              // Win10, Win11, Windows 10
	regexp.MustCompile(`\bwin1[01]\b`),                        // win11
	regexp.MustCompile(`\b\d+gb\s+(ram|ddr[345]|memory)\b`),   // 256GB RAM
	regexp.MustCompile(`\b\d+gb\s+(ssd|hdd|nvme|m\.?2)\b`),    // 512GB SSD
	regexp.MustCompile(`\b\d+\s*tb\s+(ssd|hdd|nvme|m\.?2)\b`), // 1TB NVMe
	regexp.MustCompile(`\b(ddr[345])\s+\d+gb\b`),              // DDR4 8GB
}

// DetectSystemTypeFromTitle short-circuits to workstation/desktop when
// the title combines a strong chassis token with at least one system-
// completeness signal (CPU model, RAM amount, OS, core count). Returns
// "" when only the chassis token is present — defer to the LLM (or to
// IsAccessoryOnly when the title is part-shaped).
//
// Why a separate-from-specifics title detector: smoke testing on the
// dev image surfaced two failure modes that `DetectSystemTypeFromSpecifics`
// missed:
//
//  1. The LLM classifier inconsistently routes "ThinkStation P920 ...
//     Gold 6148 256GB" to `server` instead of `workstation`. Item
//     specifics didn't always include `Series: ThinkStation` so the
//     specifics hook didn't fire either.
//  2. The compoundAccessoryPatterns "power cable" rule short-circuits
//     "EliteDesk 800 ... + Power Cable" to ComponentOther before the
//     LLM ever sees it.
//
// A title-based detector that runs before IsAccessoryOnly fixes both:
// strong chassis token + system signal is unambiguous enough to skip
// the LLM AND override the accessory short-circuit.
func DetectSystemTypeFromTitle(title string) domain.ComponentType {
	lower := strings.ToLower(title)
	if !matchesAny(lower, systemCompletenessSignals) {
		return ""
	}
	if matchesAny(lower, workstationTitlePrimaries) {
		return domain.ComponentWorkstation
	}
	if matchesAny(lower, desktopTitlePrimaries) {
		return domain.ComponentDesktop
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
