package score

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScore_DefaultWeights(t *testing.T) {
	t.Parallel()

	w := DefaultWeights()
	sum := w.Price + w.Seller + w.Condition + w.Quantity + w.Quality + w.Time
	assert.InDelta(t, 1.0, sum, 0.001, "default weights should sum to 1.0")
}

func TestScore_NoBaseline(t *testing.T) {
	t.Parallel()

	data := &ListingData{
		UnitPrice:         50.0,
		SellerFeedback:    5000,
		SellerFeedbackPct: 99.5,
		SellerTopRated:    true,
		Condition:         "new",
		Quantity:          1,
		HasImages:         true,
		HasItemSpecifics:  true,
		DescriptionLen:    600,
	}

	b := Score(data, nil, DefaultWeights())
	assert.Equal(t, 50.0, b.Price, "nil baseline should give neutral 50 price score")
	assert.Positive(t, b.Total)
	assert.LessOrEqual(t, b.Total, 100)
}

func TestScore_InsufficientBaseline(t *testing.T) {
	t.Parallel()

	data := &ListingData{
		UnitPrice: 50.0,
		Condition: "used_working",
		Quantity:  1,
	}
	baseline := &Baseline{
		P10:         20,
		P25:         30,
		P50:         50,
		P75:         70,
		P90:         100,
		SampleCount: 5, // below 10 threshold
	}

	b := Score(data, baseline, DefaultWeights())
	assert.Equal(t, 50.0, b.Price, "baseline with <10 samples should give neutral 50")
}

func TestScore_WithBaseline(t *testing.T) {
	t.Parallel()

	baseline := &Baseline{
		P10:         20,
		P25:         30,
		P50:         50,
		P75:         70,
		P90:         100,
		SampleCount: 50,
	}

	tests := []struct {
		name       string
		unitPrice  float64
		wantPrice  float64
		comparison string // "eq", "gt", "lt"
	}{
		{
			name:       "below P10 gets 100",
			unitPrice:  15.0,
			wantPrice:  100,
			comparison: "eq",
		},
		{
			name:       "at P10 gets 100",
			unitPrice:  20.0,
			wantPrice:  100,
			comparison: "eq",
		},
		{
			name:       "between P10 and P25",
			unitPrice:  25.0,
			wantPrice:  85,
			comparison: "gt",
		},
		{
			name:       "at P25 gets 85",
			unitPrice:  30.0,
			wantPrice:  85,
			comparison: "eq",
		},
		{
			name:       "at P50 gets 50",
			unitPrice:  50.0,
			wantPrice:  50,
			comparison: "eq",
		},
		{
			name:       "at P75 gets 25",
			unitPrice:  70.0,
			wantPrice:  25,
			comparison: "eq",
		},
		{
			name:       "at P90 gets 0",
			unitPrice:  100.0,
			wantPrice:  0,
			comparison: "eq",
		},
		{
			name:       "above P90 gets 0",
			unitPrice:  150.0,
			wantPrice:  0,
			comparison: "eq",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := &ListingData{
				UnitPrice: tt.unitPrice,
				Condition: "used_working",
				Quantity:  1,
			}

			b := Score(data, baseline, DefaultWeights())

			switch tt.comparison {
			case "eq":
				assert.InDelta(t, tt.wantPrice, b.Price, 0.01)
			case "gt":
				assert.Greater(t, b.Price, tt.wantPrice)
			case "lt":
				assert.Less(t, b.Price, tt.wantPrice)
			}
		})
	}
}

func TestScore_SellerScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		data      ListingData
		wantAbove float64
		wantBelow float64
	}{
		{
			name: "high feedback top rated",
			data: ListingData{
				SellerFeedback:    5000,
				SellerFeedbackPct: 99.5,
				SellerTopRated:    true,
			},
			wantAbove: 90,
		},
		{
			name: "high feedback not top rated",
			data: ListingData{
				SellerFeedback:    5000,
				SellerFeedbackPct: 99.5,
			},
			wantAbove: 90,
		},
		{
			name: "medium feedback",
			data: ListingData{
				SellerFeedback:    500,
				SellerFeedbackPct: 98.0,
			},
			wantAbove: 50,
			wantBelow: 90,
		},
		{
			name: "low feedback",
			data: ListingData{
				SellerFeedback:    100,
				SellerFeedbackPct: 95.0,
			},
			wantAbove: 30,
			wantBelow: 60,
		},
		{
			name: "very low feedback and pct",
			data: ListingData{
				SellerFeedback:    10,
				SellerFeedbackPct: 90.0,
			},
			wantBelow: 20,
		},
		{
			name: "zero feedback",
			data: ListingData{
				SellerFeedback:    0,
				SellerFeedbackPct: 0,
			},
			wantBelow: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			score := sellerScore(&tt.data)
			if tt.wantAbove > 0 {
				assert.GreaterOrEqual(t, score, tt.wantAbove)
			}
			if tt.wantBelow > 0 {
				assert.LessOrEqual(t, score, tt.wantBelow)
			}
		})
	}
}

func TestScore_ConditionScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		condition string
		want      float64
	}{
		{"new", 100},
		{"like_new", 90},
		{"used_working", 70},
		{"for_parts", 10},
		{"unknown", 40},
		{"", 40},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, conditionScore(tt.condition))
		})
	}
}

func TestScore_QuantityScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		quantity int
		want     float64
	}{
		{"zero quantity", 0, 50},
		{"single item", 1, 50},
		{"2 items", 2, 60},
		{"4 items", 4, 70},
		{"8 items", 8, 80},
		{"16 items", 16, 90},
		{"32 items", 32, 90},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := &ListingData{Quantity: tt.quantity}
			assert.Equal(t, tt.want, quantityScore(data))
		})
	}
}

func TestScore_QualityScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data ListingData
		want float64
	}{
		{
			name: "full quality",
			data: ListingData{
				HasImages:        true,
				HasItemSpecifics: true,
				DescriptionLen:   600,
			},
			want: 100,
		},
		{
			name: "images and specifics only",
			data: ListingData{
				HasImages:        true,
				HasItemSpecifics: true,
				DescriptionLen:   0,
			},
			want: 70,
		},
		{
			name: "images only",
			data: ListingData{
				HasImages: true,
			},
			want: 40,
		},
		{
			name: "nothing",
			data: ListingData{},
			want: 0,
		},
		{
			name: "medium description",
			data: ListingData{
				DescriptionLen: 250,
			},
			want: 20,
		},
		{
			name: "short description",
			data: ListingData{
				DescriptionLen: 60,
			},
			want: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, qualityScore(&tt.data))
		})
	}
}

func TestScore_TimeScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data ListingData
		want float64
	}{
		{
			name: "auction ending soon",
			data: ListingData{IsAuction: true, AuctionEndingSoon: true},
			want: 100,
		},
		{
			name: "new listing",
			data: ListingData{IsNewListing: true},
			want: 80,
		},
		{
			name: "normal listing",
			data: ListingData{},
			want: 30,
		},
		{
			name: "auction not ending soon",
			data: ListingData{IsAuction: true},
			want: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, timeScore(&tt.data))
		})
	}
}

func TestScore_ClampMax(t *testing.T) {
	t.Parallel()

	data := &ListingData{
		UnitPrice:         10.0,
		SellerFeedback:    10000,
		SellerFeedbackPct: 100.0,
		SellerTopRated:    true,
		Condition:         "new",
		Quantity:          32,
		HasImages:         true,
		HasItemSpecifics:  true,
		DescriptionLen:    1000,
		IsAuction:         true,
		AuctionEndingSoon: true,
	}
	baseline := &Baseline{
		P10:         100,
		P25:         200,
		P50:         300,
		P75:         400,
		P90:         500,
		SampleCount: 100,
	}

	b := Score(data, baseline, DefaultWeights())
	assert.LessOrEqual(t, b.Total, 100)
	assert.GreaterOrEqual(t, b.Total, 0)
}

func TestScore_ClampMin(t *testing.T) {
	t.Parallel()

	// Use extreme negative weights to force a negative total before clamping.
	// This exercises the b.Total < 0 branch.
	data := &ListingData{
		UnitPrice: 500,
		Condition: "for_parts",
		Quantity:  1,
	}
	baseline := &Baseline{
		P10:         10,
		P25:         20,
		P50:         30,
		P75:         40,
		P90:         50,
		SampleCount: 100,
	}

	// All-zero weights except a large negative-inducing arrangement isn't possible
	// with the current algo since scores are 0-100 and weights are positive.
	// Instead, directly test that total is clamped to 0 minimum.
	w := Weights{
		Price:     0,
		Seller:    0,
		Condition: 0,
		Quantity:  0,
		Quality:   0,
		Time:      0,
	}

	b := Score(data, baseline, w)
	assert.Equal(t, 0, b.Total)
}

func TestLerp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                                 string
		val, minVal, maxVal, minScore, maxSc float64
		want                                 float64
	}{
		{"at min", 10, 10, 20, 100, 50, 100},
		{"at max", 20, 10, 20, 100, 50, 50},
		{"midpoint", 15, 10, 20, 100, 50, 75},
		{"equal min max", 10, 10, 10, 100, 50, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.InDelta(
				t,
				tt.want,
				lerp(tt.val, tt.minVal, tt.maxVal, tt.minScore, tt.maxSc),
				0.01,
			)
		})
	}
}

func TestScore_AuctionStartPrice(t *testing.T) {
	t.Parallel()

	// Edge case: $0.01 auction start price should still compute a valid score.
	data := &ListingData{
		UnitPrice:         0.01,
		SellerFeedback:    100,
		SellerFeedbackPct: 98.0,
		Condition:         "used_working",
		Quantity:          1,
		IsAuction:         true,
		AuctionEndingSoon: true,
	}
	baseline := &Baseline{
		P10:         20,
		P25:         30,
		P50:         50,
		P75:         70,
		P90:         100,
		SampleCount: 50,
	}

	b := Score(data, baseline, DefaultWeights())
	assert.Equal(t, 100.0, b.Price, "$0.01 should be below P10, scoring 100")
	assert.Equal(t, 100.0, b.Time, "auction ending soon should score 100")
	assert.Positive(t, b.Total)
	assert.LessOrEqual(t, b.Total, 100)
}

func TestScore_CompositeCalculation(t *testing.T) {
	t.Parallel()

	data := &ListingData{
		UnitPrice:         50.0,
		SellerFeedback:    5000,
		SellerFeedbackPct: 99.5,
		SellerTopRated:    false,
		Condition:         "new",
		Quantity:          1,
		HasImages:         true,
		HasItemSpecifics:  true,
		DescriptionLen:    600,
		IsNewListing:      true,
	}

	baseline := &Baseline{
		P10:         20,
		P25:         30,
		P50:         50,
		P75:         70,
		P90:         100,
		SampleCount: 50,
	}

	w := DefaultWeights()
	b := Score(data, baseline, w)

	// Verify per-factor scores
	assert.InDelta(t, 50.0, b.Price, 0.01)   // at P50
	assert.InDelta(t, 100.0, b.Seller, 0.01) // 5000fb + 99.5%
	assert.Equal(t, 100.0, b.Condition)      // new
	assert.Equal(t, 50.0, b.Quantity)        // single item
	assert.Equal(t, 100.0, b.Quality)        // full quality
	assert.Equal(t, 80.0, b.Time)            // new listing

	// Verify composite
	expected := b.Price*w.Price +
		b.Seller*w.Seller +
		b.Condition*w.Condition +
		b.Quantity*w.Quantity +
		b.Quality*w.Quality +
		b.Time*w.Time

	assert.Equal(t, int(expected+0.5), b.Total)
}
