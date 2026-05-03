package extract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

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
