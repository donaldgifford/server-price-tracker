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

	tests := []struct {
		name  string
		model string
		want  string
	}{
		{"empty", "", ""},
		{"P40 -> tesla", "P40", "tesla"},
		{"P100 -> tesla", "P100", "tesla"},
		{"V100 -> tesla", "V100", "tesla"},
		{"K80 -> tesla", "K80", "tesla"},
		{"M40 -> tesla", "M40", "tesla"},
		{"M60 -> tesla", "M60", "tesla"},
		{"T4 -> tesla", "T4", "tesla"},
		{"A10 -> a-series", "A10", "a-series"},
		{"A30 -> a-series", "A30", "a-series"},
		{"A40 -> a-series", "A40", "a-series"},
		{"A100 -> a-series", "A100", "a-series"},
		{"L4 -> l-series", "L4", "l-series"},
		{"L40 -> l-series", "L40", "l-series"},
		{"L40S -> l-series", "L40S", "l-series"},
		{"H100 -> h-series", "H100", "h-series"},
		{"H200 -> h-series", "H200", "h-series"},
		{"MI100 -> instinct", "MI100", "instinct"},
		{"MI210 -> instinct", "MI210", "instinct"},
		{"MI300 -> instinct", "MI300", "instinct"},
		{"P4000 ambiguous -> empty", "P4000", ""},
		{"RTX 4000 -> empty (handled by family field, not model)", "RTX 4000", ""},
		{"unknown model -> empty", "SuperGPU 9000", ""},
		{"trailing whitespace stripped", " P40 ", "tesla"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, extract.DetectGPUFamilyFromModel(tt.model))
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
			want: map[string]any{"vram_gb": 24, "model": "P40", "family": "tesla"},
		},
		{
			name: "vram unit MB float64 -> GB",
			in:   map[string]any{"vram_gb": float64(81920), "model": "A100"},
			want: map[string]any{"vram_gb": 80, "model": "A100", "family": "a-series"},
		},
		{
			name: "vram already in GB unchanged",
			in:   map[string]any{"vram_gb": 24, "family": "Tesla", "model": "P40"},
			want: map[string]any{"vram_gb": 24, "family": "tesla", "model": "P40"},
		},
		{
			name: "vram out-of-range divisors stay unchanged",
			in:   map[string]any{"vram_gb": 24000000, "model": "P40"},
			want: map[string]any{"vram_gb": 24000000, "model": "P40", "family": "tesla"},
		},
		{
			name: "family canonicalisation",
			in:   map[string]any{"vram_gb": 24, "family": "  Tesla  ", "model": "P40"},
			want: map[string]any{"vram_gb": 24, "family": "tesla", "model": "P40"},
		},
		{
			name: "family inferred from model when empty",
			in:   map[string]any{"vram_gb": 80, "model": "A100"},
			want: map[string]any{"vram_gb": 80, "model": "A100", "family": "a-series"},
		},
		{
			name: "family canonicalisation wins over inference",
			in:   map[string]any{"vram_gb": 80, "family": "Hopper", "model": "A100"},
			want: map[string]any{"vram_gb": 80, "family": "h-series", "model": "A100"},
		},
		{
			name: "vram rounding 23 -> 24",
			in:   map[string]any{"vram_gb": 23, "family": "tesla", "model": "P40"},
			want: map[string]any{"vram_gb": 24, "family": "tesla", "model": "P40"},
		},
		{
			name: "vram rounding 25 -> 24",
			in:   map[string]any{"vram_gb": 25, "family": "tesla", "model": "P40"},
			want: map[string]any{"vram_gb": 24, "family": "tesla", "model": "P40"},
		},
		{
			name: "vram rounding 39 -> 40",
			in:   map[string]any{"vram_gb": 39, "model": "A100"},
			want: map[string]any{"vram_gb": 40, "model": "A100", "family": "a-series"},
		},
		{
			name: "vram rounding 81 -> 80",
			in:   map[string]any{"vram_gb": 81, "model": "A100"},
			want: map[string]any{"vram_gb": 80, "model": "A100", "family": "a-series"},
		},
		{
			name: "vram rounding 13 -> 12",
			in:   map[string]any{"vram_gb": 13, "model": "P100"},
			want: map[string]any{"vram_gb": 12, "model": "P100", "family": "tesla"},
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
			want: map[string]any{"vram_gb": 80, "model": "A100", "family": "a-series"},
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
			want: map[string]any{"vram_gb": 24, "family": "tesla", "model": "P40"},
		},
		{
			name: "missing vram_gb returns without error",
			in:   map[string]any{"family": "tesla", "model": "P40"},
			want: map[string]any{"family": "tesla", "model": "P40"},
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
