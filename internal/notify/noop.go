package notify

import (
	"context"
	"log/slog"
)

// NoOpNotifier implements Notifier by logging discarded alerts. It is used
// when Discord (or another notification backend) is not configured.
type NoOpNotifier struct {
	log *slog.Logger
}

// NewNoOpNotifier creates a notifier that discards alerts with a log message.
func NewNoOpNotifier(log *slog.Logger) *NoOpNotifier {
	return &NoOpNotifier{log: log}
}

// SendAlert logs and discards a single alert.
func (n *NoOpNotifier) SendAlert(_ context.Context, alert *AlertPayload) error {
	n.log.Debug("notification discarded (no backend configured)",
		"watch", alert.WatchName,
		"listing", alert.ListingTitle,
		"score", alert.Score,
	)
	return nil
}

// SendBatchAlert logs and discards a batch of alerts.
func (n *NoOpNotifier) SendBatchAlert(_ context.Context, alerts []AlertPayload, watchName string) error {
	n.log.Debug("batch notification discarded (no backend configured)",
		"watch", watchName,
		"count", len(alerts),
	)
	return nil
}
