package extract

import (
	"strings"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// conditionMap maps normalized eBay condition strings to domain conditions.
var conditionMap = map[string]domain.Condition{
	// normalized enum values (identity mappings)
	"new":          domain.ConditionNew,
	"like_new":     domain.ConditionLikeNew,
	"used_working": domain.ConditionUsedWorking,
	"for_parts":    domain.ConditionForParts,
	"unknown":      domain.ConditionUnknown,
	// eBay / LLM variants
	"brand new":                domain.ConditionNew,
	"factory sealed":           domain.ConditionNew,
	"open box":                 domain.ConditionLikeNew,
	"manufacturer refurbished": domain.ConditionLikeNew,
	"used":                     domain.ConditionUsedWorking,
	"pre-owned":                domain.ConditionUsedWorking,
	"seller refurbished":       domain.ConditionUsedWorking,
	"pulled from working":      domain.ConditionUsedWorking,
	"tested working":           domain.ConditionUsedWorking,
	"for parts":                domain.ConditionForParts,
	"not working":              domain.ConditionForParts,
	"parts only":               domain.ConditionForParts,
	"as-is":                    domain.ConditionForParts,
}

// NormalizeCondition maps a raw condition string (from eBay or LLM) to a
// normalized domain.Condition. Returns ConditionUnknown if the input doesn't
// match any known condition.
func NormalizeCondition(raw string) domain.Condition {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return domain.ConditionUnknown
	}

	if c, ok := conditionMap[normalized]; ok {
		return c
	}

	return domain.ConditionUnknown
}
