package extract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestRenderClassifyPrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		wantSubs []string
	}{
		{
			name:  "simple title",
			title: "Samsung 32GB DDR4 ECC",
			wantSubs: []string{
				"Title: Samsung 32GB DDR4 ECC",
				"ram, drive, server, cpu, nic, other",
				"Respond with ONLY a single word from the list above",
			},
		},
		{
			name:  "title with special characters",
			title: `Intel X710-DA2 10GbE SFP+ "Dual Port" & PCIe <x8>`,
			wantSubs: []string{
				`Title: Intel X710-DA2 10GbE SFP+ "Dual Port" & PCIe <x8>`,
			},
		},
		{
			name:     "empty title",
			title:    "",
			wantSubs: []string{"Title: \n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := extract.RenderClassifyPrompt(tt.title)
			require.NoError(t, err)

			for _, sub := range tt.wantSubs {
				assert.Contains(t, result, sub)
			}
		})
	}
}

func TestRenderExtractPrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		componentType domain.ComponentType
		title         string
		specs         map[string]string
		wantSubs      []string
		wantErr       bool
	}{
		{
			name:          "RAM with specs",
			componentType: domain.ComponentRAM,
			title:         "Samsung 32GB 2Rx4 PC4-2666V DDR4 ECC REG",
			specs:         map[string]string{"Brand": "Samsung", "Memory Type": "DDR4"},
			wantSubs: []string{
				"Title: Samsung 32GB 2Rx4 PC4-2666V DDR4 ECC REG",
				"Brand: Samsung",
				"Memory Type: DDR4",
				"capacity_gb",
				"generation",
			},
		},
		{
			name:          "RAM without specs",
			componentType: domain.ComponentRAM,
			title:         "Samsung 32GB DDR4",
			specs:         nil,
			wantSubs: []string{
				"Item Specifics: N/A",
			},
		},
		{
			name:          "drive prompt",
			componentType: domain.ComponentDrive,
			title:         "Samsung PM883 3.84TB SATA SSD 2.5",
			specs:         nil,
			wantSubs: []string{
				"Title: Samsung PM883 3.84TB SATA SSD 2.5",
				"capacity_bytes",
				`"interface"`,
				`"type"`,
			},
		},
		{
			name:          "CPU prompt",
			componentType: domain.ComponentCPU,
			title:         "Intel Xeon Gold 6130 SR3B0",
			specs:         map[string]string{"Socket": "LGA3647"},
			wantSubs: []string{
				"Title: Intel Xeon Gold 6130 SR3B0",
				"Socket: LGA3647",
				"base_clock_ghz",
				"matched_pair",
			},
		},
		{
			name:          "NIC prompt",
			componentType: domain.ComponentNIC,
			title:         "Intel X710-DA2 10GbE SFP+ Dual Port",
			specs:         nil,
			wantSubs: []string{
				"Title: Intel X710-DA2 10GbE SFP+ Dual Port",
				"port_count",
				"port_type",
				"firmware_protocol",
			},
		},
		{
			name:          "server uses generic render",
			componentType: domain.ComponentServer,
			title:         "Dell R740xd 24xSFF",
			specs:         nil,
			wantSubs: []string{
				"Title: Dell R740xd 24xSFF",
				"drive_bays",
				"raid_controller",
				// Description will be empty since we use RenderExtractPrompt
				"Description (first 500 chars): ",
			},
		},
		{
			name:          "unknown component type returns error",
			componentType: "unknown",
			title:         "Test",
			wantErr:       true,
		},
		{
			name:          "special chars in title preserved",
			componentType: domain.ComponentRAM,
			title:         `LOT 8x 16GB PC4-2400T Hynix "HMA82GR7AFR8N-UH" DDR4 <ECC> & REG`,
			specs:         nil,
			wantSubs: []string{
				`LOT 8x 16GB PC4-2400T Hynix "HMA82GR7AFR8N-UH" DDR4 <ECC> & REG`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := extract.RenderExtractPrompt(
				tt.componentType,
				tt.title,
				tt.specs,
			)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			for _, sub := range tt.wantSubs {
				assert.Contains(t, result, sub)
			}
		})
	}
}

func TestRenderServerExtractPrompt(t *testing.T) {
	t.Parallel()

	result, err := extract.RenderServerExtractPrompt(
		"Dell R740xd 24xSFF 2U Server",
		map[string]string{"Model": "R740xd"},
		"Dell PowerEdge R740xd with 24 SFF drive bays...",
	)
	require.NoError(t, err)

	assert.Contains(t, result, "Title: Dell R740xd 24xSFF 2U Server")
	assert.Contains(t, result, "Model: R740xd")
	assert.Contains(
		t,
		result,
		"Description (first 500 chars): Dell PowerEdge R740xd with 24 SFF drive bays...",
	)
	assert.Contains(t, result, "drive_bays")
	assert.Contains(t, result, "boots_tested")
}

func TestRenderExtractPrompt_RAMContainsPC4Rules(t *testing.T) {
	t.Parallel()

	result, err := extract.RenderExtractPrompt(
		domain.ComponentRAM,
		"Samsung 32GB DDR4 PC4-21300 ECC",
		nil,
	)
	require.NoError(t, err)

	// Verify the prompt contains PC4-to-MHz mapping rules.
	assert.Contains(t, result, "PC4-21300=2666")
	assert.Contains(t, result, "PC3-12800=1600")
	assert.Contains(t, result, "PC5-38400=4800")
	assert.Contains(t, result, "derived from PC module number if present")
}
