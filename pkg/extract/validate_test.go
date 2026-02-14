package extract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestValidateExtraction_Common(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		attrs   map[string]any
		wantErr string
	}{
		{
			name: "missing condition",
			attrs: map[string]any{
				"confidence": 0.9,
			},
			wantErr: "condition",
		},
		{
			name: "unrecognized condition normalizes to unknown",
			attrs: map[string]any{
				"condition":  "broken",
				"confidence": 0.9,
			},
		},
		{
			name: "capitalized condition normalizes correctly",
			attrs: map[string]any{
				"condition":  "New",
				"confidence": 0.9,
			},
		},
		{
			name: "eBay condition string normalizes correctly",
			attrs: map[string]any{
				"condition":  "Pre-Owned",
				"confidence": 0.9,
			},
		},
		{
			name: "missing confidence",
			attrs: map[string]any{
				"condition": "new",
			},
			wantErr: "confidence",
		},
		{
			name: "confidence too high",
			attrs: map[string]any{
				"condition":  "new",
				"confidence": 1.5,
			},
			wantErr: "confidence",
		},
		{
			name: "confidence too low",
			attrs: map[string]any{
				"condition":  "new",
				"confidence": -0.1,
			},
			wantErr: "confidence",
		},
		{
			name: "quantity less than 1",
			attrs: map[string]any{
				"condition":  "new",
				"confidence": 0.9,
				"quantity":   0,
			},
			wantErr: "quantity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := extract.ValidateExtraction(domain.ComponentOther, tt.attrs)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateExtraction_RAM(t *testing.T) {
	t.Parallel()

	validRAM := map[string]any{
		"condition":   "used_working",
		"confidence":  0.95,
		"quantity":    1,
		"capacity_gb": 32,
		"generation":  "DDR4",
		"speed_mhz":   2666,
	}

	tests := []struct {
		name    string
		modify  func(map[string]any)
		wantErr string
	}{
		{
			name:   "valid RAM passes",
			modify: func(_ map[string]any) {},
		},
		{
			name:    "missing capacity_gb",
			modify:  func(a map[string]any) { delete(a, "capacity_gb") },
			wantErr: "capacity_gb",
		},
		{
			name:    "capacity_gb too low",
			modify:  func(a map[string]any) { a["capacity_gb"] = 0 },
			wantErr: "capacity_gb",
		},
		{
			name:    "capacity_gb too high",
			modify:  func(a map[string]any) { a["capacity_gb"] = 2048 },
			wantErr: "capacity_gb",
		},
		{
			name:    "missing generation",
			modify:  func(a map[string]any) { delete(a, "generation") },
			wantErr: "generation",
		},
		{
			name:    "invalid generation",
			modify:  func(a map[string]any) { a["generation"] = "DDR2" },
			wantErr: "generation",
		},
		{
			name:    "speed_mhz too low",
			modify:  func(a map[string]any) { a["speed_mhz"] = 400 },
			wantErr: "speed_mhz",
		},
		{
			name:    "speed_mhz too high",
			modify:  func(a map[string]any) { a["speed_mhz"] = 10000 },
			wantErr: "speed_mhz",
		},
		{
			name:   "speed_mhz absent is ok",
			modify: func(a map[string]any) { delete(a, "speed_mhz") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attrs := copyAttrs(validRAM)
			tt.modify(attrs)

			err := extract.ValidateExtraction(domain.ComponentRAM, attrs)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateExtraction_Drive(t *testing.T) {
	t.Parallel()

	validDrive := map[string]any{
		"condition":  "new",
		"confidence": 0.9,
		"quantity":   1,
		"capacity":   "3.84TB",
		"interface":  "SATA",
	}

	tests := []struct {
		name    string
		modify  func(map[string]any)
		wantErr string
	}{
		{
			name:   "valid drive passes",
			modify: func(_ map[string]any) {},
		},
		{
			name:    "missing capacity",
			modify:  func(a map[string]any) { delete(a, "capacity") },
			wantErr: "capacity",
		},
		{
			name:    "missing interface",
			modify:  func(a map[string]any) { delete(a, "interface") },
			wantErr: "interface",
		},
		{
			name:    "invalid interface",
			modify:  func(a map[string]any) { a["interface"] = "IDE" },
			wantErr: "interface",
		},
		{
			name:    "invalid form_factor",
			modify:  func(a map[string]any) { a["form_factor"] = "M.2" },
			wantErr: "form_factor",
		},
		{
			name:   "valid form_factor",
			modify: func(a map[string]any) { a["form_factor"] = "2.5" },
		},
		{
			name:    "invalid type",
			modify:  func(a map[string]any) { a["type"] = "tape" },
			wantErr: "type",
		},
		{
			name:   "valid type SSD",
			modify: func(a map[string]any) { a["type"] = "SSD" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attrs := copyAttrs(validDrive)
			tt.modify(attrs)

			err := extract.ValidateExtraction(domain.ComponentDrive, attrs)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateExtraction_Server(t *testing.T) {
	t.Parallel()

	validServer := map[string]any{
		"condition":    "used_working",
		"confidence":   0.88,
		"quantity":     1,
		"manufacturer": "Dell",
		"model":        "R740xd",
	}

	tests := []struct {
		name    string
		modify  func(map[string]any)
		wantErr string
	}{
		{
			name:   "valid server passes",
			modify: func(_ map[string]any) {},
		},
		{
			name:    "missing manufacturer",
			modify:  func(a map[string]any) { delete(a, "manufacturer") },
			wantErr: "manufacturer",
		},
		{
			name:    "empty manufacturer",
			modify:  func(a map[string]any) { a["manufacturer"] = "" },
			wantErr: "manufacturer",
		},
		{
			name:    "missing model",
			modify:  func(a map[string]any) { delete(a, "model") },
			wantErr: "model",
		},
		{
			name:    "invalid form_factor",
			modify:  func(a map[string]any) { a["form_factor"] = "rack" },
			wantErr: "form_factor",
		},
		{
			name:   "valid form_factor 2U",
			modify: func(a map[string]any) { a["form_factor"] = "2U" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attrs := copyAttrs(validServer)
			tt.modify(attrs)

			err := extract.ValidateExtraction(domain.ComponentServer, attrs)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateExtraction_CPU(t *testing.T) {
	t.Parallel()

	validCPU := map[string]any{
		"condition":    "used_working",
		"confidence":   0.95,
		"quantity":     1,
		"manufacturer": "Intel",
		"family":       "Xeon",
		"model":        "6130",
	}

	tests := []struct {
		name    string
		modify  func(map[string]any)
		wantErr string
	}{
		{
			name:   "valid CPU passes",
			modify: func(_ map[string]any) {},
		},
		{
			name:    "missing manufacturer",
			modify:  func(a map[string]any) { delete(a, "manufacturer") },
			wantErr: "manufacturer",
		},
		{
			name:    "invalid manufacturer",
			modify:  func(a map[string]any) { a["manufacturer"] = "ARM" },
			wantErr: "manufacturer",
		},
		{
			name:    "missing family",
			modify:  func(a map[string]any) { delete(a, "family") },
			wantErr: "family",
		},
		{
			name:    "invalid family",
			modify:  func(a map[string]any) { a["family"] = "Core" },
			wantErr: "family",
		},
		{
			name:    "missing model",
			modify:  func(a map[string]any) { delete(a, "model") },
			wantErr: "model",
		},
		{
			name:    "cores too low",
			modify:  func(a map[string]any) { a["cores"] = 0 },
			wantErr: "cores",
		},
		{
			name:    "cores too high",
			modify:  func(a map[string]any) { a["cores"] = 300 },
			wantErr: "cores",
		},
		{
			name:   "cores absent is ok",
			modify: func(a map[string]any) { delete(a, "cores") },
		},
		{
			name:    "base_clock_ghz too low",
			modify:  func(a map[string]any) { a["base_clock_ghz"] = 0.1 },
			wantErr: "base_clock_ghz",
		},
		{
			name:    "base_clock_ghz too high",
			modify:  func(a map[string]any) { a["base_clock_ghz"] = 7.0 },
			wantErr: "base_clock_ghz",
		},
		{
			name:    "tdp_watts too low",
			modify:  func(a map[string]any) { a["tdp_watts"] = 5 },
			wantErr: "tdp_watts",
		},
		{
			name:    "tdp_watts too high",
			modify:  func(a map[string]any) { a["tdp_watts"] = 600 },
			wantErr: "tdp_watts",
		},
		{
			name:   "valid AMD EPYC",
			modify: func(a map[string]any) { a["manufacturer"] = "AMD"; a["family"] = "EPYC" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attrs := copyAttrs(validCPU)
			tt.modify(attrs)

			err := extract.ValidateExtraction(domain.ComponentCPU, attrs)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateExtraction_NIC(t *testing.T) {
	t.Parallel()

	validNIC := map[string]any{
		"condition":  "used_working",
		"confidence": 0.94,
		"quantity":   1,
		"speed":      "10GbE",
		"port_count": 2,
	}

	tests := []struct {
		name    string
		modify  func(map[string]any)
		wantErr string
	}{
		{
			name:   "valid NIC passes",
			modify: func(_ map[string]any) {},
		},
		{
			name:    "missing speed",
			modify:  func(a map[string]any) { delete(a, "speed") },
			wantErr: "speed",
		},
		{
			name:    "invalid speed",
			modify:  func(a map[string]any) { a["speed"] = "5GbE" },
			wantErr: "speed",
		},
		{
			name:    "missing port_count",
			modify:  func(a map[string]any) { delete(a, "port_count") },
			wantErr: "port_count",
		},
		{
			name:    "port_count too low",
			modify:  func(a map[string]any) { a["port_count"] = 0 },
			wantErr: "port_count",
		},
		{
			name:    "port_count too high",
			modify:  func(a map[string]any) { a["port_count"] = 10 },
			wantErr: "port_count",
		},
		{
			name:    "invalid port_type",
			modify:  func(a map[string]any) { a["port_type"] = "USB" },
			wantErr: "port_type",
		},
		{
			name:   "valid port_type SFP28",
			modify: func(a map[string]any) { a["port_type"] = "SFP28" },
		},
		{
			name:   "valid 100GbE QSFP28",
			modify: func(a map[string]any) { a["speed"] = "100GbE"; a["port_type"] = "QSFP28" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attrs := copyAttrs(validNIC)
			tt.modify(attrs)

			err := extract.ValidateExtraction(domain.ComponentNIC, attrs)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateExtraction_ConditionNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "capitalized New", input: "New", expected: "new"},
		{name: "uppercase USED", input: "Pre-Owned", expected: "used_working"},
		{name: "mixed case Open Box", input: "Open Box", expected: "like_new"},
		{name: "already normalized", input: "for_parts", expected: "for_parts"},
		{name: "unrecognized defaults to unknown", input: "broken", expected: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			attrs := map[string]any{
				"condition":  tt.input,
				"confidence": 0.9,
			}
			err := extract.ValidateExtraction(domain.ComponentOther, attrs)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, attrs["condition"])
		})
	}
}

func copyAttrs(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
