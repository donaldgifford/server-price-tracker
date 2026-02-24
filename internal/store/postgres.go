package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

const defaultPoolSize = 10

// PostgresStore implements Store using pgxpool (connection-pooled PostgreSQL).
//
// TODO(test): PostgresStore methods require live Postgres, tested via integration tests.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore creates a new PostgresStore with connection pooling.
func NewPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parsing connection string: %w", err)
	}

	cfg.MaxConns = defaultPoolSize

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return &PostgresStore{pool: pool}, nil
}

// Close gracefully shuts down the connection pool.
func (s *PostgresStore) Close() {
	s.pool.Close()
}

// Ping verifies the database connection is alive.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Migrate applies pending SQL schema migrations.
func (s *PostgresStore) Migrate(ctx context.Context) error {
	return RunMigrations(ctx, s.pool)
}

// UpsertListing inserts or updates a listing by ebay_item_id.
func (s *PostgresStore) UpsertListing(ctx context.Context, l *domain.Listing) error {
	args := pgx.NamedArgs{
		"ebay_item_id":          l.EbayID,
		"title":                 l.Title,
		"item_url":              l.ItemURL,
		"image_url":             l.ImageURL,
		"price":                 l.Price,
		"currency":              l.Currency,
		"shipping_cost":         l.ShippingCost,
		"listing_type":          string(l.ListingType),
		"seller_name":           l.SellerName,
		"seller_feedback_score": l.SellerFeedback,
		"seller_feedback_pct":   l.SellerFeedbackPct,
		"seller_top_rated":      l.SellerTopRated,
		"condition_raw":         l.ConditionRaw,
		"condition_norm":        string(l.ConditionNorm),
		"quantity":              l.Quantity,
		"listed_at":             l.ListedAt,
	}

	return s.pool.QueryRow(ctx, queryUpsertListing, args).Scan(
		&l.ID, &l.FirstSeenAt, &l.UpdatedAt,
	)
}

// GetListing retrieves a listing by its eBay item ID.
func (s *PostgresStore) GetListing(ctx context.Context, ebayID string) (*domain.Listing, error) {
	l := &domain.Listing{}
	err := scanListing(s.pool.QueryRow(ctx, queryGetListingByEbayID, ebayID), l)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// GetListingByID retrieves a listing by its internal UUID.
func (s *PostgresStore) GetListingByID(ctx context.Context, id string) (*domain.Listing, error) {
	l := &domain.Listing{}
	err := scanListing(s.pool.QueryRow(ctx, queryGetListingByID, id), l)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// ListListings queries listings with optional filters, returning results and total count.
func (s *PostgresStore) ListListings(
	ctx context.Context,
	opts *ListingQuery,
) ([]domain.Listing, int, error) {
	dataSQL, countSQL, args := opts.ToSQL()

	// Get total count.
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting listings: %w", err)
	}

	// Get data rows.
	rows, err := s.pool.Query(ctx, dataSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying listings: %w", err)
	}
	defer rows.Close()

	var listings []domain.Listing
	for rows.Next() {
		var l domain.Listing
		if err := scanListingRow(rows, &l); err != nil {
			return nil, 0, fmt.Errorf("scanning listing: %w", err)
		}
		listings = append(listings, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating listings: %w", err)
	}

	return listings, total, nil
}

// UpdateListingExtraction updates the extraction fields for a listing.
func (s *PostgresStore) UpdateListingExtraction(
	ctx context.Context,
	id string,
	componentType string,
	attrs map[string]any,
	confidence float64,
	productKey string,
) error {
	attrsJSON, err := json.Marshal(attrs)
	if err != nil {
		return fmt.Errorf("marshaling attributes: %w", err)
	}

	_, err = s.pool.Exec(ctx, queryUpdateListingExtraction,
		id, componentType, attrsJSON, confidence, productKey,
	)
	if err != nil {
		return fmt.Errorf("updating listing extraction: %w", err)
	}
	return nil
}

// UpdateScore updates the score and breakdown for a listing.
func (s *PostgresStore) UpdateScore(
	ctx context.Context,
	id string,
	score int,
	breakdown json.RawMessage,
) error {
	_, err := s.pool.Exec(ctx, queryUpdateScore, id, score, breakdown)
	if err != nil {
		return fmt.Errorf("updating score: %w", err)
	}
	return nil
}

// ListUnextractedListings returns listings that haven't been classified yet.
func (s *PostgresStore) ListUnextractedListings(
	ctx context.Context,
	limit int,
) ([]domain.Listing, error) {
	return s.queryListings(ctx, queryListUnextractedListings, limit)
}

// ListUnscoredListings returns listings that have been extracted but not scored.
func (s *PostgresStore) ListUnscoredListings(
	ctx context.Context,
	limit int,
) ([]domain.Listing, error) {
	return s.queryListings(ctx, queryListUnscoredListings, limit)
}

// CreateWatch inserts a new watch.
func (s *PostgresStore) CreateWatch(ctx context.Context, w *domain.Watch) error {
	filtersJSON, err := json.Marshal(w.Filters)
	if err != nil {
		return fmt.Errorf("marshaling filters: %w", err)
	}

	args := pgx.NamedArgs{
		"name":            w.Name,
		"search_query":    w.SearchQuery,
		"category_id":     w.CategoryID,
		"component_type":  string(w.ComponentType),
		"filters":         filtersJSON,
		"score_threshold": w.ScoreThreshold,
		"enabled":         w.Enabled,
	}

	return s.pool.QueryRow(ctx, queryCreateWatch, args).Scan(
		&w.ID, &w.CreatedAt, &w.UpdatedAt,
	)
}

// GetWatch retrieves a watch by its ID.
func (s *PostgresStore) GetWatch(ctx context.Context, id string) (*domain.Watch, error) {
	w := &domain.Watch{}
	var filtersJSON []byte

	err := s.pool.QueryRow(ctx, queryGetWatch, id).Scan(
		&w.ID, &w.Name, &w.SearchQuery, &w.CategoryID, &w.ComponentType,
		&filtersJSON, &w.ScoreThreshold, &w.Enabled, &w.LastPolledAt, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(filtersJSON, &w.Filters); err != nil {
		return nil, fmt.Errorf("unmarshaling watch filters: %w", err)
	}

	return w, nil
}

// ListWatches returns all watches, optionally filtered to enabled only.
func (s *PostgresStore) ListWatches(
	ctx context.Context,
	enabledOnly bool,
) ([]domain.Watch, error) {
	query := queryListWatchesAll
	if enabledOnly {
		query = queryListWatchesEnabled
	}

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying watches: %w", err)
	}
	defer rows.Close()

	var watches []domain.Watch
	for rows.Next() {
		var w domain.Watch
		var filtersJSON []byte

		if err := rows.Scan(
			&w.ID, &w.Name, &w.SearchQuery, &w.CategoryID, &w.ComponentType,
			&filtersJSON, &w.ScoreThreshold, &w.Enabled, &w.LastPolledAt, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning watch: %w", err)
		}

		if err := json.Unmarshal(filtersJSON, &w.Filters); err != nil {
			return nil, fmt.Errorf("unmarshaling watch filters: %w", err)
		}

		watches = append(watches, w)
	}

	return watches, rows.Err()
}

// UpdateWatch updates an existing watch.
func (s *PostgresStore) UpdateWatch(ctx context.Context, w *domain.Watch) error {
	filtersJSON, err := json.Marshal(w.Filters)
	if err != nil {
		return fmt.Errorf("marshaling filters: %w", err)
	}

	args := pgx.NamedArgs{
		"id":              w.ID,
		"name":            w.Name,
		"search_query":    w.SearchQuery,
		"category_id":     w.CategoryID,
		"component_type":  string(w.ComponentType),
		"filters":         filtersJSON,
		"score_threshold": w.ScoreThreshold,
		"enabled":         w.Enabled,
	}

	_, err = s.pool.Exec(ctx, queryUpdateWatch, args)
	if err != nil {
		return fmt.Errorf("updating watch: %w", err)
	}
	return nil
}

// DeleteWatch removes a watch by its ID.
func (s *PostgresStore) DeleteWatch(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, queryDeleteWatch, id)
	if err != nil {
		return fmt.Errorf("deleting watch: %w", err)
	}
	return nil
}

// SetWatchEnabled enables or disables a watch.
func (s *PostgresStore) SetWatchEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.pool.Exec(ctx, querySetWatchEnabled, id, enabled)
	if err != nil {
		return fmt.Errorf("setting watch enabled: %w", err)
	}
	return nil
}

// GetBaseline retrieves a price baseline by product key.
func (s *PostgresStore) GetBaseline(
	ctx context.Context,
	productKey string,
) (*domain.PriceBaseline, error) {
	b := &domain.PriceBaseline{}
	err := s.pool.QueryRow(ctx, queryGetBaseline, productKey).Scan(
		&b.ID, &b.ProductKey, &b.SampleCount,
		&b.P10, &b.P25, &b.P50, &b.P75, &b.P90, &b.Mean,
		&b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ListBaselines returns all price baselines.
func (s *PostgresStore) ListBaselines(ctx context.Context) ([]domain.PriceBaseline, error) {
	rows, err := s.pool.Query(ctx, queryListBaselines)
	if err != nil {
		return nil, fmt.Errorf("querying baselines: %w", err)
	}
	defer rows.Close()

	var baselines []domain.PriceBaseline
	for rows.Next() {
		var b domain.PriceBaseline
		if err := rows.Scan(
			&b.ID, &b.ProductKey, &b.SampleCount,
			&b.P10, &b.P25, &b.P50, &b.P75, &b.P90, &b.Mean,
			&b.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning baseline: %w", err)
		}
		baselines = append(baselines, b)
	}

	return baselines, rows.Err()
}

// RecomputeBaseline recalculates the price baseline for a product key.
func (s *PostgresStore) RecomputeBaseline(
	ctx context.Context,
	productKey string,
	windowDays int,
) error {
	_, err := s.pool.Exec(ctx, queryRecomputeBaseline, productKey, windowDays)
	if err != nil {
		return fmt.Errorf("recomputing baseline for %s: %w", productKey, err)
	}
	return nil
}

// RecomputeAllBaselines recalculates baselines for all known product keys.
func (s *PostgresStore) RecomputeAllBaselines(ctx context.Context, windowDays int) error {
	rows, err := s.pool.Query(ctx, queryListDistinctProductKeys)
	if err != nil {
		return fmt.Errorf("listing product keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return fmt.Errorf("scanning product key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating product keys: %w", err)
	}

	for _, key := range keys {
		if err := s.RecomputeBaseline(ctx, key, windowDays); err != nil {
			return err
		}
	}

	return nil
}

// CreateAlert inserts a new alert, silently ignoring duplicates.
func (s *PostgresStore) CreateAlert(ctx context.Context, a *domain.Alert) error {
	err := s.pool.QueryRow(ctx, queryCreateAlert,
		a.WatchID, a.ListingID, a.Score,
	).Scan(&a.ID, &a.CreatedAt)

	// ON CONFLICT DO NOTHING returns no rows — treat as success.
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// ListPendingAlerts returns all un-notified alerts.
func (s *PostgresStore) ListPendingAlerts(ctx context.Context) ([]domain.Alert, error) {
	return s.queryAlerts(ctx, queryListPendingAlerts)
}

// ListAlertsByWatch returns alerts for a specific watch.
func (s *PostgresStore) ListAlertsByWatch(
	ctx context.Context,
	watchID string,
	limit int,
) ([]domain.Alert, error) {
	return s.queryAlerts(ctx, queryListAlertsByWatch, watchID, limit)
}

// MarkAlertNotified marks a single alert as notified.
func (s *PostgresStore) MarkAlertNotified(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, queryMarkAlertNotified, id)
	if err != nil {
		return fmt.Errorf("marking alert notified: %w", err)
	}
	return nil
}

// MarkAlertsNotified marks multiple alerts as notified.
func (s *PostgresStore) MarkAlertsNotified(ctx context.Context, ids []string) error {
	_, err := s.pool.Exec(ctx, queryMarkAlertsNotified, ids)
	if err != nil {
		return fmt.Errorf("marking alerts notified: %w", err)
	}
	return nil
}

// HasRecentAlert returns true if a notified alert for the same (watch, listing)
// pair exists within the given cooldown window.
func (s *PostgresStore) HasRecentAlert(
	ctx context.Context,
	watchID, listingID string,
	cooldown time.Duration,
) (bool, error) {
	cutoff := time.Now().Add(-cooldown)
	var exists bool
	if err := s.pool.QueryRow(ctx, queryHasRecentAlert, watchID, listingID, cutoff).Scan(&exists); err != nil {
		return false, fmt.Errorf("checking recent alert: %w", err)
	}
	return exists, nil
}

// InsertNotificationAttempt records the outcome of a notification send attempt.
func (s *PostgresStore) InsertNotificationAttempt(
	ctx context.Context,
	alertID string,
	succeeded bool,
	httpStatus int,
	errText string,
) error {
	_, err := s.pool.Exec(ctx, queryInsertNotificationAttempt, alertID, succeeded, httpStatus, errText)
	if err != nil {
		return fmt.Errorf("inserting notification attempt: %w", err)
	}
	return nil
}

// HasSuccessfulNotification returns true if at least one successful notification
// attempt exists for the given alert.
func (s *PostgresStore) HasSuccessfulNotification(ctx context.Context, alertID string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, queryHasSuccessfulNotification, alertID).Scan(&exists); err != nil {
		return false, fmt.Errorf("checking successful notification: %w", err)
	}
	return exists, nil
}

// CountWatches returns the total and enabled watch counts.
func (s *PostgresStore) CountWatches(ctx context.Context) (int, int, error) {
	var total, enabled int
	if err := s.pool.QueryRow(ctx, queryCountWatches).Scan(&total, &enabled); err != nil {
		return 0, 0, fmt.Errorf("counting watches: %w", err)
	}
	return total, enabled, nil
}

// CountListings returns the total number of listings.
func (s *PostgresStore) CountListings(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, queryCountListings).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting listings: %w", err)
	}
	return count, nil
}

// CountUnextractedListings returns listings without LLM extraction.
func (s *PostgresStore) CountUnextractedListings(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, queryCountUnextractedListings).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting unextracted listings: %w", err)
	}
	return count, nil
}

// CountUnscoredListings returns listings extracted but not yet scored.
func (s *PostgresStore) CountUnscoredListings(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, queryCountUnscoredListings).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting unscored listings: %w", err)
	}
	return count, nil
}

// CountPendingAlerts returns the number of un-notified alerts.
func (s *PostgresStore) CountPendingAlerts(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, queryCountPendingAlerts).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting pending alerts: %w", err)
	}
	return count, nil
}

// CountBaselinesByMaturity returns counts of cold (<10 samples) and warm (>=10) baselines.
func (s *PostgresStore) CountBaselinesByMaturity(ctx context.Context) (int, int, error) {
	var cold, warm int
	if err := s.pool.QueryRow(ctx, queryCountBaselinesByMaturity).Scan(&cold, &warm); err != nil {
		return 0, 0, fmt.Errorf("counting baselines by maturity: %w", err)
	}
	return cold, warm, nil
}

// CountProductKeysWithoutBaseline returns the number of distinct product keys
// in listings that have no corresponding price baseline.
func (s *PostgresStore) CountProductKeysWithoutBaseline(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, queryCountProductKeysWithoutBaseline).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting product keys without baseline: %w", err)
	}
	return count, nil
}

// ListIncompleteExtractions returns listings with incomplete extraction data.
// If componentType is empty, returns all component types. Otherwise filters by type.
func (s *PostgresStore) ListIncompleteExtractions(
	ctx context.Context,
	componentType string,
	limit int,
) ([]domain.Listing, error) {
	if componentType == "" {
		return s.queryListings(ctx, queryListIncompleteExtractions, limit)
	}

	rows, err := s.pool.Query(ctx, queryListIncompleteExtractionsForType, componentType, limit)
	if err != nil {
		return nil, fmt.Errorf("querying incomplete extractions: %w", err)
	}
	defer rows.Close()

	var listings []domain.Listing
	for rows.Next() {
		var l domain.Listing
		if err := scanListingRow(rows, &l); err != nil {
			return nil, fmt.Errorf("scanning listing: %w", err)
		}
		listings = append(listings, l)
	}

	return listings, rows.Err()
}

// CountIncompleteExtractions returns the total count of listings with incomplete extraction data.
func (s *PostgresStore) CountIncompleteExtractions(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, queryCountIncompleteExtractions).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting incomplete extractions: %w", err)
	}
	return count, nil
}

// CountIncompleteExtractionsByType returns incomplete extraction counts grouped by component type.
func (s *PostgresStore) CountIncompleteExtractionsByType(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, queryCountIncompleteExtractionsByType)
	if err != nil {
		return nil, fmt.Errorf("counting incomplete extractions by type: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var ct string
		var count int
		if err := rows.Scan(&ct, &count); err != nil {
			return nil, fmt.Errorf("scanning incomplete extraction count: %w", err)
		}
		result[ct] = count
	}

	return result, rows.Err()
}

// InsertJobRun records the start of a scheduled job and returns its UUID.
func (s *PostgresStore) InsertJobRun(ctx context.Context, jobName string) (string, error) {
	var id string
	if err := s.pool.QueryRow(ctx, queryInsertJobRun, jobName).Scan(&id); err != nil {
		return "", fmt.Errorf("inserting job run: %w", err)
	}
	return id, nil
}

// CompleteJobRun marks a job run as finished with the given status and metadata.
func (s *PostgresStore) CompleteJobRun(
	ctx context.Context,
	id string,
	status string,
	errText string,
	rowsAffected int,
) error {
	_, err := s.pool.Exec(ctx, queryCompleteJobRun, id, status, errText, rowsAffected)
	if err != nil {
		return fmt.Errorf("completing job run: %w", err)
	}
	return nil
}

// ListJobRuns returns the most recent runs for a specific job, newest first.
func (s *PostgresStore) ListJobRuns(
	ctx context.Context,
	jobName string,
	limit int,
) ([]domain.JobRun, error) {
	rows, err := s.pool.Query(ctx, queryListJobRuns, jobName, limit)
	if err != nil {
		return nil, fmt.Errorf("querying job runs: %w", err)
	}
	defer rows.Close()

	return scanJobRuns(rows)
}

// ListLatestJobRuns returns the single most recent run for each distinct job name.
func (s *PostgresStore) ListLatestJobRuns(ctx context.Context) ([]domain.JobRun, error) {
	rows, err := s.pool.Query(ctx, queryListLatestJobRuns)
	if err != nil {
		return nil, fmt.Errorf("querying latest job runs: %w", err)
	}
	defer rows.Close()

	return scanJobRuns(rows)
}

// UpdateWatchLastPolled sets the last_polled_at timestamp for a watch.
func (s *PostgresStore) UpdateWatchLastPolled(
	ctx context.Context,
	watchID string,
	t time.Time,
) error {
	_, err := s.pool.Exec(ctx, queryUpdateWatchLastPolled, watchID, t)
	if err != nil {
		return fmt.Errorf("updating watch last_polled_at: %w", err)
	}
	return nil
}

// RecoverStaleJobRuns marks any 'running' job rows older than olderThan as 'crashed',
// then deletes all rows older than 30 days. Returns the number of rows marked as crashed.
func (s *PostgresStore) RecoverStaleJobRuns(
	ctx context.Context,
	olderThan time.Duration,
) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	tag, err := s.pool.Exec(ctx, queryMarkStaleJobRunsCrashed, cutoff)
	if err != nil {
		return 0, fmt.Errorf("marking stale job runs crashed: %w", err)
	}
	affected := int(tag.RowsAffected())

	if _, err := s.pool.Exec(ctx, queryDeleteOldJobRuns); err != nil {
		return affected, fmt.Errorf("deleting old job runs: %w", err)
	}

	return affected, nil
}

// AcquireSchedulerLock attempts to acquire a distributed lock for the given job.
// Returns true if the lock was acquired, false if another holder already owns it.
func (s *PostgresStore) AcquireSchedulerLock(
	ctx context.Context,
	jobName string,
	holder string,
	ttl time.Duration,
) (bool, error) {
	expiresAt := time.Now().Add(ttl)

	var gotName string
	err := s.pool.QueryRow(ctx, queryAcquireSchedulerLock, jobName, holder, expiresAt).Scan(&gotName)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // lock held by another; conflict not replaced
	}
	if err != nil {
		return false, fmt.Errorf("acquiring scheduler lock: %w", err)
	}

	return true, nil
}

// ReleaseSchedulerLock deletes the lock row for the given job and holder.
func (s *PostgresStore) ReleaseSchedulerLock(
	ctx context.Context,
	jobName string,
	holder string,
) error {
	_, err := s.pool.Exec(ctx, queryReleaseSchedulerLock, jobName, holder)
	if err != nil {
		return fmt.Errorf("releasing scheduler lock: %w", err)
	}
	return nil
}

// scanJobRuns scans rows from a job_runs query into a slice.
func scanJobRuns(rows pgx.Rows) ([]domain.JobRun, error) {
	var runs []domain.JobRun
	for rows.Next() {
		var r domain.JobRun
		if err := rows.Scan(
			&r.ID, &r.JobName, &r.StartedAt, &r.CompletedAt,
			&r.Status, &r.ErrorText, &r.RowsAffected,
		); err != nil {
			return nil, fmt.Errorf("scanning job run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// queryListings is a helper for listing queries with a LIMIT parameter.
func (s *PostgresStore) queryListings(
	ctx context.Context,
	query string,
	limit int,
) ([]domain.Listing, error) {
	rows, err := s.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("querying listings: %w", err)
	}
	defer rows.Close()

	var listings []domain.Listing
	for rows.Next() {
		var l domain.Listing
		if err := scanListingRow(rows, &l); err != nil {
			return nil, fmt.Errorf("scanning listing: %w", err)
		}
		listings = append(listings, l)
	}

	return listings, rows.Err()
}

// queryAlerts is a helper for alert queries.
func (s *PostgresStore) queryAlerts(
	ctx context.Context,
	query string,
	args ...any,
) ([]domain.Alert, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying alerts: %w", err)
	}
	defer rows.Close()

	var alerts []domain.Alert
	for rows.Next() {
		var a domain.Alert
		if err := rows.Scan(
			&a.ID, &a.WatchID, &a.ListingID, &a.Score,
			&a.Notified, &a.NotifiedAt, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning alert: %w", err)
		}
		alerts = append(alerts, a)
	}

	return alerts, rows.Err()
}

// scannable abstracts pgx.Row and pgx.Rows for reuse.
type scannable interface {
	Scan(dest ...any) error
}

// scanListing scans a full listing row from a pgx.Row.
func scanListing(row scannable, l *domain.Listing) error {
	return row.Scan(
		&l.ID, &l.EbayID, &l.Title, &l.ItemURL, &l.ImageURL,
		&l.Price, &l.Currency, &l.ShippingCost, &l.ListingType,
		&l.SellerName, &l.SellerFeedback, &l.SellerFeedbackPct, &l.SellerTopRated,
		&l.ConditionRaw, &l.ConditionNorm, &l.ComponentType, &l.Quantity, &l.Attributes,
		&l.ExtractionConfidence, &l.ProductKey, &l.Score, &l.ScoreBreakdown,
		&l.ListedAt, &l.SoldAt, &l.SoldPrice, &l.FirstSeenAt, &l.UpdatedAt,
	)
}

// scanListingRow scans a listing from pgx.Rows (same fields).
func scanListingRow(rows pgx.Rows, l *domain.Listing) error {
	return rows.Scan(
		&l.ID, &l.EbayID, &l.Title, &l.ItemURL, &l.ImageURL,
		&l.Price, &l.Currency, &l.ShippingCost, &l.ListingType,
		&l.SellerName, &l.SellerFeedback, &l.SellerFeedbackPct, &l.SellerTopRated,
		&l.ConditionRaw, &l.ConditionNorm, &l.ComponentType, &l.Quantity, &l.Attributes,
		&l.ExtractionConfidence, &l.ProductKey, &l.Score, &l.ScoreBreakdown,
		&l.ListedAt, &l.SoldAt, &l.SoldPrice, &l.FirstSeenAt, &l.UpdatedAt,
	)
}
