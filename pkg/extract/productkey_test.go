package extract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
)

func TestProductKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		componentType string
		attrs         map[string]any
		want          string
	}{
		{
			name:          "RAM full attrs",
			componentType: "ram",
			attrs: map[string]any{
				"generation":  "DDR4",
				"ecc":         true,
				"registered":  true,
				"capacity_gb": 32,
				"speed_mhz":   2666,
			},
			want: "ram:ddr4:ecc_reg:32gb:2666",
		},
		{
			name:          "RAM missing speed",
			componentType: "ram",
			attrs: map[string]any{
				"generation":  "DDR4",
				"ecc":         true,
				"registered":  true,
				"capacity_gb": 32,
			},
			want: "ram:ddr4:ecc_reg:32gb:0",
		},
		{
			name:          "RAM ECC unbuffered",
			componentType: "ram",
			attrs: map[string]any{
				"generation":  "DDR4",
				"ecc":         true,
				"registered":  false,
				"capacity_gb": 16,
				"speed_mhz":   3200,
			},
			want: "ram:ddr4:ecc_unbuf:16gb:3200",
		},
		{
			name:          "RAM non-ECC",
			componentType: "ram",
			attrs: map[string]any{
				"generation":  "DDR5",
				"ecc":         false,
				"capacity_gb": 16,
				"speed_mhz":   4800,
			},
			want: "ram:ddr5:non_ecc:16gb:4800",
		},
		{
			name:          "Drive SSD NVMe",
			componentType: "drive",
			attrs: map[string]any{
				"interface":   "NVMe",
				"form_factor": "2.5",
				"capacity":    "3.84TB",
				"type":        "SSD",
			},
			want: "drive:nvme:2.5:3.84tb:ssd",
		},
		{
			name:          "Drive HDD 10K",
			componentType: "drive",
			attrs: map[string]any{
				"interface":   "SAS",
				"form_factor": "2.5",
				"capacity":    "1.2TB",
				"type":        "HDD",
				"rpm":         10000,
			},
			want: "drive:sas:2.5:1.2tb:10k",
		},
		{
			name:          "Drive HDD 7200",
			componentType: "drive",
			attrs: map[string]any{
				"interface":   "SATA",
				"form_factor": "3.5",
				"capacity":    "4TB",
				"type":        "HDD",
				"rpm":         7200,
			},
			want: "drive:sata:3.5:4tb:7k2",
		},
		{
			name:          "Server Dell R740xd configured",
			componentType: "server",
			attrs: map[string]any{
				"manufacturer":      "Dell",
				"model":             "R740xd",
				"drive_form_factor": "SFF",
				"tier":              "configured",
			},
			want: "server:dell:r740xd:sff:configured",
		},
		{
			name:          "Server Dell R740xd barebone",
			componentType: "server",
			attrs: map[string]any{
				"manufacturer":      "Dell",
				"model":             "R740xd",
				"drive_form_factor": "SFF",
				"tier":              "barebone",
			},
			want: "server:dell:r740xd:sff:barebone",
		},
		{
			name:          "Server with no tier defaults to unknown suffix",
			componentType: "server",
			attrs: map[string]any{
				"manufacturer":      "Dell",
				"model":             "R740xd",
				"drive_form_factor": "SFF",
			},
			want: "server:dell:r740xd:sff:unknown",
		},
		{
			name:          "CPU Intel Xeon",
			componentType: "cpu",
			attrs: map[string]any{
				"manufacturer": "Intel",
				"family":       "Xeon",
				"model":        "Gold 6130",
			},
			want: "cpu:intel:xeon:gold_6130",
		},
		{
			name:          "CPU AMD EPYC",
			componentType: "cpu",
			attrs: map[string]any{
				"manufacturer": "AMD",
				"family":       "EPYC",
				"model":        "7763",
			},
			want: "cpu:amd:epyc:7763",
		},
		{
			name:          "NIC 10GbE 2-port SFP+",
			componentType: "nic",
			attrs: map[string]any{
				"speed":      "10GbE",
				"port_count": 2,
				"port_type":  "SFP+",
			},
			want: "nic:10gbe:2p:sfp+",
		},
		{
			name:          "NIC 25GbE from float64 port_count",
			componentType: "nic",
			attrs: map[string]any{
				"speed":      "25GbE",
				"port_count": float64(2),
				"port_type":  "SFP28",
			},
			want: "nic:25gbe:2p:sfp28",
		},
		{
			name:          "unknown type falls through to other",
			componentType: "psu",
			attrs:         map[string]any{},
			want:          "other:psu",
		},
		{
			name:          "GPU NVIDIA Tesla P40 24GB",
			componentType: "gpu",
			attrs: map[string]any{
				"manufacturer": "NVIDIA",
				"family":       "Tesla",
				"model":        "P40",
				"vram_gb":      24,
			},
			want: "gpu:nvidia:tesla:p40:24gb",
		},
		{
			name:          "GPU AMD Instinct MI210 64GB",
			componentType: "gpu",
			attrs: map[string]any{
				"manufacturer": "AMD",
				"family":       "Instinct",
				"model":        "MI210",
				"vram_gb":      64,
			},
			want: "gpu:amd:instinct:mi210:64gb",
		},
		{
			name:          "GPU family null falls back to unknown",
			componentType: "gpu",
			attrs: map[string]any{
				"manufacturer": "NVIDIA",
				"model":        "A100",
				"vram_gb":      80,
			},
			want: "gpu:nvidia:unknown:a100:80gb",
		},
		{
			name:          "GPU vram_gb from float64",
			componentType: "gpu",
			attrs: map[string]any{
				"manufacturer": "NVIDIA",
				"family":       "Tesla",
				"model":        "V100",
				"vram_gb":      float64(32),
			},
			want: "gpu:nvidia:tesla:v100:32gb",
		},
		{
			name:          "GPU empty attrs defaults to unknown/0",
			componentType: "gpu",
			attrs:         map[string]any{},
			want:          "gpu:unknown:unknown:unknown:0gb",
		},
		{
			name:          "nil attrs defaults to unknown/zero",
			componentType: "ram",
			attrs:         nil,
			want:          "ram:unknown:unknown:0gb:0",
		},
		{
			name:          "empty attrs defaults to unknown/zero",
			componentType: "server",
			attrs:         map[string]any{},
			want:          "server:unknown:unknown:unknown:unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extract.ProductKey(tt.componentType, tt.attrs)
			assert.Equal(t, tt.want, got)
		})
	}
}
