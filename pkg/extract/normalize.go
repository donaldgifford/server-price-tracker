package extract

import (
	"slices"
	"strings"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// Placeholder strings the LLM sometimes returns for optional enum fields
// instead of null. Compared case-insensitively.
var placeholderValues = []string{"n/a", "na", "unknown", "none", "null", "not specified", "not applicable"}

// optionalEnumFields lists string fields that are optional enums and should
// have placeholder values stripped (set to null) before validation.
var optionalEnumFields = []string{"form_factor", "type", "interface", "port_type"}

// NormalizeExtraction runs all pre-validation cleanups against the raw LLM
// attribute map. It mutates attrs in place. Run this before
// ValidateExtraction so the validator sees the cleaned-up data.
func NormalizeExtraction(componentType domain.ComponentType, title string, attrs map[string]any) {
	stripPlaceholderEnums(attrs)
	defaultConfidence(attrs)

	if componentType == domain.ComponentRAM {
		normalizeCapacityGB(attrs)
		NormalizeRAMSpeed(title, attrs)
	}

	if componentType == domain.ComponentServer {
		// Title-derived tier is authoritative — overwrites any LLM
		// guess. Anchors barebone shells to their own price baseline
		// so they stop scoring 100 against fully-configured servers.
		// See IMPL-0016 Phase 6.
		attrs["tier"] = DetectServerTier(title)
	}
}

// stripPlaceholderEnums removes optional enum fields whose value is a
// placeholder like "N/A" or "unknown" (which would fail enum validation).
func stripPlaceholderEnums(attrs map[string]any) {
	for _, key := range optionalEnumFields {
		s, ok := attrString(attrs, key)
		if !ok {
			continue
		}
		if slices.Contains(placeholderValues, strings.ToLower(strings.TrimSpace(s))) {
			delete(attrs, key)
		}
	}
}

// defaultConfidence sets confidence to 0.5 if the LLM omitted it. Validation
// requires confidence to be present and in 0.0-1.0; defaulting prevents
// otherwise-valid extractions from being rejected for a missing meta field.
func defaultConfidence(attrs map[string]any) {
	if _, ok := attrs["confidence"]; !ok {
		attrs["confidence"] = 0.5
	}
}

// normalizeCapacityGB recovers GB units when the LLM returned MB or MiB.
// Common patterns: "32GB" returned as 32768 (MiB), 32000 (MB), etc.
// Only applies when the resulting value is in the valid 1-1024 GB range.
func normalizeCapacityGB(attrs map[string]any) {
	capacity, ok := attrInt(attrs, "capacity_gb")
	if !ok || capacity <= 1024 {
		return
	}
	for _, divisor := range []int{1024, 1000} {
		if capacity%divisor == 0 {
			candidate := capacity / divisor
			if candidate >= 1 && candidate <= 1024 {
				attrs["capacity_gb"] = candidate
				return
			}
		}
	}
}
