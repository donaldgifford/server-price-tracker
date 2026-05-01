package extract

import (
	"regexp"
	"strings"
)

// Server tier constants — appended to the server product key so
// barebone shells, partially-configured systems, and fully-loaded
// servers each land in their own price baseline. See IMPL-0016 Phase 6.
const (
	ServerTierBarebone   = "barebone"
	ServerTierPartial    = "partial"
	ServerTierConfigured = "configured"
)

// barebonePatterns match explicit "this server is sold without
// CPU/RAM/HDD" markers in the title. Any single match is sufficient —
// sellers usually combine them ("No CPU / No RAM / No HDDs"), but one
// strong signal is enough to bucket separately.
var barebonePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bbar(e)?bone\b`),
	regexp.MustCompile(`\b(no|w/o|without|no/?)\s*(cpu|ram|memory|hdd?s?|drives?|os)\b`),
	regexp.MustCompile(`\bcto\b`), // configure-to-order — ships bare
}

// cpuPresentPatterns match titles that name a specific CPU. We require
// model granularity (e.g., "Xeon Gold 5118") rather than a bare family
// name because "Xeon" alone appears in titles for CPU-shaped accessories
// (heatsinks, brackets) — only an actual model number proves the
// listing has a CPU installed.
var cpuPresentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(gold|silver|platinum|bronze)\s+\d{4}\b`),
	regexp.MustCompile(`\bxeon\s+(e[357]|d|w|gold|silver|platinum|bronze)[-\s]*\d`),
	regexp.MustCompile(`\bepyc\s+\d{4}`),
	regexp.MustCompile(`\bcore\s+i[3579]\b`),
}

// ramPresentPatterns match capacity markers paired with RAM context.
// `\d+gb` alone would false-positive on drive capacities ("1TB"), so
// we anchor on RAM-specific tokens (DDRn, RDIMM, ECC, "RAM", "memory").
var ramPresentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bddr[2345]\b`),
	regexp.MustCompile(`\b(rdimm|udimm|lrdimm|fbdimm|sodimm)\b`),
	regexp.MustCompile(`\b\d+\s*gb\s+(ram|memory|ecc|reg|rdimm)\b`),
	regexp.MustCompile(`\b(ram|memory)[:\s]+\d+\s*gb\b`),
}

// DetectServerTier inspects the title and returns one of barebone /
// partial / configured. Conservative by default — when the title gives
// no clear signal either way, returns ServerTierPartial so the row
// doesn't pollute either extreme baseline.
//
// Order matters: barebone signals override CPU/RAM presence (a listing
// can mention "Xeon Gold 5118 socket — NO CPU INCLUDED" and still be a
// shell).
func DetectServerTier(title string) string {
	lower := strings.ToLower(title)

	if matchesAny(lower, barebonePatterns) {
		return ServerTierBarebone
	}

	hasCPU := matchesAny(lower, cpuPresentPatterns)
	hasRAM := matchesAny(lower, ramPresentPatterns)

	if hasCPU && hasRAM {
		return ServerTierConfigured
	}
	return ServerTierPartial
}
