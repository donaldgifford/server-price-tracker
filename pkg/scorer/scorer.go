package score

import (
	"math"
)

// Weights defines the relative importance of each scoring factor.
type Weights struct {
	Price     float64
	Seller    float64
	Condition float64
	Quantity  float64
	Quality   float64
	Time      float64
}

// DefaultWeights returns the default scoring weights.
func DefaultWeights() Weights {
	return Weights{
		Price:     0.40,
		Seller:    0.20,
		Condition: 0.15,
		Quantity:  0.10,
		Quality:   0.10,
		Time:      0.05,
	}
}

// Baseline holds the percentile distribution for a product category.
type Baseline struct {
	P10         float64
	P25         float64
	P50         float64
	P75         float64
	P90         float64
	SampleCount int
}

// ListingData holds the fields needed for scoring (decoupled from DB model).
type ListingData struct {
	UnitPrice          float64
	SellerFeedback     int
	SellerFeedbackPct  float64
	SellerTopRated     bool
	Condition          string
	Quantity           int
	HasImages          bool
	HasItemSpecifics   bool
	DescriptionLen     int
	IsAuction          bool
	AuctionEndingSoon  bool // within 1 hour
	IsNewListing       bool // listed within last 2 hours
}

// Breakdown shows per-factor scores.
type Breakdown struct {
	Price     float64 `json:"price"`
	Seller    float64 `json:"seller"`
	Condition float64 `json:"condition"`
	Quantity  float64 `json:"quantity"`
	Quality   float64 `json:"quality"`
	Time      float64 `json:"time"`
	Total     int     `json:"total"`
}

// Score computes the composite deal score for a listing.
func Score(data ListingData, baseline *Baseline, w Weights) Breakdown {
	b := Breakdown{}

	// Price percentile score
	if baseline != nil && baseline.SampleCount >= 10 {
		b.Price = priceScore(data.UnitPrice, baseline)
	} else {
		b.Price = 50 // neutral when no baseline
	}

	// Seller trust score
	b.Seller = sellerScore(data)

	// Condition score
	b.Condition = conditionScore(data.Condition)

	// Quantity / lot value score
	b.Quantity = quantityScore(data)

	// Listing quality score
	b.Quality = qualityScore(data)

	// Time pressure score
	b.Time = timeScore(data)

	// Weighted composite
	total := b.Price*w.Price +
		b.Seller*w.Seller +
		b.Condition*w.Condition +
		b.Quantity*w.Quantity +
		b.Quality*w.Quality +
		b.Time*w.Time

	b.Total = int(math.Round(total))
	if b.Total > 100 {
		b.Total = 100
	}
	if b.Total < 0 {
		b.Total = 0
	}

	return b
}

// priceScore maps unit price to a 0-100 score based on percentile position.
func priceScore(unitPrice float64, b *Baseline) float64 {
	switch {
	case unitPrice <= b.P10:
		return 100
	case unitPrice <= b.P25:
		return lerp(unitPrice, b.P10, b.P25, 100, 85)
	case unitPrice <= b.P50:
		return lerp(unitPrice, b.P25, b.P50, 85, 50)
	case unitPrice <= b.P75:
		return lerp(unitPrice, b.P50, b.P75, 50, 25)
	case unitPrice <= b.P90:
		return lerp(unitPrice, b.P75, b.P90, 25, 0)
	default:
		return 0
	}
}

// sellerScore evaluates seller trustworthiness.
func sellerScore(d ListingData) float64 {
	// Feedback score component (how many transactions)
	var fbScore float64
	switch {
	case d.SellerFeedback >= 5000:
		fbScore = 100
	case d.SellerFeedback >= 500:
		fbScore = 70
	case d.SellerFeedback >= 100:
		fbScore = 40
	default:
		fbScore = 10
	}

	// Feedback percentage component
	var pctScore float64
	switch {
	case d.SellerFeedbackPct >= 99.5:
		pctScore = 100
	case d.SellerFeedbackPct >= 98.0:
		pctScore = 80
	case d.SellerFeedbackPct >= 95.0:
		pctScore = 50
	default:
		pctScore = 0
	}

	// Combine with top-rated bonus
	score := (fbScore + pctScore) / 2
	if d.SellerTopRated {
		score = math.Min(score+20, 100)
	}

	return score
}

// conditionScore maps normalized condition to a score.
func conditionScore(condition string) float64 {
	switch condition {
	case "new":
		return 100
	case "like_new":
		return 90
	case "used_working":
		return 70
	case "for_parts":
		return 10
	default:
		return 40
	}
}

// quantityScore rewards good per-unit value in lots.
// Single items get a neutral score; lots are evaluated on whether
// the per-unit pricing typically beats single-item prices.
func quantityScore(d ListingData) float64 {
	if d.Quantity <= 1 {
		return 50 // neutral for single items
	}
	// Lots inherently indicate better per-unit value
	// The actual per-unit price comparison happens in the price score
	// This factor rewards the convenience of bulk purchasing
	switch {
	case d.Quantity >= 16:
		return 90
	case d.Quantity >= 8:
		return 80
	case d.Quantity >= 4:
		return 70
	default:
		return 60
	}
}

// qualityScore evaluates how well-described the listing is.
func qualityScore(d ListingData) float64 {
	score := 0.0

	if d.HasImages {
		score += 40
	}
	if d.HasItemSpecifics {
		score += 30
	}

	// Description length heuristic
	switch {
	case d.DescriptionLen > 500:
		score += 30
	case d.DescriptionLen > 200:
		score += 20
	case d.DescriptionLen > 50:
		score += 10
	}

	return math.Min(score, 100)
}

// timeScore adds urgency for auction endings and new BIN listings.
func timeScore(d ListingData) float64 {
	if d.IsAuction && d.AuctionEndingSoon {
		return 100
	}
	if d.IsNewListing {
		return 80 // fresh BIN listing, grab it before others
	}
	return 30
}

// lerp linearly interpolates a value between two score boundaries.
func lerp(val, minVal, maxVal, minScore, maxScore float64) float64 {
	if maxVal == minVal {
		return minScore
	}
	t := (val - minVal) / (maxVal - minVal)
	return minScore + t*(maxScore-minScore)
}
