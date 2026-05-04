// Package store defines the datastore abstraction for server-price-tracker.
// All business logic depends on the Store interface, never on concrete
// implementations. This enables mock-based testing without a running database.
package store

import (
	"context"
	"encoding/json"
	"time"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ListingQuery defines optional filters for listing queries.
type ListingQuery struct {
	ComponentType *string
	MinScore      *int
	MaxScore      *int
	ProductKey    *string
	SellerMinFB   *int
	Conditions    []string
	Limit         int // default 50
	Offset        int
	OrderBy       string // "score", "price", "first_seen_at"
}

// AlertReviewStatus narrows the alert review list to a state subset.
// Each value maps to a deterministic SQL WHERE clause regardless of
// server config, so a shared URL like
// `/alerts?status=undismissed` produces the same result everywhere.
type AlertReviewStatus string

// Alert review status values.
const (
	AlertStatusActive      AlertReviewStatus = "active"      // notified=false (the default work surface)
	AlertStatusDismissed   AlertReviewStatus = "dismissed"   // dismissed_at IS NOT NULL
	AlertStatusNotified    AlertReviewStatus = "notified"    // notified=true
	AlertStatusUndismissed AlertReviewStatus = "undismissed" // dismissed_at IS NULL (queue under summary mode)
	AlertStatusAll         AlertReviewStatus = "all"
)

// AlertReviewQuery defines filters for the alert review surface.
type AlertReviewQuery struct {
	Search        string            // ILIKE substring against listings.title (empty = no filter)
	ComponentType string            // exact match against listings.component_type (empty = no filter)
	WatchID       string            // exact match against alerts.watch_id (empty = no filter)
	MinScore      int               // alerts.score >= MinScore (0 = no filter)
	Status        AlertReviewStatus // see constants above; empty = AlertStatusActive
	Sort          string            // "score" (default) | "created" | "watch"
	Page          int               // 1-indexed; 0 or negative = 1
	PerPage       int               // default 25, hard cap 100
}

// AlertReviewResult is one page of the alert review list plus pagination
// metadata. Total reflects the unpaginated result-set size for the same
// filter so callers can render "page X of Y".
type AlertReviewResult struct {
	Items   []domain.AlertWithListing
	Total   int
	Page    int
	PerPage int
}

// JudgeCandidatesQuery scopes the LLM-as-judge worker's per-tick batch.
// Lookback bounds the alerts.created_at window so we don't repeatedly
// scan the full alert history when the worker is healthy. Limit caps
// the batch — the worker enforces the daily budget separately.
type JudgeCandidatesQuery struct {
	Lookback time.Duration // alerts.created_at >= now() - Lookback
	Limit    int           // 0 = use store default (50)
}

// Store defines all data access operations for server-price-tracker.
type Store interface {
	// Listings
	UpsertListing(ctx context.Context, l *domain.Listing) error
	GetListing(ctx context.Context, ebayID string) (*domain.Listing, error)
	GetListingByID(ctx context.Context, id string) (*domain.Listing, error)
	ListListings(ctx context.Context, opts *ListingQuery) ([]domain.Listing, int, error)
	UpdateListingExtraction(
		ctx context.Context,
		id string,
		componentType string,
		attrs map[string]any,
		confidence float64,
		productKey string,
	) error
	UpdateScore(ctx context.Context, id string, score int, breakdown json.RawMessage) error
	ListUnextractedListings(ctx context.Context, limit int) ([]domain.Listing, error)
	ListUnscoredListings(ctx context.Context, limit int) ([]domain.Listing, error)
	ListIncompleteExtractions(ctx context.Context, componentType string, limit int) ([]domain.Listing, error)
	ListListingsCursor(ctx context.Context, afterID string, limit int) ([]domain.Listing, error)

	// Watches
	CreateWatch(ctx context.Context, w *domain.Watch) error
	GetWatch(ctx context.Context, id string) (*domain.Watch, error)
	ListWatches(ctx context.Context, enabledOnly bool) ([]domain.Watch, error)
	UpdateWatch(ctx context.Context, w *domain.Watch) error
	DeleteWatch(ctx context.Context, id string) error
	SetWatchEnabled(ctx context.Context, id string, enabled bool) error

	// Baselines
	GetBaseline(ctx context.Context, productKey string) (*domain.PriceBaseline, error)
	ListBaselines(ctx context.Context) ([]domain.PriceBaseline, error)
	RecomputeBaseline(ctx context.Context, productKey string, windowDays int) error
	RecomputeAllBaselines(ctx context.Context, windowDays int) error

	// Alerts
	CreateAlert(ctx context.Context, a *domain.Alert) error
	ListPendingAlerts(ctx context.Context) ([]domain.Alert, error)
	ListAlertsByWatch(ctx context.Context, watchID string, limit int) ([]domain.Alert, error)
	MarkAlertNotified(ctx context.Context, id string) error
	MarkAlertsNotified(ctx context.Context, ids []string) error
	HasRecentAlert(ctx context.Context, watchID, listingID string, cooldown time.Duration) (bool, error)
	InsertNotificationAttempt(ctx context.Context, alertID string, succeeded bool, httpStatus int, errText string) error
	HasSuccessfulNotification(ctx context.Context, alertID string) (bool, error)

	// Alert review (DESIGN-0010)
	ListAlertsForReview(ctx context.Context, q *AlertReviewQuery) (AlertReviewResult, error)
	GetAlertDetail(ctx context.Context, id string) (*domain.AlertDetail, error)
	// DismissAlerts marks the given alerts as dismissed (skipping any
	// already dismissed). Returns the number of rows actually
	// transitioned plus the slice of non-empty trace IDs for those rows
	// — the IMPL-0019 alert-review-UI flow scores each dismissed trace
	// in Langfuse so dismissals become labelled training data. NULL or
	// empty trace_id values are filtered out, so callers can iterate
	// the slice directly without a guard.
	DismissAlerts(ctx context.Context, ids []string) (int, []string, error)
	RestoreAlerts(ctx context.Context, ids []string) (int, error)

	// Judge (IMPL-0019 Phase 5)
	//
	// ListAlertsForJudging returns the AlertContext slice the LLM-as-judge
	// worker should evaluate this tick — alerts created in (now-lookback)
	// that don't yet have a row in judge_scores. Limit caps the batch
	// size; 0 means "use the worker's default."
	ListAlertsForJudging(ctx context.Context, q *JudgeCandidatesQuery) ([]domain.JudgeCandidate, error)
	// InsertJudgeScore persists the verdict. Conflict on alert_id is a
	// no-op so the worker can be re-run without poisoning duplicates.
	InsertJudgeScore(ctx context.Context, s *domain.JudgeScore) error
	// SumJudgeCostSince is the daily-budget query. Returns the total
	// cost_usd for verdicts judged on/after `since`; the worker
	// multiplies UTC midnight in.
	SumJudgeCostSince(ctx context.Context, since time.Time) (float64, error)
	// GetJudgeScore returns the persisted verdict for a single alert,
	// or nil + nil error when no row exists yet (pre-judge alerts).
	GetJudgeScore(ctx context.Context, alertID string) (*domain.JudgeScore, error)

	GetSystemState(ctx context.Context) (*domain.SystemState, error)

	// RateLimiterState
	PersistRateLimiterState(ctx context.Context, tokensUsed, dailyLimit int, resetAt time.Time) error
	LoadRateLimiterState(ctx context.Context) (*domain.RateLimiterState, error)

	// Scheduler
	InsertJobRun(ctx context.Context, jobName string) (id string, err error)
	CompleteJobRun(ctx context.Context, id string, status string, errText string, rowsAffected int) error
	ListJobRuns(ctx context.Context, jobName string, limit int) ([]domain.JobRun, error)
	ListLatestJobRuns(ctx context.Context) ([]domain.JobRun, error)
	UpdateWatchLastPolled(ctx context.Context, watchID string, t time.Time) error
	RecoverStaleJobRuns(ctx context.Context, olderThan time.Duration) (int, error)
	AcquireSchedulerLock(ctx context.Context, jobName string, holder string, ttl time.Duration) (bool, error)
	ReleaseSchedulerLock(ctx context.Context, jobName string, holder string) error

	// ExtractionQueue
	EnqueueExtraction(ctx context.Context, listingID string, priority int) error
	DequeueExtractions(ctx context.Context, workerID string, batchSize int) ([]domain.ExtractionJob, error)
	CompleteExtractionJob(ctx context.Context, id string, errText string) error
	CountPendingExtractionJobs(ctx context.Context) (int, error)

	// Migrations
	Migrate(ctx context.Context) error

	// Health
	Ping(ctx context.Context) error
}
