package extract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
)

func TestIsAccessoryOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  bool
	}{
		// Pure accessory titles → true
		{"backplane lowercase", "dell r740 backplane k2y8n7", true},
		{
			name: "backplane production example",
			title: "DELL EMC POWEREDGE R740xd 24 BAY SFF SERVER BACKPLANE " +
				"K2Y8N7 58D2W P1MJ3",
			want: true,
		},
		{"drive caddy", "Dell PowerEdge 2.5\" drive caddy", true},
		{"caddies plural", "Lot of 8 server drive caddies", true},
		{"sled", "Dell hot-swap sled tray", true},
		{"rack rails", "Dell ReadyRails II rack rails for R730", true},
		{"single rail", "Dell server rail kit", true},
		{"bezel", "Dell PowerEdge R730 front bezel", true},
		{"mounting bracket", "HPE ProLiant mounting bracket", true},
		{"riser", "Dell PowerEdge R720 riser card", true},
		{"gpu riser", "PCIe GPU riser cable adapter", true},
		{"heatsink no primary", "Replacement copper heatsink", true},
		{"heat-sink hyphenated", "Server heat-sink with fan shroud", true},
		{"fan assembly", "Dell PowerEdge fan assembly", true},
		{"fan kit", "Replacement fan kit for ProLiant", true},
		{"cable no primary", "Dell server power cable adapter", true},

		// Primary component titles → false
		{
			name:  "dell server",
			title: "Dell PowerEdge R740xd 24-bay 2x Xeon Gold",
			want:  false,
		},
		{"cisco server", "Cisco UCS C220 M5 1U Server", false},
		{
			name:  "ddr4 ram",
			title: "Samsung 32GB DDR4 ECC RDIMM 2666MHz",
			want:  false,
		},
		{"nvme drive", "Samsung 1TB NVMe PCIe 3.0 SSD", false},
		{"sas drive", "HGST 4TB SAS 12Gbps 7.2K HDD", false},
		{"xeon cpu", "Intel Xeon Gold 6130 SR3B0 16-core", false},
		{"epyc cpu", "AMD EPYC 7551 32-core processor", false},

		// Mixed titles → primary keyword wins, defer to LLM
		{
			name:  "form factor server with rails",
			title: "4U Dell PowerEdge server with rack rails included",
			want:  false,
		},
		{
			name:  "ssd in tray",
			title: "1TB NVMe SSD in 2.5\" hot-swap tray",
			want:  false,
		},
		{
			name:  "ram in lot with caddies",
			title: "32GB DDR4 RAM and drive caddies bundle",
			want:  false,
		},
		{
			name:  "xeon heatsink defers to llm",
			title: "Intel Xeon heatsink for LGA2011",
			want:  false,
		},
		{
			name:  "sas cable defers to llm",
			title: "Dell mini-SAS HD cable 0.5m",
			want:  false,
		},

		// Casing
		{"all caps", "BACKPLANE K2Y8N7", true},
		{"mixed case", "BackPlane Assembly", true},

		// Empty / whitespace
		{"empty string", "", false},
		{"whitespace only", "   \t\n", false},

		// No accessory and no primary keyword
		{"unrelated", "Apple MacBook Pro 16-inch", false},

		// Compound accessory wins over primary keyword (post-deploy fix).
		{
			name: "sas cable beats sas primary",
			title: "Dell NMRJN Poweredge R740XD 24HDD SFF H350 H750 " +
				"SAS Cable",
			want: true,
		},
		{
			name:  "nvme ssd backplane beats nvme/ssd primaries",
			title: "Dell PowerEdge R640 10 Bay 2.5\" NVME SSD Backplane Board MWY54",
			want:  true,
		},

		// New primary signals route real barebone servers to LLM even
		// when an accessory keyword (heatsink, tray, bezel, …) appears.
		{
			name:  "barebone server defers",
			title: "Dell PowerEdge R640 10-Bay SFF Barebone Server 2 x HeatSink H330 2 x 1100W PSU",
			want:  false,
		},
		{
			name:  "chassis defers",
			title: "Dell PowerEdge R940 24 x 2.5\" Server Chassis, 4x heatsinks, 2x PSU",
			want:  false,
		},
		{
			name:  "xeon model SKU defers",
			title: "DELL R740XD 18LFF 2x Gold 5118 2.3GHZ=24Cores 3x HD Tray H730P",
			want:  false,
		},
		{
			name:  "idrac defers",
			title: "Dell PowerEdge R640 8bay (2) sinks, 8-HP fans,8-trays, H740P, Idrac 9 Ent, 2xPsu",
			want:  false,
		},
		{
			// Original failure mode must remain caught — 24 BAY in a
			// backplane title should still route to other.
			name: "production backplane stays accessory",
			title: "DELL EMC POWEREDGE R740xd 24 BAY SFF SERVER BACKPLANE " +
				"K2Y8N7 58D2W P1MJ3",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extract.IsAccessoryOnly(tt.title)
			assert.Equal(t, tt.want, got, "title=%q", tt.title)
		})
	}
}
