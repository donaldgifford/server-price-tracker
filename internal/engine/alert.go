package engine

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	"github.com/donaldgifford/server-price-tracker/internal/notify"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

const batchThreshold = 5

// ProcessAlerts sends notifications for pending alerts, then marks them as notified.
// Alerts are grouped by watch — if a watch has 5+ pending alerts, they are sent as
// a batch. Failed notifications are not marked as notified.
func ProcessAlerts(
	ctx context.Context,
	s store.Store,
	n notify.Notifier,
) error {
	pending, err := s.ListPendingAlerts(ctx)
	if err != nil {
		return fmt.Errorf("listing pending alerts: %w", err)
	}

	if len(pending) == 0 {
		return nil
	}

	// Group alerts by watch ID.
	grouped := groupByWatch(pending)

	for watchID, alerts := range grouped {
		watch, err := s.GetWatch(ctx, watchID)
		if err != nil {
			continue // watch may have been deleted
		}

		if err := sendAlerts(ctx, s, n, watch, alerts); err != nil {
			metrics.NotificationFailuresTotal.Inc()
			metrics.NotificationLastFailureTimestamp.Set(float64(time.Now().Unix()))
			continue
		}
	}

	return nil
}

func groupByWatch(alerts []domain.Alert) map[string][]domain.Alert {
	grouped := make(map[string][]domain.Alert)
	for _, a := range alerts {
		grouped[a.WatchID] = append(grouped[a.WatchID], a)
	}
	return grouped
}

func sendAlerts(
	ctx context.Context,
	s store.Store,
	n notify.Notifier,
	watch *domain.Watch,
	alerts []domain.Alert,
) error {
	if len(alerts) >= batchThreshold {
		return sendBatch(ctx, s, n, watch, alerts)
	}

	for i := range alerts {
		if err := sendSingle(ctx, s, n, watch, &alerts[i]); err != nil {
			return err
		}
	}

	return nil
}

func sendSingle(
	ctx context.Context,
	s store.Store,
	n notify.Notifier,
	watch *domain.Watch,
	alert *domain.Alert,
) error {
	// Idempotency: skip if already successfully notified (prevents re-send after timeout).
	already, err := s.HasSuccessfulNotification(ctx, alert.ID)
	if err != nil {
		return fmt.Errorf("checking notification status: %w", err)
	}
	if already {
		return nil
	}

	listing, err := s.GetListingByID(ctx, alert.ListingID)
	if err != nil {
		return fmt.Errorf("getting listing %s: %w", alert.ListingID, err)
	}

	payload := buildAlertPayload(watch, listing, alert.Score)
	sendErr := n.SendAlert(ctx, payload)

	// Record the attempt regardless of outcome.
	errText := ""
	if sendErr != nil {
		errText = sendErr.Error()
	}
	if attemptErr := s.InsertNotificationAttempt(ctx, alert.ID, sendErr == nil, 0, errText); attemptErr != nil {
		slog.Default().Warn("failed to record notification attempt",
			"alert_id", alert.ID, "error", attemptErr,
		)
	}

	if sendErr != nil {
		return fmt.Errorf("sending alert: %w", sendErr)
	}

	metrics.AlertsFiredTotal.Inc()
	metrics.NotificationLastSuccessTimestamp.Set(float64(time.Now().Unix()))
	metrics.AlertsFiredByWatch.WithLabelValues(watch.Name).Inc()

	return s.MarkAlertNotified(ctx, alert.ID)
}

func sendBatch(
	ctx context.Context,
	s store.Store,
	n notify.Notifier,
	watch *domain.Watch,
	alerts []domain.Alert,
) error {
	payloads := make([]notify.AlertPayload, 0, len(alerts))
	toSend := make([]domain.Alert, 0, len(alerts))

	for i := range alerts {
		// Idempotency: skip if already successfully notified.
		already, err := s.HasSuccessfulNotification(ctx, alerts[i].ID)
		if err != nil || already {
			continue
		}
		listing, err := s.GetListingByID(ctx, alerts[i].ListingID)
		if err != nil {
			continue // listing may have been removed
		}
		payloads = append(payloads, *buildAlertPayload(watch, listing, alerts[i].Score))
		toSend = append(toSend, alerts[i])
	}

	if len(payloads) == 0 {
		return nil
	}

	sendErr := n.SendBatchAlert(ctx, payloads, watch.Name)

	// Record attempts for every alert that was included in the batch.
	errText := ""
	if sendErr != nil {
		errText = sendErr.Error()
	}
	alertIDs := make([]string, 0, len(toSend))
	for i := range toSend {
		if attemptErr := s.InsertNotificationAttempt(ctx, toSend[i].ID, sendErr == nil, 0, errText); attemptErr != nil {
			slog.Default().Warn("failed to record notification attempt",
				"alert_id", toSend[i].ID, "error", attemptErr,
			)
		}
		alertIDs = append(alertIDs, toSend[i].ID)
	}

	if sendErr != nil {
		return fmt.Errorf("sending batch alert: %w", sendErr)
	}

	metrics.AlertsFiredTotal.Add(float64(len(alertIDs)))
	metrics.NotificationLastSuccessTimestamp.Set(float64(time.Now().Unix()))
	metrics.AlertsFiredByWatch.WithLabelValues(watch.Name).Add(float64(len(alertIDs)))

	return s.MarkAlertsNotified(ctx, alertIDs)
}

func buildAlertPayload(
	watch *domain.Watch,
	listing *domain.Listing,
	score int,
) *notify.AlertPayload {
	return &notify.AlertPayload{
		WatchName:     watch.Name,
		ListingTitle:  listing.Title,
		EbayURL:       listing.ItemURL,
		ImageURL:      listing.ImageURL,
		Price:         fmt.Sprintf("$%.2f", listing.Price),
		UnitPrice:     fmt.Sprintf("$%.2f", listing.UnitPrice()),
		Score:         score,
		Seller:        fmt.Sprintf("%s (%d)", listing.SellerName, listing.SellerFeedback),
		Condition:     string(listing.ConditionNorm),
		ComponentType: string(listing.ComponentType),
	}
}
