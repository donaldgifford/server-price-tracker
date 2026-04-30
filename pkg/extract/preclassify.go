package extract

import (
	"regexp"
	"strings"
)

// accessoryPatterns match titles that describe a server-part accessory rather
// than the host component. A match short-circuits the LLM classifier and
// routes the listing to ComponentOther. See DESIGN-0011.
var accessoryPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bbackplane\b`),
	regexp.MustCompile(`\b(drive\s+)?(caddy|caddies|tray|trays|sled|sleds)\b`),
	regexp.MustCompile(`\brails?\b`),
	regexp.MustCompile(`\bbezels?\b`),
	regexp.MustCompile(`\b(mounting\s+)?brackets?\b`),
	regexp.MustCompile(`\brisers?\b`),
	regexp.MustCompile(`\bheat[\s-]?sinks?\b`),
	regexp.MustCompile(`\bfan\s+(assembly|kit|tray|module)\b`),
	regexp.MustCompile(`\bcable\b`),
	regexp.MustCompile(`\bgpu\s+riser\b`),
}

// primaryComponentPatterns match strong primary-component keywords. When a
// title hits both an accessory and a primary pattern, the LLM gets to decide
// — the accessory keyword is likely incidental ("4U server with rack rails
// included"). See DESIGN-0011.
//
// Form factors (1U, 2U, ...) are included because they're a strong signal
// that the listing is an actual chassis rather than a part for one — they
// route titles like "4U server with rails" to the LLM while leaving
// part-only listings ("POWEREDGE R740xd 24 BAY BACKPLANE") in the
// short-circuit lane.
var primaryComponentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bddr[2345]\b`),
	regexp.MustCompile(`\b(rdimm|udimm|lrdimm|fbdimm|sodimm)\b`),
	regexp.MustCompile(`\b(nvme|sas|sata|scsi)\b`),
	regexp.MustCompile(`\bssd\b`),
	regexp.MustCompile(`\bhdd\b`),
	regexp.MustCompile(`\b(xeon|epyc|opteron|threadripper)\b`),
	regexp.MustCompile(`\b(\d+gb|\d+tb)\b`),
	regexp.MustCompile(`\b\d+u\b`),
}

// IsAccessoryOnly reports whether the title describes a bare server-part
// accessory with no primary-component context. When true, the caller should
// route the listing to ComponentOther without calling the LLM.
//
// The check is conservative: any primary-component keyword in the title
// (DDR4, NVMe, Xeon, capacity markers) defers to the LLM, since the
// accessory keyword is most likely incidental in those cases.
func IsAccessoryOnly(title string) bool {
	lower := strings.ToLower(title)
	if !matchesAny(lower, accessoryPatterns) {
		return false
	}
	if matchesAny(lower, primaryComponentPatterns) {
		return false
	}
	return true
}

func matchesAny(s string, patterns []*regexp.Regexp) bool {
	for _, p := range patterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}
