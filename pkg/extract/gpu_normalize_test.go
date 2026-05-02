package extract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
)

func TestCanonicalizeGPUFamily(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"tesla lowercase", "tesla", "tesla"},
		{"tesla title case", "Tesla", "tesla"},
		{"tesla all caps with padding", "  TESLA  ", "tesla"},
		{"quadro", "Quadro", "quadro"},
		{"geforce", "GeForce", "geforce"},
		{"rtx pro spaced", "RTX Pro", "rtx"},
		{"rtx-pro hyphenated", "RTX-PRO", "rtx"},
		{"a-series hyphen", "A-Series", "a-series"},
		{"a series spaced", "A Series", "a-series"},
		{"ampere -> a-series", "Ampere", "a-series"},
		{"l-series hyphen", "L-Series", "l-series"},
		{"lovelace -> l-series", "Lovelace", "l-series"},
		{"hopper -> h-series", "Hopper", "h-series"},
		{"radeon pro spaced", "Radeon Pro", "radeon-pro"},
		{"radeonpro joined", "RadeonPro", "radeon-pro"},
		{"instinct", "Instinct", "instinct"},
		{"arc", "Arc", "arc"},
		{"arc pro", "Arc Pro", "arc"},
		{"unknown stays lowercased", "SomeNewFamily", "somenewfamily"},
		{"unknown spaces become hyphens", "Some Future Family", "some-future-family"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, extract.CanonicalizeGPUFamily(tt.input))
		})
	}
}

func TestDetectGPUFamilyFromModel(t *testing.T) {
	t.Parallel()

	// Inference rules expect canonical (lowercase, prefix-stripped) model
	// values produced by CanonicalizeGPUModel.
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{"empty", "", ""},
		{"p40 -> tesla", "p40", "tesla"},
		{"p100 -> tesla", "p100", "tesla"},
		{"v100 -> tesla", "v100", "tesla"},
		{"k80 -> tesla", "k80", "tesla"},
		{"m40 -> tesla", "m40", "tesla"},
		{"m60 -> tesla", "m60", "tesla"},
		{"t4 -> tesla", "t4", "tesla"},
		{"a10 -> a-series", "a10", "a-series"},
		{"a30 -> a-series", "a30", "a-series"},
		{"a40 -> a-series", "a40", "a-series"},
		{"a100 -> a-series", "a100", "a-series"},
		{"l4 -> l-series", "l4", "l-series"},
		{"l40 -> l-series", "l40", "l-series"},
		{"l40s -> l-series", "l40s", "l-series"},
		{"h100 -> h-series", "h100", "h-series"},
		{"h200 -> h-series", "h200", "h-series"},
		{"mi100 -> instinct", "mi100", "instinct"},
		{"mi210 -> instinct", "mi210", "instinct"},
		{"mi300 -> instinct", "mi300", "instinct"},
		{"3090 -> geforce-rtx", "3090", "geforce-rtx"},
		{"3090ti -> geforce-rtx", "3090ti", "geforce-rtx"},
		{"4070super -> geforce-rtx", "4070super", "geforce-rtx"},
		{"4090 -> geforce-rtx", "4090", "geforce-rtx"},
		{"5090 -> geforce-rtx", "5090", "geforce-rtx"},
		{"a2000 -> quadro-rtx", "a2000", "quadro-rtx"},
		{"a4000 -> quadro-rtx", "a4000", "quadro-rtx"},
		{"a5000 -> quadro-rtx", "a5000", "quadro-rtx"},
		{"a6000 -> quadro-rtx", "a6000", "quadro-rtx"},
		{"p4000 ambiguous -> empty", "p4000", ""},
		{"unknown model -> empty", "supergpu 9000", ""},
		{"trailing whitespace stripped", " p40 ", "tesla"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, extract.DetectGPUFamilyFromModel(tt.model))
		})
	}
}

func TestCanonicalizeGPUModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"already canonical lowercase", "3090", "3090"},
		{"uppercase A100 lowercased", "A100", "a100"},
		{"trim whitespace", "  P40  ", "p40"},
		{"rtx underscore prefix stripped", "rtx_3090", "3090"},
		{"rtx space prefix stripped", "RTX 3090", "3090"},
		{"rtx hyphen prefix stripped", "rtx-3090", "3090"},
		{"rtx no separator prefix stripped", "rtx3090", "3090"},
		{"ti underscore separator collapsed", "rtx_3090_ti", "3090ti"},
		{"ti space separator collapsed", "3090 ti", "3090ti"},
		{"ti hyphen separator collapsed", "3090-ti", "3090ti"},
		{"ti no separator unchanged", "3090ti", "3090ti"},
		{"super separator collapsed", "rtx_4070_super", "4070super"},
		{"super space separator collapsed", "4070 super", "4070super"},
		{"workstation rtx a-series prefix stripped", "RTX A2000", "a2000"},
		{"workstation rtx a-series underscore stripped", "rtx_a4000", "a4000"},
		{"data center sku unchanged (no rtx prefix)", "P40", "p40"},
		{"a100 unchanged", "A100", "a100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, extract.CanonicalizeGPUModel(tt.input))
		})
	}
}

func TestNormalizeGPUExtraction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   map[string]any
		want map[string]any
	}{
		{
			name: "vram unit MB -> GB (24576 -> 24)",
			in:   map[string]any{"vram_gb": 24576, "model": "P40"},
			want: map[string]any{"vram_gb": 24, "model": "p40", "family": "tesla"},
		},
		{
			name: "vram unit MB float64 -> GB",
			in:   map[string]any{"vram_gb": float64(81920), "model": "A100"},
			want: map[string]any{"vram_gb": 80, "model": "a100", "family": "a-series"},
		},
		{
			name: "vram already in GB unchanged",
			in:   map[string]any{"vram_gb": 24, "family": "Tesla", "model": "P40"},
			want: map[string]any{"vram_gb": 24, "family": "tesla", "model": "p40"},
		},
		{
			name: "vram out-of-range divisors stay unchanged",
			in:   map[string]any{"vram_gb": 24000000, "model": "P40"},
			want: map[string]any{"vram_gb": 24000000, "model": "p40", "family": "tesla"},
		},
		{
			name: "family canonicalisation",
			in:   map[string]any{"vram_gb": 24, "family": "  Tesla  ", "model": "P40"},
			want: map[string]any{"vram_gb": 24, "family": "tesla", "model": "p40"},
		},
		{
			name: "family inferred from model when empty",
			in:   map[string]any{"vram_gb": 80, "model": "A100"},
			want: map[string]any{"vram_gb": 80, "model": "a100", "family": "a-series"},
		},
		{
			name: "model inference wins over llm family for known sku",
			in:   map[string]any{"vram_gb": 80, "family": "Tesla", "model": "A100"},
			want: map[string]any{"vram_gb": 80, "family": "a-series", "model": "a100"},
		},
		{
			name: "model inference wins over conflicting llm family",
			in:   map[string]any{"vram_gb": 80, "family": "Hopper", "model": "A100"},
			want: map[string]any{"vram_gb": 80, "family": "a-series", "model": "a100"},
		},
		{
			name: "llm family used when model is ambiguous",
			in:   map[string]any{"vram_gb": 16, "family": "Quadro", "model": "P4000"},
			want: map[string]any{"vram_gb": 16, "family": "quadro", "model": "p4000"},
		},
		{
			name: "llm family used when model is empty",
			in:   map[string]any{"vram_gb": 24, "family": "Tesla"},
			want: map[string]any{"vram_gb": 24, "family": "tesla"},
		},
		{
			name: "rtx prefix stripped from consumer model",
			in:   map[string]any{"vram_gb": 24, "model": "rtx_3090"},
			want: map[string]any{"vram_gb": 24, "model": "3090", "family": "geforce-rtx"},
		},
		{
			name: "rtx space-separated prefix stripped",
			in:   map[string]any{"vram_gb": 24, "model": "RTX 3090"},
			want: map[string]any{"vram_gb": 24, "model": "3090", "family": "geforce-rtx"},
		},
		{
			name: "rtx no-separator prefix stripped",
			in:   map[string]any{"vram_gb": 24, "model": "rtx3090"},
			want: map[string]any{"vram_gb": 24, "model": "3090", "family": "geforce-rtx"},
		},
		{
			name: "ti separator collapsed (3090_ti -> 3090ti)",
			in:   map[string]any{"vram_gb": 24, "model": "rtx_3090_ti"},
			want: map[string]any{"vram_gb": 24, "model": "3090ti", "family": "geforce-rtx"},
		},
		{
			name: "ti no separator unchanged (3090ti)",
			in:   map[string]any{"vram_gb": 24, "model": "RTX 3090ti"},
			want: map[string]any{"vram_gb": 24, "model": "3090ti", "family": "geforce-rtx"},
		},
		{
			name: "ti space separator collapsed (3090 ti -> 3090ti)",
			in:   map[string]any{"vram_gb": 24, "model": "3090 ti"},
			want: map[string]any{"vram_gb": 24, "model": "3090ti", "family": "geforce-rtx"},
		},
		{
			name: "consumer rtx 4090 inferred as geforce-rtx",
			in:   map[string]any{"vram_gb": 24, "model": "RTX 4090"},
			want: map[string]any{"vram_gb": 24, "model": "4090", "family": "geforce-rtx"},
		},
		{
			name: "consumer rtx 5090 inferred as geforce-rtx",
			in:   map[string]any{"vram_gb": 32, "model": "RTX 5090"},
			want: map[string]any{"vram_gb": 32, "model": "5090", "family": "geforce-rtx"},
		},
		{
			name: "consumer rtx 4070 super inferred as geforce-rtx",
			in:   map[string]any{"vram_gb": 12, "model": "rtx_4070_super"},
			want: map[string]any{"vram_gb": 12, "model": "4070super", "family": "geforce-rtx"},
		},
		{
			name: "workstation rtx a2000 inferred as quadro-rtx",
			in:   map[string]any{"vram_gb": 12, "family": "Quadro", "model": "RTX A2000"},
			want: map[string]any{"vram_gb": 12, "family": "quadro-rtx", "model": "a2000"},
		},
		{
			name: "workstation rtx a4000 inferred as quadro-rtx",
			in:   map[string]any{"vram_gb": 16, "model": "rtx_a4000"},
			want: map[string]any{"vram_gb": 16, "model": "a4000", "family": "quadro-rtx"},
		},
		{
			name: "workstation rtx a6000 inferred over llm family",
			in:   map[string]any{"vram_gb": 48, "family": "Ampere", "model": "A6000"},
			want: map[string]any{"vram_gb": 48, "family": "quadro-rtx", "model": "a6000"},
		},
		{
			name: "consumer rtx with conflicting tesla family — inference wins",
			in:   map[string]any{"vram_gb": 24, "family": "Tesla", "model": "rtx_3090"},
			want: map[string]any{"vram_gb": 24, "family": "geforce-rtx", "model": "3090"},
		},
		{
			name: "vram rounding 23 -> 24",
			in:   map[string]any{"vram_gb": 23, "family": "tesla", "model": "P40"},
			want: map[string]any{"vram_gb": 24, "family": "tesla", "model": "p40"},
		},
		{
			name: "vram rounding 25 -> 24",
			in:   map[string]any{"vram_gb": 25, "family": "tesla", "model": "P40"},
			want: map[string]any{"vram_gb": 24, "family": "tesla", "model": "p40"},
		},
		{
			name: "vram rounding 39 -> 40",
			in:   map[string]any{"vram_gb": 39, "model": "A100"},
			want: map[string]any{"vram_gb": 40, "model": "a100", "family": "a-series"},
		},
		{
			name: "vram rounding 81 -> 80",
			in:   map[string]any{"vram_gb": 81, "model": "A100"},
			want: map[string]any{"vram_gb": 80, "model": "a100", "family": "a-series"},
		},
		{
			name: "vram rounding 13 -> 12",
			in:   map[string]any{"vram_gb": 13, "model": "P100"},
			want: map[string]any{"vram_gb": 12, "model": "p100", "family": "tesla"},
		},
		{
			name: "vram rounding 7 -> 8",
			in:   map[string]any{"vram_gb": 7},
			want: map[string]any{"vram_gb": 8},
		},
		{
			name: "vram rounding 15 -> 16",
			in:   map[string]any{"vram_gb": 15},
			want: map[string]any{"vram_gb": 16},
		},
		{
			name: "vram exact 80 unchanged",
			in:   map[string]any{"vram_gb": 80, "model": "A100"},
			want: map[string]any{"vram_gb": 80, "model": "a100", "family": "a-series"},
		},
		{
			name: "vram out-of-list 14 stays 14",
			in:   map[string]any{"vram_gb": 14},
			want: map[string]any{"vram_gb": 14},
		},
		{
			name: "vram out-of-list 20 stays 20",
			in:   map[string]any{"vram_gb": 20},
			want: map[string]any{"vram_gb": 20},
		},
		{
			name: "vram out-of-list 28 stays 28",
			in:   map[string]any{"vram_gb": 28},
			want: map[string]any{"vram_gb": 28},
		},
		{
			name: "round-trip messy extraction",
			in:   map[string]any{"vram_gb": 24576, "family": " Tesla ", "model": "P40"},
			want: map[string]any{"vram_gb": 24, "family": "tesla", "model": "p40"},
		},
		{
			name: "missing vram_gb returns without error",
			in:   map[string]any{"family": "tesla", "model": "P40"},
			want: map[string]any{"family": "tesla", "model": "p40"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			extract.NormalizeGPUExtraction(tt.in)
			assert.Equal(t, tt.want, tt.in)
		})
	}
}
