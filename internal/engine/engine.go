package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/config"
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
	analyticsClient    *ebay.AnalyticsClient
	rateLimiter        *ebay.RateLimiter
	maxCallsPerCycle   int
	baselineWindowDays int
	staggerOffset      time.Duration
	alertsConfig       config.AlertsConfig
	workerCount        int
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

// WithAnalyticsClient sets the eBay Analytics API client for quota syncing.
func WithAnalyticsClient(ac *ebay.AnalyticsClient) EngineOption {
	return func(e *Engine) {
		e.analyticsClient = ac
	}
}

// WithRateLimiter sets the rate limiter for quota syncing.
func WithRateLimiter(rl *ebay.RateLimiter) EngineOption {
	return func(e *Engine) {
		e.rateLimiter = rl
	}
}

// WithAlertsConfig sets the alert behavior configuration.
func WithAlertsConfig(cfg config.AlertsConfig) EngineOption {
	return func(e *Engine) {
		e.alertsConfig = cfg
	}
}

// WithWorkerCount sets the number of extraction worker goroutines.
func WithWorkerCount(n int) EngineOption {
	return func(e *Engine) {
		e.workerCount = n
	}
}

const (
	workerIdleSleep    = 100 * time.Millisecond
	defaultWorkerCount = 1
)

// StartExtractionWorkers launches workerCount goroutines that drain the extraction queue.
// Workers stop when ctx is cancelled.
func (eng *Engine) StartExtractionWorkers(ctx context.Context) {
	count := eng.workerCount
	if count <= 0 {
		count = defaultWorkerCount
	}
	for i := range count {
		workerID := fmt.Sprintf("worker-%d", i)
		go eng.runExtractionWorker(ctx, workerID)
	}
}

// runExtractionWorker continuously dequeues and processes extraction jobs until ctx is done.
func (eng *Engine) runExtractionWorker(ctx context.Context, workerID string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		jobs, err := eng.store.DequeueExtractions(ctx, workerID, 1)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			eng.log.Error("dequeue failed", "worker", workerID, "error", err)
			time.Sleep(workerIdleSleep)
			continue
		}

		if len(jobs) == 0 {
			time.Sleep(workerIdleSleep)
			continue
		}

		eng.processExtractionJob(ctx, workerID, &jobs[0])
	}
}

// processExtractionJob extracts, scores, and completes a single queued job.
func (eng *Engine) processExtractionJob(
	ctx context.Context,
	workerID string,
	job *domain.ExtractionJob,
) {
	listing, err := eng.store.GetListingByID(ctx, job.ListingID)
	if err != nil {
		eng.log.Error("get listing failed",
			"worker", workerID, "listing", job.ListingID, "error", err,
		)
		eng.completeJob(ctx, workerID, job.ID, err.Error())
		return
	}

	extractStart := time.Now()
	ct, attrs, extractErr := eng.extractor.ClassifyAndExtract(ctx, listing.Title, nil)
	metrics.ExtractionDuration.Observe(time.Since(extractStart).Seconds())

	if extractErr != nil {
		eng.log.Error("extraction failed",
			"worker", workerID, "listing", listing.EbayID, "error", extractErr,
		)
		metrics.ExtractionFailuresTotal.Inc()
		eng.completeJob(ctx, workerID, job.ID, extractErr.Error())
		return
	}

	productKey := extract.ProductKey(string(ct), attrs)
	if updateErr := eng.store.UpdateListingExtraction(
		ctx, listing.ID, string(ct), attrs, 0.9, productKey,
	); updateErr != nil {
		eng.log.Error("update extraction failed",
			"worker", workerID, "listing", listing.EbayID, "error", updateErr,
		)
		eng.completeJob(ctx, workerID, job.ID, updateErr.Error())
		return
	}

	listing.ProductKey = productKey
	listing.ComponentType = ct

	if scoreErr := ScoreListing(ctx, eng.store, listing); scoreErr != nil {
		eng.log.Error("scoring failed",
			"worker", workerID, "listing", listing.EbayID, "error", scoreErr,
		)
	}

	eng.completeJob(ctx, workerID, job.ID, "")
}

// completeJob marks a queue entry as done, logging on failure.
func (eng *Engine) completeJob(ctx context.Context, workerID, jobID, errText string) {
	if err := eng.store.CompleteExtractionJob(ctx, jobID, errText); err != nil {
		eng.log.Error("complete job failed",
			"worker", workerID, "job", jobID, "error", err,
		)
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

		// Record last poll time regardless of processing outcome.
		if pollErr := eng.store.UpdateWatchLastPolled(ctx, w.ID, time.Now()); pollErr != nil {
			eng.log.Warn("failed to update watch last_polled_at",
				"watch", w.Name, "error", pollErr,
			)
		}

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

	// Sync quota metrics from eBay Analytics API after ingestion.
	eng.SyncQuota(ctx)

	// Sync system state gauges after ingestion.
	eng.SyncStateMetrics(ctx)

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

	// Enqueue for async extraction by the worker pool.
	if err := eng.store.EnqueueExtraction(ctx, listing.ID, 0); err != nil {
		eng.log.Error("enqueue extraction failed", "listing", listing.EbayID, "error", err)
	}

	// Evaluate alert for listings that already have a score (e.g., re-ingested).
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

	// When re-alerts are enabled, skip listings notified within the cooldown window.
	if eng.alertsConfig.ReAlertsEnabled {
		recent, err := eng.store.HasRecentAlert(
			ctx, w.ID, listing.ID, eng.alertsConfig.ReAlertsCooldown,
		)
		if err != nil {
			eng.log.Error("checking recent alert failed", "listing", listing.ID, "error", err)
			return
		}
		if recent {
			eng.log.Debug("skipping alert: within cooldown window",
				"watch", w.Name, "listing", listing.ID,
			)
			return
		}
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

// SyncQuota calls the eBay Analytics API to update Prometheus gauge metrics
// and sync the in-memory rate limiter with eBay's authoritative quota state.
// Failures are logged but never propagated — this is best-effort.
func (eng *Engine) SyncQuota(ctx context.Context) {
	if eng.analyticsClient == nil {
		return
	}

	quota, err := eng.analyticsClient.GetBrowseQuota(ctx)
	if err != nil {
		eng.log.Warn("failed to sync eBay quota from analytics API", "error", err)
		return
	}

	metrics.EbayRateLimit.Set(float64(quota.Limit))
	metrics.EbayRateRemaining.Set(float64(quota.Remaining))
	metrics.EbayRateResetTimestamp.Set(float64(quota.ResetAt.Unix()))

	if eng.rateLimiter != nil {
		eng.rateLimiter.Sync(quota.Count, quota.Limit, quota.ResetAt)
	}

	eng.log.Debug("eBay quota synced",
		"count", quota.Count,
		"limit", quota.Limit,
		"remaining", quota.Remaining,
		"reset_at", quota.ResetAt,
	)
}

// SyncStateMetrics queries the store for current counts and updates Prometheus
// gauges. Failures are logged but never propagated — this is best-effort.
func (eng *Engine) SyncStateMetrics(ctx context.Context) {
	total, enabled, err := eng.store.CountWatches(ctx)
	if err != nil {
		eng.log.Warn("failed to count watches", "error", err)
	} else {
		metrics.WatchesTotal.Set(float64(total))
		metrics.WatchesEnabled.Set(float64(enabled))
	}

	listings, err := eng.store.CountListings(ctx)
	if err != nil {
		eng.log.Warn("failed to count listings", "error", err)
	} else {
		metrics.ListingsTotal.Set(float64(listings))
	}

	unextracted, err := eng.store.CountUnextractedListings(ctx)
	if err != nil {
		eng.log.Warn("failed to count unextracted listings", "error", err)
	} else {
		metrics.ListingsUnextracted.Set(float64(unextracted))
	}

	unscored, err := eng.store.CountUnscoredListings(ctx)
	if err != nil {
		eng.log.Warn("failed to count unscored listings", "error", err)
	} else {
		metrics.ListingsUnscored.Set(float64(unscored))
	}

	pending, err := eng.store.CountPendingAlerts(ctx)
	if err != nil {
		eng.log.Warn("failed to count pending alerts", "error", err)
	} else {
		metrics.AlertsPending.Set(float64(pending))
	}

	cold, warm, err := eng.store.CountBaselinesByMaturity(ctx)
	if err != nil {
		eng.log.Warn("failed to count baselines by maturity", "error", err)
	} else {
		metrics.BaselinesCold.Set(float64(cold))
		metrics.BaselinesWarm.Set(float64(warm))
		metrics.BaselinesTotal.Set(float64(cold + warm))
	}

	noBaseline, err := eng.store.CountProductKeysWithoutBaseline(ctx)
	if err != nil {
		eng.log.Warn("failed to count product keys without baseline", "error", err)
	} else {
		metrics.ProductKeysNoBaseline.Set(float64(noBaseline))
	}

	incomplete, err := eng.store.CountIncompleteExtractions(ctx)
	if err != nil {
		eng.log.Warn("failed to count incomplete extractions", "error", err)
	} else {
		metrics.ListingsIncompleteExtraction.Set(float64(incomplete))
	}

	byType, err := eng.store.CountIncompleteExtractionsByType(ctx)
	if err != nil {
		eng.log.Warn("failed to count incomplete extractions by type", "error", err)
	} else {
		for ct, count := range byType {
			metrics.ListingsIncompleteExtractionByType.WithLabelValues(ct).Set(float64(count))
		}
	}

	queueDepth, err := eng.store.CountPendingExtractionJobs(ctx)
	if err != nil {
		eng.log.Warn("failed to count pending extraction jobs", "error", err)
	} else {
		metrics.ExtractionQueueDepth.Set(float64(queueDepth))
	}
}

// RunReExtraction enqueues listings with incomplete extraction data for re-processing.
// Returns the count of successfully enqueued listings.
func (eng *Engine) RunReExtraction(ctx context.Context, componentType string, limit int) (int, error) {
	const defaultLimit = 100
	if limit <= 0 {
		limit = defaultLimit
	}

	listings, err := eng.store.ListIncompleteExtractions(ctx, componentType, limit)
	if err != nil {
		return 0, fmt.Errorf("listing incomplete extractions: %w", err)
	}

	if len(listings) == 0 {
		eng.log.Info("no incomplete extractions found")
		return 0, nil
	}

	var enqueued int
	for i := range listings {
		if ctx.Err() != nil {
			return enqueued, ctx.Err()
		}
		if err := eng.store.EnqueueExtraction(ctx, listings[i].ID, 1); err != nil {
			eng.log.Error("enqueue re-extraction failed",
				"listing", listings[i].EbayID, "error", err,
			)
			continue
		}
		enqueued++
	}

	eng.log.Info("re-extraction enqueued",
		"total", len(listings),
		"enqueued", enqueued,
	)

	return enqueued, nil
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
