package extract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestNormalizeExtraction_DefaultConfidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		attrs   map[string]any
		wantSet bool
		wantVal float64
	}{
		{
			name:    "fills missing confidence with default",
			attrs:   map[string]any{},
			wantSet: true,
			wantVal: 0.5,
		},
		{
			name:    "preserves explicit float64 confidence",
			attrs:   map[string]any{"confidence": 0.9},
			wantSet: true,
			wantVal: 0.9,
		},
		{
			name:    "preserves explicit zero confidence",
			attrs:   map[string]any{"confidence": 0.0},
			wantSet: true,
			wantVal: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			extract.NormalizeExtraction(domain.ComponentRAM, "irrelevant", tt.attrs)
			got, ok := tt.attrs["confidence"]
			assert.Equal(t, tt.wantSet, ok)
			if ok {
				assert.InDelta(t, tt.wantVal, got, 0.0001)
			}
		})
	}
}

func TestNormalizeExtraction_StripsPlaceholderEnums(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		field       string
		value       any
		shouldStrip bool
	}{
		{name: "form_factor N/A stripped", field: "form_factor", value: "N/A", shouldStrip: true},
		{name: "form_factor unknown stripped", field: "form_factor", value: "unknown", shouldStrip: true},
		{name: "form_factor None stripped", field: "form_factor", value: "None", shouldStrip: true},
		{name: "form_factor null-string stripped", field: "form_factor", value: "null", shouldStrip: true},
		{name: "form_factor not specified stripped", field: "form_factor", value: "Not Specified", shouldStrip: true},
		{name: "form_factor 3U preserved", field: "form_factor", value: "3U", shouldStrip: false},
		{name: "type unknown stripped", field: "type", value: "unknown", shouldStrip: true},
		{name: "interface NA stripped", field: "interface", value: "NA", shouldStrip: true},
		{name: "port_type N/A stripped", field: "port_type", value: "N/A", shouldStrip: true},
		{name: "form_factor empty preserved", field: "form_factor", value: "", shouldStrip: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			attrs := map[string]any{tt.field: tt.value, "confidence": 0.8}
			extract.NormalizeExtraction(domain.ComponentServer, "irrelevant", attrs)
			_, present := attrs[tt.field]
			assert.Equal(t, !tt.shouldStrip, present)
		})
	}
}

func TestNormalizeExtraction_CapacityGB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   any
		wantVal int
		wantSet bool
	}{
		{name: "32 GB stays 32", input: 32, wantVal: 32, wantSet: true},
		{name: "1024 GB stays 1024 (boundary)", input: 1024, wantVal: 1024, wantSet: true},
		{name: "32768 (32*1024 MiB) -> 32", input: 32768, wantVal: 32, wantSet: true},
		{name: "16384 (16*1024 MiB) -> 16", input: 16384, wantVal: 16, wantSet: true},
		{name: "32000 (32*1000 MB) -> 32", input: 32000, wantVal: 32, wantSet: true},
		{name: "64000 (64*1000 MB) -> 64", input: 64000, wantVal: 64, wantSet: true},
		{name: "65536 (64*1024 MiB) -> 64", input: 65536, wantVal: 64, wantSet: true},
		{name: "float64 from JSON 32768 -> 32", input: float64(32768), wantVal: 32, wantSet: true},
		{name: "preserved if already in range", input: 256, wantVal: 256, wantSet: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			attrs := map[string]any{
				"capacity_gb": tt.input,
				"confidence":  0.8,
				"generation":  "DDR4",
			}
			extract.NormalizeExtraction(domain.ComponentRAM, "irrelevant", attrs)
			got, ok := attrs["capacity_gb"]
			assert.Equal(t, tt.wantSet, ok)
			if ok {
				switch g := got.(type) {
				case int:
					assert.Equal(t, tt.wantVal, g)
				case float64:
					assert.Equal(t, tt.wantVal, int(g))
				}
			}
		})
	}
}

// TestNormalizeExtraction_RAMSpeedFromTitle confirms the orchestrator
// invokes NormalizeRAMSpeed for RAM components, recovering speed from
// PC module markers when the LLM produced an out-of-range value.
func TestNormalizeExtraction_RAMSpeedFromTitle(t *testing.T) {
	t.Parallel()

	attrs := map[string]any{
		"speed_mhz":   19200, // PC4 byte-rate, not MHz
		"capacity_gb": 32,
		"generation":  "DDR4",
		"confidence":  0.7,
	}
	extract.NormalizeExtraction(
		domain.ComponentRAM,
		"Samsung 32GB DDR4 PC4-19200 ECC",
		attrs,
	)
	assert.Equal(t, 2400, attrs["speed_mhz"])
}

// TestNormalizeExtraction_NonRAMSkipsRAMOnly confirms RAM-only
// normalization does not touch non-RAM attribute maps.
func TestNormalizeExtraction_NonRAMSkipsRAMOnly(t *testing.T) {
	t.Parallel()

	// A drive listing with capacity_gb (not a real drive field, but should
	// not be mutated since we only normalize capacity_gb for RAM).
	attrs := map[string]any{
		"capacity_gb": 32768,
		"confidence":  0.8,
	}
	extract.NormalizeExtraction(domain.ComponentDrive, "irrelevant", attrs)
	assert.Equal(t, 32768, attrs["capacity_gb"])
}

// TestNormalizeExtraction_ServerTier asserts that title-derived tier
// is written into attrs for server listings (IMPL-0016 Phase 6).
func TestNormalizeExtraction_ServerTier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "barebone shell",
			title: "Dell PowerEdge R740XD 24-Bay SFF Barebone Server",
			want:  extract.ServerTierBarebone,
		},
		{
			name:  "configured",
			title: "Dell R740 2x Xeon Gold 5118 64GB DDR4 RAM",
			want:  extract.ServerTierConfigured,
		},
		{
			name:  "ambiguous defaults to partial",
			title: "Dell PowerEdge R740XD Server",
			want:  extract.ServerTierPartial,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			attrs := map[string]any{}
			extract.NormalizeExtraction(domain.ComponentServer, tt.title, attrs)
			assert.Equal(t, tt.want, attrs["tier"], "title=%q", tt.title)
		})
	}
}

// TestNormalizeExtraction_ServerTierOverridesLLM guards that the
// title-derived tier always wins over an LLM-emitted tier value —
// the LLM can't see the full title context as reliably as a regex.
func TestNormalizeExtraction_ServerTierOverridesLLM(t *testing.T) {
	t.Parallel()

	attrs := map[string]any{
		"tier": "configured", // hypothetical LLM guess
	}
	extract.NormalizeExtraction(
		domain.ComponentServer,
		"Dell PowerEdge R640 Barebone No CPU/RAM/HDD",
		attrs,
	)
	assert.Equal(t, extract.ServerTierBarebone, attrs["tier"])
}
