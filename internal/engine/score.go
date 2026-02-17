// Package engine implements the core business logic loops:
// scoring, ingestion, baseline recomputation, and alert evaluation.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	score "github.com/donaldgifford/server-price-tracker/pkg/scorer"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ScoreListing computes and persists the deal score for a single listing.
// Returns nil if the listing has no product key (cannot be scored).
func ScoreListing(
	ctx context.Context,
	s store.Store,
	listing *domain.Listing,
) error {
	if listing.ProductKey == "" {
		return nil
	}

	baseline, err := s.GetBaseline(ctx, listing.ProductKey)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("getting baseline for %s: %w", listing.ProductKey, err)
	}

	data := buildListingData(listing)
	var scorerBaseline *score.Baseline
	if baseline != nil {
		scorerBaseline = &score.Baseline{
			P10:         baseline.P10,
			P25:         baseline.P25,
			P50:         baseline.P50,
			P75:         baseline.P75,
			P90:         baseline.P90,
			SampleCount: baseline.SampleCount,
		}
	}

	breakdown := score.Score(data, scorerBaseline, score.DefaultWeights())

	if scorerBaseline != nil && scorerBaseline.SampleCount >= score.MinBaselineSamples {
		metrics.ScoringWithBaselineTotal.Inc()
	} else {
		metrics.ScoringColdStartTotal.Inc()
	}

	breakdownJSON, err := json.Marshal(breakdown)
	if err != nil {
		return fmt.Errorf("marshaling breakdown: %w", err)
	}

	metrics.ScoringDistribution.Observe(float64(breakdown.Total))

	return s.UpdateScore(ctx, listing.ID, breakdown.Total, breakdownJSON)
}

// RescoreListings re-scores all unscored listings.
func RescoreListings(ctx context.Context, s store.Store, limit int) (int, error) {
	listings, err := s.ListUnscoredListings(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("listing unscored: %w", err)
	}

	return scoreAll(ctx, s, listings)
}

// RescoreByProductKey re-scores all listings matching a product key.
func RescoreByProductKey(
	ctx context.Context,
	s store.Store,
	productKey string,
) (int, error) {
	q := &store.ListingQuery{
		ProductKey: &productKey,
		Limit:      500,
	}
	listings, _, err := s.ListListings(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("listing by product key: %w", err)
	}

	return scoreAll(ctx, s, listings)
}

// RescoreAll re-scores all active listings.
func RescoreAll(ctx context.Context, s store.Store) (int, error) {
	q := &store.ListingQuery{
		Limit: 500,
	}
	listings, _, err := s.ListListings(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("listing all: %w", err)
	}

	return scoreAll(ctx, s, listings)
}

func scoreAll(ctx context.Context, s store.Store, listings []domain.Listing) (int, error) {
	var errs []error
	scored := 0

	for i := range listings {
		if err := ScoreListing(ctx, s, &listings[i]); err != nil {
			errs = append(errs, fmt.Errorf("scoring %s: %w", listings[i].ID, err))
			continue
		}
		scored++
	}

	return scored, errors.Join(errs...)
}

func buildListingData(l *domain.Listing) *score.ListingData {
	return &score.ListingData{
		UnitPrice:         l.UnitPrice(),
		SellerFeedback:    l.SellerFeedback,
		SellerFeedbackPct: l.SellerFeedbackPct,
		SellerTopRated:    l.SellerTopRated,
		Condition:         string(l.ConditionNorm),
		Quantity:          l.Quantity,
		HasImages:         l.ImageURL != "",
		HasItemSpecifics:  len(l.Attributes) > 0,
		IsAuction:         l.ListingType == domain.ListingAuction,
	}
}
