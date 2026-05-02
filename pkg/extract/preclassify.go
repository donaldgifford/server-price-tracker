package extract

import (
	"regexp"
	"strings"
)

// compoundAccessoryPatterns capture accessory-as-noun phrases like "SAS
// cable" or "NVMe SSD backplane" where a technology adjective directly
// modifies an accessory noun. These override primaryComponentPatterns:
// when the accessory keyword IS the product being sold, the technology
// word is just describing it, not signalling a host component.
//
// Discovered post-deploy: a "Dell ... Poweredge R740XD ... SAS Cable"
// listing was deferring to the LLM (because "sas" is a primary keyword)
// and the LLM was picking `server` from the strong product-line token.
var compoundAccessoryPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(sas|sata|nvme|m\.?2|pcie|usb|power|fan)\s+(cables?|caddy|caddies|tray|trays|sled|sleds|backplane|riser|bezel|bracket)\b`),
	regexp.MustCompile(`\b(nvme|sas|sata)\s+ssd\s+(backplane|tray|caddy)\b`),
}

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
	// Server-as-noun signals — strong indicators that the listing IS the
	// server rather than a part for one. Keep these narrow enough to not
	// match accessory titles ("server backplane K2Y8N7" must NOT match).
	regexp.MustCompile(`\bbar(e)?bone\b`),                           // "barebone" or the "barbone" misspelling
	regexp.MustCompile(`\bchassis\b`),                               // "Server Chassis"
	regexp.MustCompile(`\b(gold|silver|platinum|bronze)\s+\d{4}\b`), // Xeon Gold 5118, Silver 4110, ...
	regexp.MustCompile(`\bidrac\b`),                                 // Dell management controller
	// GPU brand/family tokens (DESIGN-0012) — a real GPU paired with an
	// accessory keyword ("Tesla P40 + heatsink") should defer to the LLM
	// rather than short-circuit to "other".
	regexp.MustCompile(`\b(tesla|quadro|rtx\s+a\d+|a100|h100|l40|mi\d{3}|radeon\s+pro)\b`),
}

// IsAccessoryOnly reports whether the title describes a bare server-part
// accessory with no primary-component context. When true, the caller should
// route the listing to ComponentOther without calling the LLM.
//
// Decision order:
//  1. compoundAccessoryPatterns — "SAS cable", "NVMe SSD backplane", etc.
//     A technology adjective directly modifying an accessory noun is always
//     an accessory regardless of what other keywords appear in the title.
//  2. accessoryPatterns — bare accessory keywords (backplane, caddy, …).
//  3. primaryComponentPatterns — if any primary keyword appears alongside
//     a bare accessory keyword, defer to the LLM (the accessory keyword is
//     most likely incidental).
func IsAccessoryOnly(title string) bool {
	lower := strings.ToLower(title)
	if matchesAny(lower, compoundAccessoryPatterns) {
		return true
	}
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
