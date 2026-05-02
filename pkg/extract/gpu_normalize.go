package extract

import (
	"regexp"
	"strings"
)

// gpuFamilyAliases maps common LLM family spellings to canonical
// lowercase tokens used in the GPU product key. Keys are
// lowercased+trimmed before lookup.
var gpuFamilyAliases = map[string]string{
	"tesla":      "tesla",
	"quadro":     "quadro",
	"geforce":    "geforce",
	"rtx":        "rtx",
	"rtx-pro":    "rtx",
	"rtx pro":    "rtx",
	"a-series":   "a-series",
	"a series":   "a-series",
	"ampere":     "a-series",
	"l-series":   "l-series",
	"l series":   "l-series",
	"lovelace":   "l-series",
	"h-series":   "h-series",
	"h series":   "h-series",
	"hopper":     "h-series",
	"radeon pro": "radeon-pro",
	"radeonpro":  "radeon-pro",
	"radeon-pro": "radeon-pro",
	"instinct":   "instinct",
	"arc":        "arc",
	"arc pro":    "arc",
}

// gpuFamilyInferenceRules infer family from a high-confidence model
// prefix. Deliberately conservative: ambiguous prefixes (P4000,
// RTX 4000, …) are NOT matched — better to leave family empty and
// let the product-key segment fall back to "unknown" than to
// mis-infer.
var gpuFamilyInferenceRules = []struct {
	pattern *regexp.Regexp
	family  string
}{
	{regexp.MustCompile(`^(P40|P100|V100|K80|M40|M60|T4)$`), "tesla"},
	{regexp.MustCompile(`^A(10|30|40|100)$`), "a-series"},
	{regexp.MustCompile(`^L(4|40|40S)$`), "l-series"},
	{regexp.MustCompile(`^H(100|200)$`), "h-series"},
	{regexp.MustCompile(`^MI(50|60|100|210|250|300)$`), "instinct"},
}

// gpuValidVRAMSizes are the VRAM SKUs we round to within ±1 GB.
// Out-of-list values (14, 20, 28, …) stay unchanged so legitimate
// odd-VRAM cards aren't corrupted.
var gpuValidVRAMSizes = []int{8, 12, 16, 24, 32, 40, 48, 80, 96, 128}

// CanonicalizeGPUFamily collapses common spellings of a GPU family
// name to a canonical lowercase token. Unknown values fall through
// as lowercased + spaces-collapsed-to-hyphens (forward-compat for
// new families NVIDIA/AMD/Intel introduce).
func CanonicalizeGPUFamily(s string) string {
	key := strings.ToLower(strings.TrimSpace(s))
	if key == "" {
		return ""
	}
	if canonical, ok := gpuFamilyAliases[key]; ok {
		return canonical
	}
	return strings.ReplaceAll(key, " ", "-")
}

// DetectGPUFamilyFromModel returns the canonical family token for a
// model name when the prefix matches a high-confidence pattern.
// Returns "" for unknown / ambiguous models.
func DetectGPUFamilyFromModel(model string) string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return ""
	}
	for _, rule := range gpuFamilyInferenceRules {
		if rule.pattern.MatchString(trimmed) {
			return rule.family
		}
	}
	return ""
}

// NormalizeGPUExtraction repairs common GPU LLM mistakes before
// validation. Mutates attrs in place:
//
//  1. VRAM unit confusion — divide vram_gb by 1024 or 1000 if the
//     value lands in MB/MiB ranges.
//  2. Family canonicalisation — lowercase + alias lookup so different
//     spellings of the same family produce the same product key.
//  3. Family inference — if family is empty after canonicalisation
//     and model matches a high-confidence prefix, fill it in.
//  4. VRAM rounding — snap vram_gb to the nearest known SKU when
//     within ±1 GB. Out-of-list values stay unchanged.
func NormalizeGPUExtraction(attrs map[string]any) {
	normalizeGPUVRAMUnit(attrs)
	normalizeGPUFamily(attrs)
	roundGPUVRAM(attrs)
}

// normalizeGPUVRAMUnit divides vram_gb by 1024 or 1000 when the LLM
// returned MB or KB. Only applies when the resulting value lands in
// the valid 1-256 GB range.
func normalizeGPUVRAMUnit(attrs map[string]any) {
	vram, ok := attrInt(attrs, "vram_gb")
	if !ok || vram <= 256 {
		return
	}
	for _, divisor := range []int{1024, 1000} {
		if vram%divisor == 0 {
			candidate := vram / divisor
			if candidate >= 1 && candidate <= 256 {
				attrs["vram_gb"] = candidate
				return
			}
		}
	}
}

// normalizeGPUFamily resolves a canonical family token, preferring
// model-based inference over the LLM-supplied family.
//
// Why model wins: the LLM is non-deterministic on family — it may pick
// the legacy brand ("Tesla") or the architectural family ("A-series",
// "Ampere") depending on what appears in the title. For any model
// matched by gpuFamilyInferenceRules, the family is unambiguous, so
// the inference rule is the source of truth. Without this, the same
// A100 SKU fragments across `gpu:nvidia:tesla:a100:80gb` and
// `gpu:nvidia:a-series:a100:80gb` baselines.
//
// For ambiguous models (P4000, RTX 4000, …) inference returns "" and
// we fall back to canonicalising the LLM-supplied family.
func normalizeGPUFamily(attrs map[string]any) {
	model, _ := attrString(attrs, "model")
	if inferred := DetectGPUFamilyFromModel(model); inferred != "" {
		attrs["family"] = inferred
		return
	}

	family, _ := attrString(attrs, "family")
	if canonical := CanonicalizeGPUFamily(family); canonical != "" {
		attrs["family"] = canonical
	}
}

// roundGPUVRAM snaps vram_gb to the nearest known SKU when within
// ±1 GB. Out-of-list values (14, 20, …) stay unchanged so legitimate
// odd-VRAM cards aren't corrupted.
func roundGPUVRAM(attrs map[string]any) {
	vram, ok := attrInt(attrs, "vram_gb")
	if !ok {
		return
	}
	for _, sku := range gpuValidVRAMSizes {
		if abs(vram-sku) <= 1 {
			attrs["vram_gb"] = sku
			return
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
