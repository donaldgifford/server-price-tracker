package extract_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
)

func TestPC4ToMHz(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  int
		ok    bool
	}{
		// DDR3 entries.
		{name: "DDR3 PC3-10600", input: "PC3-10600", want: 1333, ok: true},
		{name: "DDR3 PC3-12800", input: "PC3-12800", want: 1600, ok: true},
		{name: "DDR3 PC3-14900", input: "PC3-14900", want: 1866, ok: true},

		// DDR4 entries.
		{name: "DDR4 PC4-17000", input: "PC4-17000", want: 2133, ok: true},
		{name: "DDR4 PC4-19200", input: "PC4-19200", want: 2400, ok: true},
		{name: "DDR4 PC4-21300", input: "PC4-21300", want: 2666, ok: true},
		{name: "DDR4 PC4-23400", input: "PC4-23400", want: 2933, ok: true},
		{name: "DDR4 PC4-25600", input: "PC4-25600", want: 3200, ok: true},

		// DDR5 entries.
		{name: "DDR5 PC5-38400", input: "PC5-38400", want: 4800, ok: true},
		{name: "DDR5 PC5-44800", input: "PC5-44800", want: 5600, ok: true},
		{name: "DDR5 PC5-51200", input: "PC5-51200", want: 6400, ok: true},

		// Suffix handling.
		{name: "suffix V", input: "PC4-21300V", want: 2666, ok: true},
		{name: "suffix R", input: "PC4-19200R", want: 2400, ok: true},
		{name: "suffix T", input: "PC4-25600T", want: 3200, ok: true},
		{name: "suffix U", input: "PC4-17000U", want: 2133, ok: true},
		{name: "suffix E", input: "PC4-25600E", want: 3200, ok: true},

		// Case insensitivity.
		{name: "lowercase", input: "pc4-21300", want: 2666, ok: true},
		{name: "mixed case", input: "Pc4-19200", want: 2400, ok: true},

		// Raw bandwidth number (no prefix).
		{name: "raw bandwidth number", input: "21300", want: 2666, ok: true},

		// Unknown/invalid.
		{name: "unknown bandwidth", input: "PC4-99999", want: 0, ok: false},
		{name: "empty string", input: "", want: 0, ok: false},
		{name: "just prefix", input: "PC4-", want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := extract.PC4ToMHz(tt.input)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractSpeedFromTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  int
		ok    bool
	}{
		// PC module number matches.
		{
			name:  "Samsung DDR4 PC4-21300",
			title: "Samsung 32GB DDR4 PC4-21300 ECC REG",
			want:  2666,
			ok:    true,
		},
		{
			name:  "SK Hynix PC4-25600",
			title: "SK Hynix 64GB PC4-25600 DDR4-3200MHz",
			want:  3200,
			ok:    true,
		},
		{
			name:  "Samsung part number with PC4",
			title: "Samsung M393A4K40CB2-CTD 32GB PC4-21300",
			want:  2666,
			ok:    true,
		},
		{
			name:  "DDR3 PC3-12800",
			title: "Kingston DDR3 8GB PC3-12800 1600MHz",
			want:  1600,
			ok:    true,
		},
		{
			name:  "DDR5 PC5-38400",
			title: "Samsung DDR5 32GB PC5-38400",
			want:  4800,
			ok:    true,
		},

		// DDR speed regex fallback (no PC module number).
		{
			name:  "DDR4-2666 fallback",
			title: "Hynix 32GB DDR4-2666 ECC RDIMM",
			want:  2666,
			ok:    true,
		},
		{
			name:  "DDR5-4800 fallback",
			title: "Kingston 64GB DDR5-4800 ECC REG",
			want:  4800,
			ok:    true,
		},

		// No match cases.
		{
			name:  "no speed info at all",
			title: "LOT OF 4 Samsung 32GB DDR4 ECC REG",
			want:  0,
			ok:    false,
		},
		{
			name:  "plain text no speed",
			title: "Server RAM 32GB",
			want:  0,
			ok:    false,
		},
		{
			name:  "MHz alone not matched",
			title: "Crucial DDR4 2933MHz 32GB",
			want:  0,
			ok:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := extract.ExtractSpeedFromTitle(tt.title)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeRAMSpeed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		title     string
		attrs     map[string]any
		wantSpeed int
		wantOK    bool
	}{
		{
			name:      "fills from PC4-21300",
			title:     "Samsung 32GB DDR4 PC4-21300 ECC",
			attrs:     map[string]any{"generation": "DDR4"},
			wantSpeed: 2666,
			wantOK:    true,
		},
		{
			name:      "fills from PC4-25600V",
			title:     "Micron 16GB PC4-25600V ECC RDIMM",
			attrs:     map[string]any{"generation": "DDR4"},
			wantSpeed: 3200,
			wantOK:    true,
		},
		{
			name:      "fills when speed_mhz is nil",
			title:     "Kingston 8GB PC3-12800 DDR3",
			attrs:     map[string]any{"speed_mhz": nil},
			wantSpeed: 1600,
			wantOK:    true,
		},
		{
			name:      "does not overwrite existing int speed",
			title:     "Samsung 32GB PC4-21300 DDR4",
			attrs:     map[string]any{"speed_mhz": 2400},
			wantSpeed: 2400,
			wantOK:    true,
		},
		{
			name:      "does not overwrite existing float64 speed",
			title:     "Samsung 32GB PC4-21300 DDR4",
			attrs:     map[string]any{"speed_mhz": float64(2666)},
			wantSpeed: 2666,
			wantOK:    true,
		},
		{
			name:      "treats zero int as unset and fills",
			title:     "Samsung 32GB PC4-21300 DDR4 ECC",
			attrs:     map[string]any{"speed_mhz": 0},
			wantSpeed: 2666,
			wantOK:    true,
		},
		{
			name:      "no PC number and no speed returns false",
			title:     "Server RAM 32GB ECC",
			attrs:     map[string]any{"generation": "DDR4"},
			wantSpeed: 0,
			wantOK:    false,
		},
		{
			name:      "no PC number with zero speed returns false",
			title:     "Server RAM 32GB ECC",
			attrs:     map[string]any{"speed_mhz": 0},
			wantSpeed: 0,
			wantOK:    false,
		},
		{
			name:      "DDR speed fallback when no PC number",
			title:     "Hynix 32GB DDR4-2666 ECC RDIMM",
			attrs:     map[string]any{"generation": "DDR4"},
			wantSpeed: 2666,
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Copy attrs to avoid mutation between parallel subtests.
			attrs := make(map[string]any, len(tt.attrs))
			maps.Copy(attrs, tt.attrs)

			ok := extract.NormalizeRAMSpeed(tt.title, attrs)
			assert.Equal(t, tt.wantOK, ok)

			if tt.wantSpeed != 0 {
				got, exists := attrs["speed_mhz"]
				assert.True(t, exists, "speed_mhz should exist in attrs")
				// When attrs already held a float64, the value stays as float64.
				if _, isFloat := got.(float64); isFloat {
					assert.Equal(t, float64(tt.wantSpeed), got)
				} else {
					assert.Equal(t, tt.wantSpeed, got)
				}
			}
		})
	}
}
