package extract_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestNormalizeCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want domain.Condition
	}{
		// new
		{name: "New", raw: "New", want: domain.ConditionNew},
		{name: "Brand New", raw: "Brand New", want: domain.ConditionNew},
		{name: "Factory Sealed", raw: "Factory Sealed", want: domain.ConditionNew},
		{name: "new lowercase", raw: "new", want: domain.ConditionNew},
		// like_new
		{name: "Open Box", raw: "Open Box", want: domain.ConditionLikeNew},
		{
			name: "Manufacturer Refurbished",
			raw:  "Manufacturer Refurbished",
			want: domain.ConditionLikeNew,
		},
		// used_working
		{name: "Used", raw: "Used", want: domain.ConditionUsedWorking},
		{name: "Pre-Owned", raw: "Pre-Owned", want: domain.ConditionUsedWorking},
		{name: "Seller Refurbished", raw: "Seller Refurbished", want: domain.ConditionUsedWorking},
		{
			name: "Pulled from Working",
			raw:  "Pulled from Working",
			want: domain.ConditionUsedWorking,
		},
		{name: "Tested Working", raw: "Tested Working", want: domain.ConditionUsedWorking},
		// for_parts
		{name: "For Parts", raw: "For Parts", want: domain.ConditionForParts},
		{name: "Not Working", raw: "Not Working", want: domain.ConditionForParts},
		{name: "Parts Only", raw: "Parts Only", want: domain.ConditionForParts},
		{name: "As-Is", raw: "As-Is", want: domain.ConditionForParts},
		// unknown
		{name: "Something Random", raw: "Something Random", want: domain.ConditionUnknown},
		{name: "empty string", raw: "", want: domain.ConditionUnknown},
		{name: "whitespace only", raw: "   ", want: domain.ConditionUnknown},
		// case insensitive
		{name: "USED uppercase", raw: "USED", want: domain.ConditionUsedWorking},
		{name: "for parts mixed case", raw: "For PARTS", want: domain.ConditionForParts},
		// trimming
		{name: "leading/trailing whitespace", raw: "  New  ", want: domain.ConditionNew},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extract.NormalizeCondition(tt.raw)
			assert.Equal(t, tt.want, got)
		})
	}
}
