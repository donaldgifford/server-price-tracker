package extract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestDetectSystemTypeFromTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  domain.ComponentType
	}{
		// Workstation positives — chassis + system signal
		{
			name:  "HP Z8 G4 with Gold 6148 and 256GB",
			title: "HP Z8 G4 Workstation 40 Cores 2x Gold 6148 256GB P2000 512GB SSD + 1TB SSD Win11",
			want:  domain.ComponentWorkstation,
		},
		{
			name:  "HP Z6 G4 with Gold 6148",
			title: "HP Z6 G4 Workstation 20-Core Gold 6148 2.4GHz 64GB RAM P4000 512GB SSD Win11",
			want:  domain.ComponentWorkstation,
		},
		{
			name:  "ThinkStation P920 with Gold 6148",
			title: "Lenovo ThinkStation P920 40 Core Workstation 2X Gold 6148 192GB 1125W No GPU HDD",
			want:  domain.ComponentWorkstation,
		},
		{
			name:  "Dell Precision T7920 with Xeon",
			title: "Dell Precision T7920 Xeon Gold 6248R 256GB RAM 1TB NVMe Win11",
			want:  domain.ComponentWorkstation,
		},
		{
			name:  "Dell Pro Max with Threadripper",
			title: "Dell Pro Max workstation Threadripper Pro 5995WX 256GB RAM",
			want:  domain.ComponentWorkstation,
		},

		// Desktop positives — chassis + system signal
		{
			name:  "EliteDesk 800 G2 SFF with i5-6500 and SSD (the bundled-cable failure mode)",
			title: "HP EliteDesk 800 G2 SFF i5-6500 CPU, DDR4 8GB, 256GB SSD Win11 Pro + Power Cable",
			want:  domain.ComponentDesktop,
		},
		{
			name:  "OptiPlex 7080 with i7",
			title: "Dell OptiPlex 7080 Micro i7-10700T 16GB 512GB SSD",
			want:  domain.ComponentDesktop,
		},
		{
			name:  "ThinkCentre M920 with Win11",
			title: "Lenovo ThinkCentre M920 SFF i5-8500 16GB DDR4 512GB SSD Win11 Pro",
			want:  domain.ComponentDesktop,
		},
		{
			name:  "ProDesk 600 with Win10",
			title: "HP ProDesk 600 G3 SFF i5-7500 8GB 256GB SSD Win10 Pro",
			want:  domain.ComponentDesktop,
		},

		// Negatives — chassis only, no system signal → defer
		{
			name:  "ThinkStation P920 power cable defers to LLM",
			title: "Lenovo ThinkStation P920 power cable replacement",
			want:  "",
		},
		{
			name:  "HP Z8 motherboard alone defers",
			title: "HP Z8 G4 motherboard for repair",
			want:  "",
		},
		{
			name:  "EliteDesk bezel alone defers",
			title: "HP EliteDesk 800 front bezel replacement",
			want:  "",
		},

		// Negatives — no chassis token at all
		{
			name:  "PowerEdge server with Gold (real server)",
			title: "Dell PowerEdge R740xd 2U Server 2x Gold 6248 256GB RAM",
			want:  "",
		},
		{
			name:  "RAM-only listing",
			title: "Samsung 32GB DDR4 ECC RDIMM 2666MHz",
			want:  "",
		},
		{
			name:  "GPU-only listing",
			title: "NVIDIA Tesla P40 24GB GDDR5 GPU Accelerator",
			want:  "",
		},

		// Edge cases
		{
			name:  "empty title",
			title: "",
			want:  "",
		},
		{
			name:  "chassis-only Precision number defers",
			title: "Dell Precision T7920 chassis only",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extract.DetectSystemTypeFromTitle(tt.title)
			assert.Equal(t, tt.want, got, "title=%q", tt.title)
		})
	}
}

func TestDetectSystemTypeFromSpecifics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		specs map[string]string
		want  domain.ComponentType
	}{
		// Workstation hits via Most Suitable For
		{
			name:  "most suitable for workstation",
			specs: map[string]string{"Most Suitable For": "Workstation"},
			want:  domain.ComponentWorkstation,
		},
		{
			name:  "most suitable for workstation lowercase",
			specs: map[string]string{"most suitable for": "workstation use"},
			want:  domain.ComponentWorkstation,
		},

		// Workstation hits via Series
		{
			name:  "series ThinkStation",
			specs: map[string]string{"Series": "ThinkStation P-Series"},
			want:  domain.ComponentWorkstation,
		},
		{
			name:  "series Z by HP",
			specs: map[string]string{"Series": "Z by HP"},
			want:  domain.ComponentWorkstation,
		},
		{
			name:  "series Dell Precision",
			specs: map[string]string{"Series": "Dell Precision"},
			want:  domain.ComponentWorkstation,
		},

		// Workstation hits via Product Line
		{
			name:  "product line Precision Tower",
			specs: map[string]string{"Product Line": "Precision Tower"},
			want:  domain.ComponentWorkstation,
		},
		{
			name:  "product line Pro Max",
			specs: map[string]string{"Product Line": "Pro Max"},
			want:  domain.ComponentWorkstation,
		},

		// Desktop hits via Series / Product Line
		{
			name:  "series OptiPlex",
			specs: map[string]string{"Series": "OptiPlex"},
			want:  domain.ComponentDesktop,
		},
		{
			name:  "series ThinkCentre",
			specs: map[string]string{"Series": "ThinkCentre"},
			want:  domain.ComponentDesktop,
		},
		{
			name:  "series EliteDesk",
			specs: map[string]string{"Series": "EliteDesk"},
			want:  domain.ComponentDesktop,
		},

		// Generic Pro line — desktop only with form-factor co-token.
		{
			name: "Pro line + tower form factor → desktop",
			specs: map[string]string{
				"Product Line": "Pro",
				"Form Factor":  "Tower",
			},
			want: domain.ComponentDesktop,
		},
		{
			name: "Pro line + SFF → desktop",
			specs: map[string]string{
				"Product Line": "Pro",
				"Form Factor":  "SFF",
			},
			want: domain.ComponentDesktop,
		},
		{
			name:  "Pro line without form factor → empty (defer to LLM)",
			specs: map[string]string{"Product Line": "Pro"},
			want:  "",
		},

		// Workstation precedes desktop when both tokens appear.
		{
			name: "ThinkStation in series wins over generic Pro",
			specs: map[string]string{
				"Series":       "ThinkStation",
				"Product Line": "Pro",
			},
			want: domain.ComponentWorkstation,
		},

		// Empty / missing fields
		{
			name:  "empty map → empty",
			specs: map[string]string{},
			want:  "",
		},
		{
			name:  "nil map → empty",
			specs: nil,
			want:  "",
		},
		{
			name:  "unrelated specs → empty",
			specs: map[string]string{"Brand": "Dell", "Color": "Black"},
			want:  "",
		},
		{
			name:  "server specs → empty (defer to LLM)",
			specs: map[string]string{"Series": "PowerEdge", "Form Factor": "2U"},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extract.DetectSystemTypeFromSpecifics(tt.specs)
			assert.Equal(t, tt.want, got, "specs=%v", tt.specs)
		})
	}
}
