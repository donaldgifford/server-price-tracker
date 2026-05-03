package extract_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestCanonicalizeSystemVendor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"Dell", "dell"},
		{"DELL", "dell"},
		{"Dell Inc.", "dell"},
		{"Dell Technologies", "dell"},
		{"HP", "hp"},
		{"HPE", "hp"},
		{"Hewlett-Packard", "hp"},
		{"Hewlett Packard Enterprise", "hp"},
		{"Lenovo", "lenovo"},
		{"IBM", "lenovo"},
		{"Apple", "apple"},
		{"  dell  ", "dell"},
		{"", ""},
		{"Random Vendor", "random-vendor"}, // unknown falls through, spaces → hyphens
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := extract.CanonicalizeSystemVendor(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCanonicalizeSystemLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"Precision", "precision"},
		{"Dell Precision", "precision"},
		{"Precision Tower", "precision"},
		{"ThinkStation", "thinkstation"},
		{"Think Station", "thinkstation"},
		{"Z by HP", "z-by-hp"},
		{"Z-by-HP", "z-by-hp"},
		{"HP Z-Series", "z-by-hp"},
		{"Pro Max", "pro-max"},
		{"pro-max", "pro-max"},
		{"PROMAX", "pro-max"},
		{"OptiPlex", "optiplex"},
		{"Dell OptiPlex", "optiplex"},
		{"ThinkCentre", "thinkcentre"},
		{"EliteDesk", "elitedesk"},
		{"HP EliteDesk", "elitedesk"},
		{"ProDesk", "prodesk"},
		{"Pro", "pro"},
		{"", ""},
		{"Random Line", "random-line"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := extract.CanonicalizeSystemLine(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCanonicalizeSystemModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"T7920", "t7920"},
		{"t7920", "t7920"},
		{"Dell T7920", "t7920"},
		{"DELL_T7920", "t7920"},
		{"HP Z8 G4", "z8 g4"},
		{"hp-z8-g4", "z8-g4"},
		{"Lenovo P620", "p620"},
		{"P620", "p620"},
		{"OptiPlex 7080", "optiplex 7080"},
		{"  M920  ", "m920"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := extract.CanonicalizeSystemModel(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInferSystemLineFromModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		// Precision T-series
		{"t7920", "precision"},
		{"t5810", "precision"},
		{"t3640", "precision"},
		// ThinkStation P-series
		{"p620", "thinkstation"},
		{"p520", "thinkstation"},
		{"p340", "thinkstation"},
		// Z by HP
		{"z8 g4", "z-by-hp"},
		{"z4g4", "z-by-hp"},
		// ThinkCentre M-series
		{"m920", "thinkcentre"},
		{"m720q", "thinkcentre"},
		// OptiPlex
		{"optiplex 7080", "optiplex"},
		{"optiplex", "optiplex"},
		// EliteDesk
		{"elitedesk 800", "elitedesk"},
		// ProDesk
		{"prodesk 600", "prodesk"},
		// Ambiguous / unknown
		{"7080", ""},
		{"random", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := extract.InferSystemLineFromModel(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeSystemExtraction_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ct   domain.ComponentType
		in   map[string]any
		want map[string]any
	}{
		{
			name: "Dell Precision T7920 — full LLM input",
			ct:   domain.ComponentWorkstation,
			in: map[string]any{
				"vendor": "Dell",
				"line":   "Precision",
				"model":  "T7920",
			},
			want: map[string]any{
				"vendor": "dell",
				"line":   "precision",
				"model":  "t7920",
			},
		},
		{
			name: "Dell Precision T7920 — line missing, inferred from model",
			ct:   domain.ComponentWorkstation,
			in: map[string]any{
				"vendor": "Dell",
				"model":  "T7920",
			},
			want: map[string]any{
				"vendor": "dell",
				"line":   "precision",
				"model":  "t7920",
			},
		},
		{
			name: "Lenovo ThinkStation P620 — vendor brand prefix in model",
			ct:   domain.ComponentWorkstation,
			in: map[string]any{
				"vendor": "Lenovo",
				"line":   "ThinkStation",
				"model":  "Lenovo P620",
			},
			want: map[string]any{
				"vendor": "lenovo",
				"line":   "thinkstation",
				"model":  "p620",
			},
		},
		{
			name: "HP Z8 G4 — Z by HP collapsed",
			ct:   domain.ComponentWorkstation,
			in: map[string]any{
				"vendor": "Hewlett-Packard",
				"line":   "Z by HP",
				"model":  "Z8 G4",
			},
			want: map[string]any{
				"vendor": "hp",
				"line":   "z-by-hp",
				"model":  "z8 g4",
			},
		},
		{
			name: "Dell OptiPlex 7080 — line inferred from model prefix",
			ct:   domain.ComponentDesktop,
			in: map[string]any{
				"vendor": "Dell",
				"model":  "OptiPlex 7080",
			},
			want: map[string]any{
				"vendor": "dell",
				"line":   "optiplex",
				"model":  "optiplex 7080",
			},
		},
		{
			name: "Lenovo ThinkCentre M920 — line inferred from model",
			ct:   domain.ComponentDesktop,
			in: map[string]any{
				"vendor": "Lenovo",
				"model":  "M920",
			},
			want: map[string]any{
				"vendor": "lenovo",
				"line":   "thinkcentre",
				"model":  "m920",
			},
		},
		{
			name: "HP EliteDesk 800 G6 — line inferred",
			ct:   domain.ComponentDesktop,
			in: map[string]any{
				"vendor": "HP",
				"model":  "EliteDesk 800 G6",
			},
			want: map[string]any{
				"vendor": "hp",
				"line":   "elitedesk",
				"model":  "elitedesk 800 g6",
			},
		},
		{
			name: "Dell Pro Max kept distinct from Precision (Open Q5)",
			ct:   domain.ComponentWorkstation,
			in: map[string]any{
				"vendor": "Dell",
				"line":   "Pro Max",
				"model":  "T7960",
			},
			want: map[string]any{
				"vendor": "dell",
				"line":   "pro-max",
				"model":  "t7960",
			},
		},
		{
			name: "ambiguous numeric model — line stays empty",
			ct:   domain.ComponentDesktop,
			in: map[string]any{
				"vendor": "Custom",
				"model":  "7080",
			},
			want: map[string]any{
				"vendor": "custom",
				"model":  "7080",
				// no line — inference returned empty, LLM didn't supply one
			},
		},
		{
			name: "Dell Inc. → dell",
			ct:   domain.ComponentDesktop,
			in: map[string]any{
				"vendor": "Dell Inc.",
				"line":   "OptiPlex",
				"model":  "5070",
			},
			want: map[string]any{
				"vendor": "dell",
				"line":   "optiplex",
				"model":  "5070",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			attrs := copySystemAttrs(tt.in)
			extract.NormalizeSystemExtraction(tt.ct, attrs)
			for k, want := range tt.want {
				assert.Equal(t, want, attrs[k], "key=%q", k)
			}
			// Assert no unexpected keys gained.
			for k := range attrs {
				if _, expected := tt.want[k]; !expected {
					_, original := tt.in[k]
					assert.True(t, original, "unexpected new key %q added by normaliser", k)
				}
			}
		})
	}
}

func copySystemAttrs(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	maps.Copy(dst, src)
	return dst
}
