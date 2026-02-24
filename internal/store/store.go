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
	CountIncompleteExtractions(ctx context.Context) (int, error)
	CountIncompleteExtractionsByType(ctx context.Context) (map[string]int, error)

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

	// Counts
	//
	// Deprecated: use GetSystemState.
	CountWatches(ctx context.Context) (total int, enabled int, err error)
	//
	// Deprecated: use GetSystemState.
	CountListings(ctx context.Context) (int, error)
	//
	// Deprecated: use GetSystemState.
	CountUnextractedListings(ctx context.Context) (int, error)
	//
	// Deprecated: use GetSystemState.
	CountUnscoredListings(ctx context.Context) (int, error)
	//
	// Deprecated: use GetSystemState.
	CountPendingAlerts(ctx context.Context) (int, error)
	//
	// Deprecated: use GetSystemState.
	CountBaselinesByMaturity(ctx context.Context) (cold int, warm int, err error)
	//
	// Deprecated: use GetSystemState.
	CountProductKeysWithoutBaseline(ctx context.Context) (int, error)
	GetSystemState(ctx context.Context) (*domain.SystemState, error)

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
