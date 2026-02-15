package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	"github.com/donaldgifford/server-price-tracker/internal/notify"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

const defaultMaxCallsPerCycle = 50

// Engine orchestrates ingestion, extraction, scoring, and alerting.
type Engine struct {
	store     store.Store
	ebay      ebay.EbayClient
	extractor extract.Extractor
	notifier  notify.Notifier
	log       *slog.Logger

	paginator          *ebay.Paginator
	maxCallsPerCycle   int
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
		maxCallsPerCycle:   defaultMaxCallsPerCycle,
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

// WithPaginator sets the paginator for multi-page eBay searches.
func WithPaginator(p *ebay.Paginator) EngineOption {
	return func(e *Engine) {
		e.paginator = p
	}
}

// WithMaxCallsPerCycle sets the maximum API pages per ingestion cycle.
func WithMaxCallsPerCycle(n int) EngineOption {
	return func(e *Engine) {
		e.maxCallsPerCycle = n
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

	var totalPages int

	for i := range watches {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if totalPages >= eng.maxCallsPerCycle {
			eng.log.Warn("cycle budget exhausted",
				"total_pages", totalPages,
				"max_calls_per_cycle", eng.maxCallsPerCycle,
			)
			break
		}

		w := &watches[i]
		eng.log.Info("processing watch", "name", w.Name, "id", w.ID)

		pagesUsed, processErr := eng.processWatch(ctx, w)
		totalPages += pagesUsed

		if processErr != nil {
			if errors.Is(processErr, ebay.ErrDailyLimitReached) {
				eng.log.Warn("daily API limit reached, stopping ingestion",
					"watch", w.Name,
					"total_pages", totalPages,
				)
				break
			}
			eng.log.Error("watch processing failed", "watch", w.Name, "error", processErr)
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

	// Always process alerts, even if budget/daily limit was hit.
	if err := ProcessAlerts(ctx, eng.store, eng.notifier); err != nil {
		eng.log.Error("alert processing failed", "error", err)
	}

	return nil
}

func (eng *Engine) processWatch(ctx context.Context, w *domain.Watch) (int, error) {
	req := ebay.SearchRequest{
		Query:      w.SearchQuery,
		CategoryID: w.CategoryID,
	}

	var listings []domain.Listing
	pagesUsed := 1

	if eng.paginator != nil {
		result, err := eng.paginator.Paginate(ctx, req, false)
		if err != nil {
			return 0, fmt.Errorf("paginating eBay: %w", err)
		}
		listings = result.NewListings
		pagesUsed = result.PagesUsed
		eng.log.Info("paginated search complete",
			"watch", w.Name,
			"pages_used", result.PagesUsed,
			"total_seen", result.TotalSeen,
			"new_listings", len(result.NewListings),
			"stopped_at", result.StoppedAt,
		)
	} else {
		resp, err := eng.ebay.Search(ctx, req)
		if err != nil {
			return 0, fmt.Errorf("searching eBay: %w", err)
		}
		listings = ebay.ToListings(resp.Items)
	}

	for i := range listings {
		eng.processListing(ctx, w, &listings[i])
	}

	return pagesUsed, nil
}

func (eng *Engine) processListing(
	ctx context.Context,
	w *domain.Watch,
	listing *domain.Listing,
) {
	if err := eng.store.UpsertListing(ctx, listing); err != nil {
		eng.log.Error("upsert failed", "ebay_id", listing.EbayID, "error", err)
		return
	}

	metrics.IngestionListingsTotal.Inc()

	// Extract attributes.
	ct, attrs, extractErr := eng.extractor.ClassifyAndExtract(
		ctx, listing.Title, nil,
	)
	if extractErr != nil {
		eng.log.Error("extraction failed", "listing", listing.EbayID, "error", extractErr)
		metrics.ExtractionFailuresTotal.Inc()
		return
	}

	productKey := extract.ProductKey(string(ct), attrs)
	if err := eng.store.UpdateListingExtraction(
		ctx, listing.ID, string(ct), attrs, 0.9, productKey,
	); err != nil {
		eng.log.Error("update extraction failed", "listing", listing.EbayID, "error", err)
		return
	}

	// Update the listing in-place for scoring.
	listing.ProductKey = productKey
	listing.ComponentType = ct

	// Score the listing.
	if err := ScoreListing(ctx, eng.store, listing); err != nil {
		eng.log.Error("scoring failed", "listing", listing.EbayID, "error", err)
		return
	}

	// Evaluate alert.
	eng.evaluateAlert(ctx, w, listing)
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
