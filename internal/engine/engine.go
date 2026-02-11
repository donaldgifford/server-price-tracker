package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	"github.com/donaldgifford/server-price-tracker/internal/notify"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// Engine orchestrates ingestion, extraction, scoring, and alerting.
type Engine struct {
	store     store.Store
	ebay      ebay.EbayClient
	extractor extract.Extractor
	notifier  notify.Notifier
	log       *slog.Logger

	baselineWindowDays int
	staggerOffset      time.Duration
}

// NewEngine creates a new Engine with injected dependencies.
func NewEngine(
	s store.Store,
	e ebay.EbayClient,
	ex extract.Extractor,
	n notify.Notifier,
	opts ...EngineOption,
) *Engine {
	eng := &Engine{
		store:              s,
		ebay:               e,
		extractor:          ex,
		notifier:           n,
		log:                slog.Default(),
		baselineWindowDays: 90,
		staggerOffset:      30 * time.Second,
	}
	for _, opt := range opts {
		opt(eng)
	}
	return eng
}

// EngineOption configures the Engine.
type EngineOption func(*Engine)

// WithLogger sets a custom logger.
func WithLogger(l *slog.Logger) EngineOption {
	return func(e *Engine) {
		e.log = l
	}
}

// WithBaselineWindowDays sets the baseline computation window.
func WithBaselineWindowDays(days int) EngineOption {
	return func(e *Engine) {
		e.baselineWindowDays = days
	}
}

// WithStaggerOffset sets the delay between processing each watch.
func WithStaggerOffset(d time.Duration) EngineOption {
	return func(e *Engine) {
		e.staggerOffset = d
	}
}

// RunIngestion executes the full ingestion pipeline for all enabled watches.
func (eng *Engine) RunIngestion(ctx context.Context) error {
	start := time.Now()
	defer func() {
		metrics.IngestionDuration.Observe(time.Since(start).Seconds())
	}()

	watches, err := eng.store.ListWatches(ctx, true)
	if err != nil {
		return fmt.Errorf("listing watches: %w", err)
	}

	for i := range watches {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		w := &watches[i]
		eng.log.Info("processing watch", "name", w.Name, "id", w.ID)
		if err := eng.processWatch(ctx, w); err != nil {
			eng.log.Error("watch processing failed", "watch", w.Name, "error", err)
			metrics.IngestionErrorsTotal.Inc()
			continue
		}

		// Stagger between watches to avoid API bursts.
		if i < len(watches)-1 && eng.staggerOffset > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(eng.staggerOffset):
			}
		}
	}

	// Process alerts after ingestion.
	if err := ProcessAlerts(ctx, eng.store, eng.notifier); err != nil {
		eng.log.Error("alert processing failed", "error", err)
	}

	return nil
}

func (eng *Engine) processWatch(ctx context.Context, w *domain.Watch) error {
	resp, err := eng.ebay.Search(ctx, ebay.SearchRequest{
		Query:      w.SearchQuery,
		CategoryID: w.CategoryID,
	})
	if err != nil {
		return fmt.Errorf("searching eBay: %w", err)
	}

	for i := range resp.Items {
		listing := convertItemSummary(&resp.Items[i])

		if err := eng.store.UpsertListing(ctx, listing); err != nil {
			eng.log.Error("upsert failed", "ebay_id", listing.EbayID, "error", err)
			continue
		}

		metrics.IngestionListingsTotal.Inc()

		// Extract attributes.
		ct, attrs, extractErr := eng.extractor.ClassifyAndExtract(
			ctx, listing.Title, nil,
		)
		if extractErr != nil {
			eng.log.Error("extraction failed", "listing", listing.EbayID, "error", extractErr)
			metrics.ExtractionFailuresTotal.Inc()
			continue
		}

		productKey := extract.ProductKey(string(ct), attrs)
		if err := eng.store.UpdateListingExtraction(
			ctx, listing.ID, string(ct), attrs, 0.9, productKey,
		); err != nil {
			eng.log.Error("update extraction failed", "listing", listing.EbayID, "error", err)
			continue
		}

		// Update the listing in-place for scoring.
		listing.ProductKey = productKey
		listing.ComponentType = ct

		// Score the listing.
		if err := ScoreListing(ctx, eng.store, listing); err != nil {
			eng.log.Error("scoring failed", "listing", listing.EbayID, "error", err)
			continue
		}

		// Evaluate alert.
		eng.evaluateAlert(ctx, w, listing)
	}

	return nil
}

func (eng *Engine) evaluateAlert(
	ctx context.Context,
	w *domain.Watch,
	listing *domain.Listing,
) {
	if listing.Score == nil || *listing.Score < w.ScoreThreshold {
		return
	}

	if !w.Filters.Match(listing) {
		return
	}

	alert := &domain.Alert{
		WatchID:   w.ID,
		ListingID: listing.ID,
		Score:     *listing.Score,
	}

	if err := eng.store.CreateAlert(ctx, alert); err != nil {
		eng.log.Error("creating alert failed", "listing", listing.ID, "error", err)
	}
}

// RunBaselineRefresh recomputes all baselines and re-scores affected listings.
func (eng *Engine) RunBaselineRefresh(ctx context.Context) error {
	if err := eng.store.RecomputeAllBaselines(ctx, eng.baselineWindowDays); err != nil {
		return fmt.Errorf("recomputing baselines: %w", err)
	}

	_, err := RescoreAll(ctx, eng.store)
	if err != nil {
		return fmt.Errorf("re-scoring after baseline refresh: %w", err)
	}

	return nil
}

func convertItemSummary(item *ebay.ItemSummary) *domain.Listing {
	l := &domain.Listing{
		EbayID:       item.ItemID,
		Title:        item.Title,
		ItemURL:      item.ItemWebURL,
		Currency:     item.Price.Currency,
		ConditionRaw: item.Condition,
		Quantity:     1,
	}

	if p, err := strconv.ParseFloat(item.Price.Value, 64); err == nil {
		l.Price = p
	}

	if item.Image != nil {
		l.ImageURL = item.Image.ImageURL
	}

	if item.Seller != nil {
		l.SellerName = item.Seller.Username
		l.SellerFeedback = item.Seller.FeedbackScore
		if pct, err := strconv.ParseFloat(item.Seller.FeedbackPercentage, 64); err == nil {
			l.SellerFeedbackPct = pct
		}
	}

	l.SellerTopRated = item.TopRatedBuyingExperience

	if len(item.ShippingOptions) > 0 && item.ShippingOptions[0].ShippingCost != nil {
		if sc, err := strconv.ParseFloat(
			item.ShippingOptions[0].ShippingCost.Value, 64,
		); err == nil {
			l.ShippingCost = &sc
		}
	}

	return l
}
