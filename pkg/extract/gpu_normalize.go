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
// prefix. Patterns operate on the canonical (lowercase, prefix-stripped)
// model produced by CanonicalizeGPUModel. Deliberately conservative:
// ambiguous prefixes (P4000, RTX 4000, …) are NOT matched — better to
// leave family empty and let the product-key segment fall back to
// "unknown" than to mis-infer.
var gpuFamilyInferenceRules = []struct {
	pattern *regexp.Regexp
	family  string
}{
	// Data center accelerators.
	{regexp.MustCompile(`^(p40|p100|v100|k80|m40|m60|t4)$`), "tesla"},
	{regexp.MustCompile(`^a(10|30|40|100)$`), "a-series"},
	{regexp.MustCompile(`^l(4|40|40s)$`), "l-series"},
	{regexp.MustCompile(`^h(100|200)$`), "h-series"},
	{regexp.MustCompile(`^mi(50|60|100|210|250|300)$`), "instinct"},
	// Consumer GeForce RTX (20/30/40/50-series), with optional Ti / Super.
	{regexp.MustCompile(`^[2-5]0\d{2}(ti|super)?$`), "geforce-rtx"},
	// Workstation RTX A-series (formerly Quadro RTX).
	{regexp.MustCompile(`^a[2-6]000$`), "quadro-rtx"},
}

// rtxPrefixRe strips a leading "rtx" brand prefix from the model field.
// The LLM frequently embeds the brand into the model ("RTX 3090",
// "rtx_a2000") despite the family field being its proper home.
var rtxPrefixRe = regexp.MustCompile(`^rtx[_\s-]?`)

// tiSuffixRe and superSuffixRe normalise separators between the SKU
// number and a Ti / Super suffix, so 3090_ti, 3090-ti, "3090 ti", and
// 3090ti all canonicalise to 3090ti.
var (
	tiSuffixRe    = regexp.MustCompile(`(\d)[_\s-]ti$`)
	superSuffixRe = regexp.MustCompile(`(\d)[_\s-]super$`)
)

// gpuValidVRAMSizes are the VRAM SKUs we round to within ±1 GB.
// Out-of-list values (14, 20, 28, …) stay unchanged so legitimate
// odd-VRAM cards aren't corrupted.
var gpuValidVRAMSizes = []int{8, 12, 16, 24, 32, 40, 48, 80, 96, 128}

// CanonicalizeGPUModel collapses common spellings of a GPU model
// identifier to a canonical lowercase token. Used for both product-key
// generation and family inference, so consumer cards like "RTX 3090",
// "rtx_3090", and "3090" all produce the same baseline.
//
// Mutations:
//   - lowercase + trim
//   - strip leading "rtx" brand prefix (rtx_3090 -> 3090, rtx_a2000 -> a2000)
//   - collapse Ti separator (3090_ti -> 3090ti, "3090 ti" -> 3090ti)
//   - collapse Super separator (4070_super -> 4070super)
//
// Unknown formats fall through as lowercase + trim only, so future
// SKUs aren't corrupted.
func CanonicalizeGPUModel(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "" {
		return ""
	}
	lower = rtxPrefixRe.ReplaceAllString(lower, "")
	lower = tiSuffixRe.ReplaceAllString(lower, "${1}ti")
	lower = superSuffixRe.ReplaceAllString(lower, "${1}super")
	return lower
}

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
//  2. Model canonicalisation — strip brand prefix and normalise Ti /
//     Super separators so "rtx_3090", "RTX 3090", "3090" all collapse
//     to "3090".
//  3. Family resolution — model-based inference wins over LLM family
//     for known canonical models; LLM family canonicalised otherwise.
//  4. VRAM rounding — snap vram_gb to the nearest known SKU when
//     within ±1 GB. Out-of-list values stay unchanged.
func NormalizeGPUExtraction(attrs map[string]any) {
	normalizeGPUVRAMUnit(attrs)
	canonicalizeGPUModelInPlace(attrs)
	normalizeGPUFamily(attrs)
	roundGPUVRAM(attrs)
}

// canonicalizeGPUModelInPlace rewrites attrs["model"] to its canonical
// form so the product key consolidates across LLM spelling variants.
func canonicalizeGPUModelInPlace(attrs map[string]any) {
	model, ok := attrString(attrs, "model")
	if !ok {
		return
	}
	if canonical := CanonicalizeGPUModel(model); canonical != "" {
		attrs["model"] = canonical
	}
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
