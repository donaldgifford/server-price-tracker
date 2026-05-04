package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// observeQueryDuration records elapsed time on the AlertsQueryDuration
// histogram. Use as `defer observeQueryDuration("op", time.Now())` at the
// top of an alert-review store method so every code path is timed.
func observeQueryDuration(op string, start time.Time) {
	metrics.AlertsQueryDuration.WithLabelValues(op).Observe(time.Since(start).Seconds())
}

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
//
// The trace_id is derived from a.TraceID; nil or empty string both
// persist as NULL via the NULLIF in queryCreateAlert. This way the
// alert review UI can deep-link back to the trace that produced the
// listing for alerts created post-IMPL-0019, while pre-existing
// alerts remain queryable without any backfill.
func (s *PostgresStore) CreateAlert(ctx context.Context, a *domain.Alert) error {
	traceID := ""
	if a.TraceID != nil {
		traceID = *a.TraceID
	}
	err := s.pool.QueryRow(ctx, queryCreateAlert,
		a.WatchID, a.ListingID, a.Score, traceID,
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
	result := "failure"
	if succeeded {
		result = "success"
	}
	metrics.NotificationAttemptsInsertedTotal.WithLabelValues(result).Inc()
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

// alertReviewDefaults clamps and normalizes a query in place. Pulled out
// so the same defaults apply whether the caller is the HTTP handler or a
// test, and so callers can pass in zero values without being surprised by
// what shows up in the response.
func alertReviewDefaults(q *AlertReviewQuery) {
	if q.Status == "" {
		q.Status = AlertStatusActive
	}
	if q.Sort == "" {
		q.Sort = "score"
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 {
		q.PerPage = 25
	}
	if q.PerPage > 100 {
		q.PerPage = 100
	}
}

// buildAlertReviewWhere assembles the WHERE clause and positional args for
// ListAlertsForReview / count. Returns the SQL fragment (always starting
// with "WHERE 1=1" so callers can append "AND ..." safely) and the args
// slice. The caller appends ORDER BY / LIMIT / OFFSET on top.
func buildAlertReviewWhere(q *AlertReviewQuery) (string, []any) {
	var (
		clauses = []string{"1=1"}
		args    []any
	)

	switch q.Status {
	case AlertStatusActive:
		clauses = append(clauses, "a.notified = false")
	case AlertStatusDismissed:
		clauses = append(clauses, "a.dismissed_at IS NOT NULL")
	case AlertStatusNotified:
		clauses = append(clauses, "a.notified = true")
	case AlertStatusUndismissed:
		clauses = append(clauses, "a.dismissed_at IS NULL")
	case AlertStatusAll:
		// no-op
	}

	if q.MinScore > 0 {
		args = append(args, q.MinScore)
		clauses = append(clauses, fmt.Sprintf("a.score >= $%d", len(args)))
	}
	if q.ComponentType != "" {
		args = append(args, q.ComponentType)
		clauses = append(clauses, fmt.Sprintf("l.component_type = $%d", len(args)))
	}
	if q.WatchID != "" {
		args = append(args, q.WatchID)
		clauses = append(clauses, fmt.Sprintf("a.watch_id = $%d", len(args)))
	}
	if q.Search != "" {
		args = append(args, q.Search)
		clauses = append(clauses, fmt.Sprintf("l.title ILIKE '%%' || $%d || '%%'", len(args)))
	}

	return "WHERE " + strings.Join(clauses, " AND "), args
}

// alertReviewOrderBy maps the sort parameter to a stable ORDER BY clause.
// Always appends id DESC as a tiebreaker so OFFSET pagination stays
// deterministic across requests when rows arrive between page loads.
func alertReviewOrderBy(sort string) string {
	switch sort {
	case "created":
		return "ORDER BY a.created_at DESC, a.id DESC"
	case "watch":
		return "ORDER BY w.name ASC, a.score DESC, a.id DESC"
	default:
		return "ORDER BY a.score DESC, a.created_at DESC, a.id DESC"
	}
}

// ListAlertsForReview returns one page of alerts joined with their listing
// and watch context, plus the total count for pagination metadata. See
// AlertReviewQuery for the available filters.
func (s *PostgresStore) ListAlertsForReview(
	ctx context.Context,
	q *AlertReviewQuery,
) (AlertReviewResult, error) {
	defer observeQueryDuration("list", time.Now())

	if q == nil {
		q = &AlertReviewQuery{}
	}
	alertReviewDefaults(q)

	where, args := buildAlertReviewWhere(q)

	countQuery := `
		SELECT count(*)
		FROM alerts a
		JOIN listings l ON l.id = a.listing_id
		JOIN watches  w ON w.id = a.watch_id
		` + where

	var total int
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return AlertReviewResult{}, fmt.Errorf("counting alerts for review: %w", err)
	}
	metrics.AlertsTableRows.Set(float64(total))

	listArgs := append([]any(nil), args...)
	listArgs = append(listArgs, q.PerPage, (q.Page-1)*q.PerPage)
	listQuery := `
		SELECT ` + alertReviewSelectColumns + `
		FROM alerts a
		JOIN listings l ON l.id = a.listing_id
		JOIN watches  w ON w.id = a.watch_id
		` + where + `
		` + alertReviewOrderBy(q.Sort) + `
		LIMIT $` + strconv.Itoa(len(listArgs)-1) + ` OFFSET $` + strconv.Itoa(len(listArgs))

	rows, err := s.pool.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return AlertReviewResult{}, fmt.Errorf("querying alerts for review: %w", err)
	}
	defer rows.Close()

	items := make([]domain.AlertWithListing, 0, q.PerPage)
	for rows.Next() {
		item, err := scanAlertWithListing(rows)
		if err != nil {
			return AlertReviewResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AlertReviewResult{}, fmt.Errorf("iterating alert review rows: %w", err)
	}

	return AlertReviewResult{
		Items:   items,
		Total:   total,
		Page:    q.Page,
		PerPage: q.PerPage,
	}, nil
}

// scanAlertWithListing scans one row of the alert+listing+watch join.
// Column order must match alertReviewSelectColumns.
func scanAlertWithListing(rows pgx.Rows) (domain.AlertWithListing, error) {
	var (
		out domain.AlertWithListing
		a   = &out.Alert
		l   = &out.Listing
	)
	err := rows.Scan(
		&a.ID, &a.WatchID, &a.ListingID, &a.Score,
		&a.Notified, &a.NotifiedAt, &a.CreatedAt, &a.DismissedAt, &a.TraceID,
		&l.ID, &l.EbayID, &l.Title, &l.ItemURL, &l.ImageURL,
		&l.Price, &l.Currency, &l.ShippingCost, &l.ListingType,
		&l.SellerName, &l.SellerFeedback, &l.SellerFeedbackPct, &l.SellerTopRated,
		&l.ConditionRaw, &l.ConditionNorm, &l.ComponentType, &l.Quantity, &l.Attributes,
		&l.ExtractionConfidence, &l.ProductKey, &l.Score, &l.ScoreBreakdown,
		&l.Active, &l.ListedAt, &l.SoldAt, &l.SoldPrice, &l.FirstSeenAt, &l.UpdatedAt,
		&out.WatchName,
	)
	if err != nil {
		return domain.AlertWithListing{}, fmt.Errorf("scanning alert review row: %w", err)
	}
	return out, nil
}

// GetAlertDetail returns a single alert plus its listing, watch, and full
// notification history. Returns nil and a wrapped pgx.ErrNoRows when the
// alert does not exist.
func (s *PostgresStore) GetAlertDetail(ctx context.Context, id string) (*domain.AlertDetail, error) {
	defer observeQueryDuration("detail", time.Now())
	detailQuery := `
		SELECT ` + alertReviewSelectColumns + `
		FROM alerts a
		JOIN listings l ON l.id = a.listing_id
		JOIN watches  w ON w.id = a.watch_id
		WHERE a.id = $1`

	rows, err := s.pool.Query(ctx, detailQuery, id)
	if err != nil {
		return nil, fmt.Errorf("querying alert detail: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterating alert detail: %w", err)
		}
		return nil, fmt.Errorf("alert %s: %w", id, pgx.ErrNoRows)
	}

	row, err := scanAlertWithListing(rows)
	if err != nil {
		return nil, err
	}
	rows.Close()

	watch, err := s.GetWatch(ctx, row.Alert.WatchID)
	if err != nil {
		return nil, fmt.Errorf("fetching watch for alert %s: %w", id, err)
	}

	history, err := s.listNotificationAttempts(ctx, id)
	if err != nil {
		return nil, err
	}

	return &domain.AlertDetail{
		Alert:               row.Alert,
		Listing:             row.Listing,
		Watch:               *watch,
		NotificationHistory: history,
	}, nil
}

func (s *PostgresStore) listNotificationAttempts(
	ctx context.Context,
	alertID string,
) ([]domain.NotificationAttempt, error) {
	rows, err := s.pool.Query(ctx, queryNotificationAttemptsByAlert, alertID)
	if err != nil {
		return nil, fmt.Errorf("querying notification attempts: %w", err)
	}
	defer rows.Close()

	var out []domain.NotificationAttempt
	for rows.Next() {
		var a domain.NotificationAttempt
		if err := rows.Scan(
			&a.ID, &a.AlertID, &a.AttemptedAt, &a.Succeeded, &a.HTTPStatus, &a.ErrorText,
		); err != nil {
			return nil, fmt.Errorf("scanning notification attempt: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DismissAlerts marks the given alerts as dismissed, skipping any that are
// already dismissed. Returns the number of rows actually transitioned plus
// the trace IDs of those rows so the alert-review handler can post a
// Langfuse dismissal score (IMPL-0019 Phase 4). Empty/NULL trace IDs are
// filtered out — callers iterate the slice directly without a guard.
func (s *PostgresStore) DismissAlerts(ctx context.Context, ids []string) (int, []string, error) {
	defer observeQueryDuration("dismiss", time.Now())
	if len(ids) == 0 {
		return 0, nil, nil
	}
	rows, err := s.pool.Query(ctx, queryDismissAlerts, ids)
	if err != nil {
		return 0, nil, fmt.Errorf("dismissing alerts: %w", err)
	}
	defer rows.Close()

	count := 0
	traceIDs := make([]string, 0, len(ids))
	for rows.Next() {
		var (
			id      string
			traceID string
		)
		if err := rows.Scan(&id, &traceID); err != nil {
			return 0, nil, fmt.Errorf("scanning dismissed alert id: %w", err)
		}
		count++
		if traceID != "" {
			traceIDs = append(traceIDs, traceID)
		}
	}
	return count, traceIDs, rows.Err()
}

// RestoreAlerts clears dismissed_at on the given alerts. Returns the number
// of rows actually transitioned plus the trace IDs of those rows so the
// alert-review handler can post a Langfuse `operator_dismissed = 0` score
// (IMPL-0019 Phase 4 follow-up). Alerts that were not previously dismissed
// are skipped silently. Empty/NULL trace IDs are filtered out — callers
// iterate the slice directly without a guard.
func (s *PostgresStore) RestoreAlerts(ctx context.Context, ids []string) (int, []string, error) {
	defer observeQueryDuration("restore", time.Now())
	if len(ids) == 0 {
		return 0, nil, nil
	}
	rows, err := s.pool.Query(ctx, queryRestoreAlerts, ids)
	if err != nil {
		return 0, nil, fmt.Errorf("restoring alerts: %w", err)
	}
	defer rows.Close()

	count := 0
	traceIDs := make([]string, 0, len(ids))
	for rows.Next() {
		var (
			id      string
			traceID string
		)
		if err := rows.Scan(&id, &traceID); err != nil {
			return 0, nil, fmt.Errorf("scanning restored alert id: %w", err)
		}
		count++
		if traceID != "" {
			traceIDs = append(traceIDs, traceID)
		}
	}
	return count, traceIDs, rows.Err()
}

// ListAlertsForJudging returns alerts in the lookback window that don't
// yet have a row in judge_scores. Joins to listings + watches +
// price_baselines so the worker has everything it needs in one query.
//
// Limit defaults to 50 when the caller passes 0; the daily budget
// caps wall-clock spend independently of batch size.
func (s *PostgresStore) ListAlertsForJudging(ctx context.Context, q *JudgeCandidatesQuery) ([]domain.JudgeCandidate, error) {
	defer observeQueryDuration("judge.list_candidates", time.Now())

	lookback := q.Lookback
	if lookback <= 0 {
		lookback = 6 * time.Hour
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	since := time.Now().Add(-lookback)

	rows, err := s.pool.Query(ctx, queryListAlertsForJudging, since, limit)
	if err != nil {
		return nil, fmt.Errorf("listing alerts for judging: %w", err)
	}
	defer rows.Close()

	out := make([]domain.JudgeCandidate, 0, limit)
	for rows.Next() {
		var c domain.JudgeCandidate
		if err := rows.Scan(
			&c.AlertID, &c.WatchID, &c.WatchName,
			&c.ListingID, &c.ListingTitle, &c.ComponentType,
			&c.Condition, &c.PriceUSD,
			&c.BaselineP25, &c.BaselineP50, &c.BaselineP75, &c.SampleSize,
			&c.Score, &c.Threshold, &c.TraceID, &c.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning judge candidate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertJudgeScore upserts a verdict; conflict on alert_id is a no-op
// so the worker is idempotent. Re-judging an alert is a manual DELETE
// + worker re-run, not an update path.
func (s *PostgresStore) InsertJudgeScore(ctx context.Context, sc *domain.JudgeScore) error {
	defer observeQueryDuration("judge.insert_score", time.Now())
	_, err := s.pool.Exec(ctx, queryInsertJudgeScore,
		sc.AlertID, sc.Score, sc.Reason, sc.Model,
		sc.InputTokens, sc.OutputTokens, sc.CostUSD,
	)
	if err != nil {
		return fmt.Errorf("inserting judge score: %w", err)
	}
	return nil
}

// SumJudgeCostSince returns the total cost_usd for verdicts judged at
// or after `since`. The worker passes UTC midnight so the result is
// today's spend; comparison against daily_budget_usd gates the next
// batch.
func (s *PostgresStore) SumJudgeCostSince(ctx context.Context, since time.Time) (float64, error) {
	defer observeQueryDuration("judge.sum_cost", time.Now())
	var sum float64
	if err := s.pool.QueryRow(ctx, querySumJudgeCostSince, since).Scan(&sum); err != nil {
		return 0, fmt.Errorf("summing judge cost: %w", err)
	}
	return sum, nil
}

// GetJudgeScore returns the verdict for one alert, or nil + nil error
// when no row exists. Used by the alert review UI to populate the
// judge_score column on the detail page.
func (s *PostgresStore) GetJudgeScore(ctx context.Context, alertID string) (*domain.JudgeScore, error) {
	defer observeQueryDuration("judge.get_score", time.Now())
	var sc domain.JudgeScore
	err := s.pool.QueryRow(ctx, queryGetJudgeScore, alertID).Scan(
		&sc.AlertID, &sc.Score, &sc.Reason, &sc.Model,
		&sc.InputTokens, &sc.OutputTokens, &sc.CostUSD, &sc.JudgedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			//nolint:nilnil // documented surface: nil + nil signals "no judge verdict yet" so the caller renders an empty cell
			return nil, nil
		}
		return nil, fmt.Errorf("fetching judge score: %w", err)
	}
	return &sc, nil
}

// GetSystemState queries the system_state view for a single-row snapshot.
func (s *PostgresStore) GetSystemState(ctx context.Context) (*domain.SystemState, error) {
	var st domain.SystemState
	err := s.pool.QueryRow(ctx, queryGetSystemState).Scan(
		&st.WatchesTotal, &st.WatchesEnabled,
		&st.ListingsTotal, &st.ListingsUnextracted, &st.ListingsUnscored,
		&st.AlertsPending,
		&st.BaselinesTotal, &st.BaselinesWarm, &st.BaselinesCold,
		&st.ProductKeysNoBaseline, &st.ListingsIncompleteExtraction,
		&st.ExtractionQueueDepth,
	)
	if err != nil {
		return nil, fmt.Errorf("getting system state: %w", err)
	}
	return &st, nil
}

// PersistRateLimiterState upserts a single-row snapshot of eBay API quota state.
func (s *PostgresStore) PersistRateLimiterState(ctx context.Context, tokensUsed, dailyLimit int, resetAt time.Time) error {
	_, err := s.pool.Exec(ctx, queryPersistRateLimiterState, tokensUsed, dailyLimit, resetAt)
	if err != nil {
		return fmt.Errorf("persisting rate limiter state: %w", err)
	}
	return nil
}

// LoadRateLimiterState returns the last persisted eBay API quota snapshot.
// Returns pgx.ErrNoRows when no state has been persisted yet.
func (s *PostgresStore) LoadRateLimiterState(ctx context.Context) (*domain.RateLimiterState, error) {
	var st domain.RateLimiterState
	err := s.pool.QueryRow(ctx, queryLoadRateLimiterState).Scan(
		&st.TokensUsed, &st.DailyLimit, &st.ResetAt, &st.SyncedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("loading rate limiter state: %w", err)
	}
	return &st, nil
}

// ListListingsCursor returns up to limit listings with id > afterID, ordered by id ASC.
// Pass an empty string for afterID to start from the beginning.
func (s *PostgresStore) ListListingsCursor(ctx context.Context, afterID string, limit int) ([]domain.Listing, error) {
	if afterID == "" {
		// Nil UUID sorts before all gen_random_uuid() values.
		afterID = "00000000-0000-0000-0000-000000000000"
	}
	rows, err := s.pool.Query(ctx, queryListListingsCursor, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing listings by cursor: %w", err)
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
			&a.Notified, &a.NotifiedAt, &a.CreatedAt, &a.DismissedAt,
			&a.TraceID,
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
		&l.Active, &l.ListedAt, &l.SoldAt, &l.SoldPrice, &l.FirstSeenAt, &l.UpdatedAt,
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
		&l.Active, &l.ListedAt, &l.SoldAt, &l.SoldPrice, &l.FirstSeenAt, &l.UpdatedAt,
	)
}

// ExtractionQueue

// EnqueueExtraction adds a listing to the extraction queue.
// If a pending job already exists for the listing, the INSERT is silently ignored.
//
// When the calling context carries an active OTel span, its trace ID is
// captured into extraction_queue.trace_id so the worker can resume the
// trace at claim time instead of starting a disconnected one. Empty
// when observability.otel.enabled was false at enqueue time —
// queryEnqueueExtraction's NULLIF coerces "" to SQL NULL.
func (s *PostgresStore) EnqueueExtraction(ctx context.Context, listingID string, priority int) error {
	traceID := traceIDFromContext(ctx)
	if _, err := s.pool.Exec(ctx, queryEnqueueExtraction, listingID, priority, traceID); err != nil {
		return fmt.Errorf("enqueuing extraction: %w", err)
	}
	return nil
}

// DequeueExtractions claims up to batchSize pending extraction jobs for the given worker.
func (s *PostgresStore) DequeueExtractions(
	ctx context.Context,
	workerID string,
	batchSize int,
) ([]domain.ExtractionJob, error) {
	rows, err := s.pool.Query(ctx, queryDequeueExtractions, workerID, batchSize)
	if err != nil {
		return nil, fmt.Errorf("dequeuing extractions: %w", err)
	}
	defer rows.Close()

	var jobs []domain.ExtractionJob
	for rows.Next() {
		var j domain.ExtractionJob
		if err := rows.Scan(
			&j.ID, &j.ListingID, &j.Priority, &j.EnqueuedAt, &j.Attempts, &j.TraceID,
		); err != nil {
			return nil, fmt.Errorf("scanning extraction job: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// CompleteExtractionJob marks a queue entry as completed with an optional error.
func (s *PostgresStore) CompleteExtractionJob(ctx context.Context, id, errText string) error {
	if _, err := s.pool.Exec(ctx, queryCompleteExtractionJob, id, errText); err != nil {
		return fmt.Errorf("completing extraction job: %w", err)
	}
	return nil
}

// CountPendingExtractionJobs returns the number of uncompleted extraction queue entries.
func (s *PostgresStore) CountPendingExtractionJobs(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, queryCountPendingExtractionJobs).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting pending extraction jobs: %w", err)
	}
	return count, nil
}
