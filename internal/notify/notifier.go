// Package notify defines the notification interface and implementations
// for alert delivery.
package notify

import (
	"context"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// AlertPayload contains the data needed to send a deal alert notification.
type AlertPayload struct {
	WatchName     string
	ListingTitle  string
	EbayURL       string
	ImageURL      string
	Price         string
	UnitPrice     string
	Score         int
	Breakdown     domain.ScoreBreakdown
	Baseline      *domain.PriceBaseline
	Seller        string
	Condition     string
	ComponentType string
}

// Notifier defines the interface for sending deal alert notifications.
//
// SendBatchAlert returns the number of alerts successfully delivered so
// callers can record per-alert outcomes (deliver = true for the first
// `sent` IDs; false for the remainder when err != nil). This shape was
// introduced when chunked Discord sends replaced single-message
// truncation — see DESIGN-0009 / IMPL-0015 Phase 5.
type Notifier interface {
	SendAlert(ctx context.Context, alert *AlertPayload) error
	SendBatchAlert(ctx context.Context, alerts []AlertPayload, watchName string) (sent int, err error)
}
