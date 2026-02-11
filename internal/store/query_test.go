package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func TestListingQuery_ToSQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		query         ListingQuery
		wantDataSQL   string
		wantCountSQL  string
		wantArgs      []any
		wantDataHas   []string // substrings that must appear in dataSQL
		wantDataNotIn []string // substrings that must NOT appear
	}{
		{
			name:  "empty query uses defaults",
			query: ListingQuery{},
			wantDataHas: []string{
				"FROM listings",
				"ORDER BY first_seen_at DESC",
				"LIMIT 50",
				"OFFSET 0",
			},
			wantDataNotIn: []string{"WHERE"},
			wantCountSQL:  "SELECT COUNT(*) FROM listings",
			wantArgs:      nil,
		},
		{
			name: "component type filter",
			query: ListingQuery{
				ComponentType: ptr("ram"),
			},
			wantDataHas: []string{
				"WHERE component_type = $1",
				"LIMIT 50",
			},
			wantCountSQL: "SELECT COUNT(*) FROM listings WHERE component_type = $1",
			wantArgs:     []any{"ram"},
		},
		{
			name: "min score filter",
			query: ListingQuery{
				MinScore: ptr(70),
			},
			wantDataHas:  []string{"WHERE score >= $1"},
			wantCountSQL: "SELECT COUNT(*) FROM listings WHERE score >= $1",
			wantArgs:     []any{70},
		},
		{
			name: "max score filter",
			query: ListingQuery{
				MaxScore: ptr(90),
			},
			wantDataHas:  []string{"WHERE score <= $1"},
			wantCountSQL: "SELECT COUNT(*) FROM listings WHERE score <= $1",
			wantArgs:     []any{90},
		},
		{
			name: "product key filter",
			query: ListingQuery{
				ProductKey: ptr("ram:ddr4:ecc_reg:32gb:2666"),
			},
			wantDataHas:  []string{"WHERE product_key = $1"},
			wantCountSQL: "SELECT COUNT(*) FROM listings WHERE product_key = $1",
			wantArgs:     []any{"ram:ddr4:ecc_reg:32gb:2666"},
		},
		{
			name: "seller minimum feedback filter",
			query: ListingQuery{
				SellerMinFB: ptr(100),
			},
			wantDataHas:  []string{"WHERE seller_feedback_score >= $1"},
			wantCountSQL: "SELECT COUNT(*) FROM listings WHERE seller_feedback_score >= $1",
			wantArgs:     []any{100},
		},
		{
			name: "single condition filter",
			query: ListingQuery{
				Conditions: []string{"new"},
			},
			wantDataHas:  []string{"WHERE condition_norm IN ($1)"},
			wantCountSQL: "SELECT COUNT(*) FROM listings WHERE condition_norm IN ($1)",
			wantArgs:     []any{"new"},
		},
		{
			name: "multiple conditions filter",
			query: ListingQuery{
				Conditions: []string{"new", "like_new", "used_working"},
			},
			wantDataHas:  []string{"WHERE condition_norm IN ($1, $2, $3)"},
			wantCountSQL: "SELECT COUNT(*) FROM listings WHERE condition_norm IN ($1, $2, $3)",
			wantArgs:     []any{"new", "like_new", "used_working"},
		},
		{
			name: "multiple filters with correct parameter numbering",
			query: ListingQuery{
				ComponentType: ptr("drive"),
				MinScore:      ptr(60),
				MaxScore:      ptr(95),
				SellerMinFB:   ptr(50),
			},
			wantDataHas: []string{
				"component_type = $1",
				"score >= $2",
				"score <= $3",
				"seller_feedback_score >= $4",
				" AND ",
			},
			wantCountSQL: "SELECT COUNT(*) FROM listings WHERE component_type = $1 AND score >= $2 AND score <= $3 AND seller_feedback_score >= $4",
			wantArgs:     []any{"drive", 60, 95, 50},
		},
		{
			name: "all filters combined",
			query: ListingQuery{
				ComponentType: ptr("cpu"),
				MinScore:      ptr(50),
				MaxScore:      ptr(100),
				ProductKey:    ptr("cpu:intel:xeon_e5-2680v4:14c:2.4ghz"),
				SellerMinFB:   ptr(200),
				Conditions:    []string{"new", "like_new"},
			},
			wantDataHas: []string{
				"component_type = $1",
				"score >= $2",
				"score <= $3",
				"product_key = $4",
				"seller_feedback_score >= $5",
				"condition_norm IN ($6, $7)",
			},
			wantArgs: []any{
				"cpu", 50, 100,
				"cpu:intel:xeon_e5-2680v4:14c:2.4ghz",
				200, "new", "like_new",
			},
		},
		{
			name: "order by score",
			query: ListingQuery{
				OrderBy: "score",
			},
			wantDataHas: []string{"ORDER BY score DESC NULLS LAST"},
		},
		{
			name: "order by price",
			query: ListingQuery{
				OrderBy: "price",
			},
			wantDataHas: []string{"ORDER BY price ASC"},
		},
		{
			name: "order by first_seen_at",
			query: ListingQuery{
				OrderBy: "first_seen_at",
			},
			wantDataHas: []string{"ORDER BY first_seen_at DESC"},
		},
		{
			name: "invalid order by falls back to default",
			query: ListingQuery{
				OrderBy: "DROP TABLE listings; --",
			},
			wantDataHas:   []string{"ORDER BY first_seen_at DESC"},
			wantDataNotIn: []string{"DROP TABLE"},
		},
		{
			name: "custom limit and offset",
			query: ListingQuery{
				Limit:  25,
				Offset: 100,
			},
			wantDataHas: []string{
				"LIMIT 25",
				"OFFSET 100",
			},
		},
		{
			name: "zero limit defaults to 50",
			query: ListingQuery{
				Limit: 0,
			},
			wantDataHas: []string{"LIMIT 50"},
		},
		{
			name: "negative limit defaults to 50",
			query: ListingQuery{
				Limit: -10,
			},
			wantDataHas: []string{"LIMIT 50"},
		},
		{
			name: "limit exceeding max is capped",
			query: ListingQuery{
				Limit: 1000,
			},
			wantDataHas: []string{"LIMIT 500"},
		},
		{
			name: "negative offset defaults to 0",
			query: ListingQuery{
				Offset: -5,
			},
			wantDataHas: []string{"OFFSET 0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := tt.query
			dataSQL, countSQL, args := q.ToSQL()

			for _, s := range tt.wantDataHas {
				assert.Contains(t, dataSQL, s, "dataSQL should contain %q", s)
			}

			for _, s := range tt.wantDataNotIn {
				assert.NotContains(t, dataSQL, s, "dataSQL should not contain %q", s)
			}

			if tt.wantCountSQL != "" {
				assert.Equal(t, tt.wantCountSQL, countSQL)
			}

			if tt.wantArgs != nil {
				require.Len(t, args, len(tt.wantArgs))
				assert.Equal(t, tt.wantArgs, args)
			} else if len(tt.wantArgs) == 0 {
				assert.Empty(t, args)
			}
		})
	}
}
