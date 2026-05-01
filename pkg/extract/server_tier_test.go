package extract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
)

func TestDetectServerTier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  string
	}{
		// Barebone — explicit markers
		{
			name:  "barebone keyword",
			title: "Dell PowerEdge R740XD 24-Bay SFF Barebone Server",
			want:  extract.ServerTierBarebone,
		},
		{
			name:  "barbone misspelling",
			title: "Dell PowerEdge R740 16 Bay SFF Server Barbone - M/B",
			want:  extract.ServerTierBarebone,
		},
		{
			name:  "no CPU / no RAM / no HDDs",
			title: "Dell PowerEdge R640 8SFF Barebone No CPU/RAM/HDD",
			want:  extract.ServerTierBarebone,
		},
		{
			name:  "without cpu",
			title: "Dell PowerEdge R740 without CPU or RAM",
			want:  extract.ServerTierBarebone,
		},
		{
			name:  "no ram lowercase",
			title: "Dell R740XD 1x Xeon Gold 6212U H740P iDRAC9 No RAM No HDD",
			want:  extract.ServerTierBarebone,
		},
		{
			name:  "cto",
			title: "Dell PowerEdge R740XD 12x3.5 Bay CTO Server",
			want:  extract.ServerTierBarebone,
		},

		// Configured — has both CPU model and RAM context
		{
			name:  "xeon gold plus ddr4",
			title: "Dell R740 2x Xeon Gold 5118 64GB DDR4 RAM",
			want:  extract.ServerTierConfigured,
		},
		{
			name:  "epyc plus rdimm",
			title: "Supermicro AS-1023US AMD EPYC 7551 256GB RDIMM",
			want:  extract.ServerTierConfigured,
		},
		{
			name:  "ram size with ram word",
			title: "HP ProLiant DL380 with Gold 6130 and 128GB RAM",
			want:  extract.ServerTierConfigured,
		},

		// Partial — one of CPU/RAM, or only ambiguous signals
		{
			name:  "cpu only",
			title: "Dell PowerEdge R740XD 1X XEON GOLD 6212U 2.4 GHz No RAM No HDD",
			want:  extract.ServerTierBarebone, // "No RAM" is a barebone marker
		},
		{
			name:  "cpu only no barebone marker",
			title: "Dell R640 with Intel Xeon Gold 5118 2.3GHz",
			want:  extract.ServerTierPartial,
		},
		{
			name:  "ram only",
			title: "Dell R740 with 64GB DDR4 RDIMM",
			want:  extract.ServerTierPartial,
		},
		{
			name:  "no signals",
			title: "Dell PowerEdge R740XD Server",
			want:  extract.ServerTierPartial,
		},
		{
			name:  "form factor only",
			title: "Dell 2U Server R640",
			want:  extract.ServerTierPartial,
		},

		// Adversarial — drive capacity must NOT be read as RAM
		{
			name:  "1tb drive is not ram",
			title: "Dell R740 with 1TB SSD and Xeon Silver 4110",
			want:  extract.ServerTierPartial, // CPU yes, RAM no (1TB is the drive)
		},
		{
			name:  "32gb without ram word is not ram",
			title: "Dell R640 server 32gb storage",
			want:  extract.ServerTierPartial, // 32gb without ram/memory/ecc/etc context
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extract.DetectServerTier(tt.title)
			assert.Equal(t, tt.want, got, "title=%q", tt.title)
		})
	}
}
