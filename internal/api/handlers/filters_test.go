package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func ptr[T any](v T) *T { return &v }

func TestParseFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		filters []string
		want    domain.WatchFilters
		wantErr string
	}{
		{
			name:    "empty filters",
			filters: nil,
			want:    domain.WatchFilters{},
		},
		{
			name:    "price_max",
			filters: []string{"price_max=100.50"},
			want:    domain.WatchFilters{PriceMax: ptr(100.50)},
		},
		{
			name:    "price_min",
			filters: []string{"price_min=10.00"},
			want:    domain.WatchFilters{PriceMin: ptr(10.0)},
		},
		{
			name:    "seller_min_feedback",
			filters: []string{"seller_min_feedback=500"},
			want:    domain.WatchFilters{SellerMinFeedback: ptr(500)},
		},
		{
			name:    "seller_min_feedback_pct",
			filters: []string{"seller_min_feedback_pct=95.5"},
			want:    domain.WatchFilters{SellerMinFeedbackPct: ptr(95.5)},
		},
		{
			name:    "seller_top_rated_only",
			filters: []string{"seller_top_rated_only=true"},
			want:    domain.WatchFilters{SellerTopRatedOnly: true},
		},
		{
			name:    "conditions single",
			filters: []string{"conditions=used_working"},
			want: domain.WatchFilters{
				Conditions: []domain.Condition{domain.ConditionUsedWorking},
			},
		},
		{
			name:    "conditions multiple",
			filters: []string{"conditions=used_working,new"},
			want: domain.WatchFilters{
				Conditions: []domain.Condition{
					domain.ConditionUsedWorking,
					domain.ConditionNew,
				},
			},
		},
		{
			name:    "attr numeric exact match",
			filters: []string{"attr:capacity_gb=32"},
			want: domain.WatchFilters{
				AttributeFilters: map[string]domain.AttributeFilter{
					"capacity_gb": {Equals: float64(32)},
				},
			},
		},
		{
			name:    "attr string exact match with eq prefix",
			filters: []string{"attr:ddr_gen=eq:ddr4"},
			want: domain.WatchFilters{
				AttributeFilters: map[string]domain.AttributeFilter{
					"ddr_gen": {Equals: "ddr4"},
				},
			},
		},
		{
			name:    "attr min range",
			filters: []string{"attr:speed_mhz=min:2400"},
			want: domain.WatchFilters{
				AttributeFilters: map[string]domain.AttributeFilter{
					"speed_mhz": {Min: ptr(2400.0)},
				},
			},
		},
		{
			name:    "attr max range",
			filters: []string{"attr:speed_mhz=max:3200"},
			want: domain.WatchFilters{
				AttributeFilters: map[string]domain.AttributeFilter{
					"speed_mhz": {Max: ptr(3200.0)},
				},
			},
		},
		{
			name: "multiple filters combined",
			filters: []string{
				"price_max=100",
				"seller_min_feedback=500",
				"conditions=used_working,new",
				"attr:capacity_gb=32",
			},
			want: domain.WatchFilters{
				PriceMax:          ptr(100.0),
				SellerMinFeedback: ptr(500),
				Conditions: []domain.Condition{
					domain.ConditionUsedWorking,
					domain.ConditionNew,
				},
				AttributeFilters: map[string]domain.AttributeFilter{
					"capacity_gb": {Equals: float64(32)},
				},
			},
		},
		{
			name:    "invalid filter format",
			filters: []string{"no-equals-sign"},
			wantErr: "invalid filter format",
		},
		{
			name:    "unknown filter key",
			filters: []string{"unknown_key=value"},
			wantErr: "unknown filter key",
		},
		{
			name:    "invalid price_max",
			filters: []string{"price_max=not-a-number"},
			wantErr: "invalid price_max",
		},
		{
			name:    "invalid seller_min_feedback",
			filters: []string{"seller_min_feedback=abc"},
			wantErr: "invalid seller_min_feedback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseFilters(tt.filters)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
