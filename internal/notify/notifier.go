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
type Notifier interface {
	SendAlert(ctx context.Context, alert AlertPayload) error
	SendBatchAlert(ctx context.Context, alerts []AlertPayload, watchName string) error
}
