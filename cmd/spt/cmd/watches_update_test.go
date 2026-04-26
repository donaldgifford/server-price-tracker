package cmd

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestApplyFilterUpdates(t *testing.T) {
	t.Parallel()

	priceMax := 500.0

	current := domain.WatchFilters{
		PriceMax: &priceMax,
		AttributeFilters: map[string]domain.AttributeFilter{
			"capacity_gb": {Equals: float64(16)},
			"ddr_gen":     {Equals: "ddr4"},
		},
	}

	tests := []struct {
		name      string
		filter    []string
		addFilter []string
		clear     bool
		wantErr   string
		assert    func(t *testing.T, got domain.WatchFilters)
	}{
		{
			name: "no flags preserves current",
			assert: func(t *testing.T, got domain.WatchFilters) {
				t.Helper()
				require.NotNil(t, got.PriceMax)
				assert.InDelta(t, 500.0, *got.PriceMax, 0.001)
				assert.Len(t, got.AttributeFilters, 2)
				assert.Equal(t, float64(16), got.AttributeFilters["capacity_gb"].Equals)
			},
		},
		{
			name:   "filter replaces entire block",
			filter: []string{"attr:capacity_gb=eq:32"},
			assert: func(t *testing.T, got domain.WatchFilters) {
				t.Helper()
				assert.Nil(t, got.PriceMax, "standard filters dropped on replace")
				assert.Len(t, got.AttributeFilters, 1, "old ddr_gen dropped on replace")
				assert.Equal(t, "32", got.AttributeFilters["capacity_gb"].Equals)
			},
		},
		{
			name:      "add-filter merges into existing AttributeFilters",
			addFilter: []string{"attr:speed_mhz=min:2400"},
			assert: func(t *testing.T, got domain.WatchFilters) {
				t.Helper()
				require.NotNil(t, got.PriceMax, "standard filters preserved on merge")
				assert.Len(t, got.AttributeFilters, 3, "merge keeps capacity_gb, ddr_gen, adds speed_mhz")
				require.NotNil(t, got.AttributeFilters["speed_mhz"].Min)
				assert.InDelta(t, 2400.0, *got.AttributeFilters["speed_mhz"].Min, 0.001)
			},
		},
		{
			name:      "add-filter overwrites existing key",
			addFilter: []string{"attr:capacity_gb=eq:64"},
			assert: func(t *testing.T, got domain.WatchFilters) {
				t.Helper()
				assert.Len(t, got.AttributeFilters, 2, "still capacity_gb + ddr_gen, no new keys")
				assert.Equal(t, "64", got.AttributeFilters["capacity_gb"].Equals,
					"capacity_gb overwritten with new value")
				assert.Equal(t, "ddr4", got.AttributeFilters["ddr_gen"].Equals,
					"ddr_gen left intact")
			},
		},
		{
			name:  "clear-filters empties everything",
			clear: true,
			assert: func(t *testing.T, got domain.WatchFilters) {
				t.Helper()
				assert.Equal(t, domain.WatchFilters{}, got)
			},
		},
		{
			name:      "filter and add-filter together error",
			filter:    []string{"attr:capacity_gb=eq:32"},
			addFilter: []string{"attr:speed_mhz=min:2400"},
			wantErr:   "mutually exclusive",
		},
		{
			name:    "filter and clear together error",
			filter:  []string{"attr:capacity_gb=eq:32"},
			clear:   true,
			wantErr: "mutually exclusive",
		},
		{
			name:      "add-filter and clear together error",
			addFilter: []string{"attr:capacity_gb=eq:32"},
			clear:     true,
			wantErr:   "mutually exclusive",
		},
		{
			name:    "invalid filter syntax surfaces parse error",
			filter:  []string{"not-a-valid-filter-format"},
			wantErr: "parsing --filter",
		},
		{
			name:      "invalid add-filter syntax surfaces parse error",
			addFilter: []string{"not-a-valid-filter-format"},
			wantErr:   "parsing --add-filter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Each subtest gets its own deep copy so map mutations don't leak
			// across t.Parallel() runs.
			workingCopy := domain.WatchFilters{
				PriceMax:         current.PriceMax,
				AttributeFilters: maps.Clone(current.AttributeFilters),
			}

			got, err := applyFilterUpdates(workingCopy, tt.filter, tt.addFilter, tt.clear)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			tt.assert(t, got)
		})
	}
}
