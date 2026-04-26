package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	"github.com/donaldgifford/server-price-tracker/internal/notify"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

const batchThreshold = 5

// AlertProcessingConfig controls how ProcessAlerts surfaces pending
// alerts. SummaryOnly collapses every tick into one Discord embed —
// see DESIGN-0010 / IMPL-0015 Phase 6. AlertsURLBase, when non-empty,
// is used as the dashboard hyperlink in the summary embed.
type AlertProcessingConfig struct {
	SummaryOnly   bool
	AlertsURLBase string
}

// ProcessAlerts sends notifications for pending alerts, then marks them as notified.
//
// Default mode (cfg.SummaryOnly=false): alerts are grouped by watch.
// Watches with 5+ pending alerts are sent as a chunked batch; smaller
// groups send per-alert embeds. Failed notifications are not marked.
//
// Summary mode (cfg.SummaryOnly=true): the entire pending pool collapses
// into a single Discord embed regardless of count or watch grouping.
// On success every pending alert is marked notified — operators triage
// via the /alerts page from there. On failure no alerts are marked.
func ProcessAlerts(
	ctx context.Context,
	s store.Store,
	n notify.Notifier,
	cfg AlertProcessingConfig,
) error {
	pending, err := s.ListPendingAlerts(ctx)
	if err != nil {
		return fmt.Errorf("listing pending alerts: %w", err)
	}

	if len(pending) == 0 {
		return nil
	}

	if cfg.SummaryOnly {
		return processSummary(ctx, s, n, pending, cfg.AlertsURLBase)
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

// processSummary delivers one synthesized summary embed for the entire
// pending alert pool. On success: mark every pending alert notified
// and emit per-ID succeeded=true attempts. On failure: emit
// per-ID succeeded=false attempts; do not mark any notified.
func processSummary(
	ctx context.Context,
	s store.Store,
	n notify.Notifier,
	pending []domain.Alert,
	alertsURLBase string,
) error {
	listings := make(map[string]*domain.Listing, len(pending))
	for i := range pending {
		listing, err := s.GetListingByID(ctx, pending[i].ListingID)
		if err != nil {
			continue // skip alerts whose listings have been removed
		}
		listings[pending[i].ID] = listing
	}

	payload := BuildSummaryPayload(pending, listings, alertsURLBase)
	sendErr := n.SendAlert(ctx, payload)

	errText := ""
	if sendErr != nil {
		errText = sendErr.Error()
	}
	ids := make([]string, 0, len(pending))
	for i := range pending {
		recordAttempt(ctx, s, pending[i].ID, sendErr == nil, errText)
		ids = append(ids, pending[i].ID)
	}

	if sendErr != nil {
		metrics.NotificationFailuresTotal.Inc()
		metrics.NotificationLastFailureTimestamp.Set(float64(time.Now().Unix()))
		return fmt.Errorf("sending summary alert: %w", sendErr)
	}

	metrics.AlertsFiredTotal.Add(float64(len(ids)))
	metrics.NotificationLastSuccessTimestamp.Set(float64(time.Now().Unix()))
	if markErr := s.MarkAlertsNotified(ctx, ids); markErr != nil {
		slog.Default().Warn("failed to mark summary alerts notified",
			"count", len(ids), "error", markErr,
		)
	}
	return nil
}

// BuildSummaryPayload synthesizes a single AlertPayload from a pool of
// pending alerts. The listings map (alert ID → listing) is best-effort:
// missing listings just don't contribute their component_type to the
// breakdown. alertsURLBase is the absolute dashboard URL prefix; empty
// means no hyperlink.
//
// SummaryFields are sorted alphabetically so the embed reads
// deterministically — important for tests and for operators
// pattern-matching the same fields across days.
func BuildSummaryPayload(
	pending []domain.Alert,
	listings map[string]*domain.Listing,
	alertsURLBase string,
) *notify.AlertPayload {
	topScore := 0
	counts := make(map[string]int)
	for i := range pending {
		if pending[i].Score > topScore {
			topScore = pending[i].Score
		}
		if l, ok := listings[pending[i].ID]; ok && l != nil {
			counts[string(l.ComponentType)]++
		}
	}

	title := fmt.Sprintf("%d new alerts (top score %d)", len(pending), topScore)

	url := ""
	if alertsURLBase != "" {
		url = alertsURLBase + "/alerts"
	}

	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fields := make([]notify.SummaryField, 0, len(keys))
	for _, k := range keys {
		fields = append(fields, notify.SummaryField{
			Name:  k,
			Value: strconv.Itoa(counts[k]),
		})
	}

	return &notify.AlertPayload{
		WatchName:     "Summary",
		ListingTitle:  title,
		EbayURL:       url,
		Score:         topScore,
		SummaryFields: fields,
	}
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

	sentCount, sendErr := n.SendBatchAlert(ctx, payloads, watch.Name)
	if sentCount < 0 {
		sentCount = 0
	}
	if sentCount > len(toSend) {
		sentCount = len(toSend)
	}

	// Per-ID accounting (resolved Q8): the first sentCount IDs got
	// delivered, the remainder did not. Record both outcomes so the
	// notification_attempts table reflects partial-success batches.
	errText := ""
	if sendErr != nil {
		errText = sendErr.Error()
	}
	delivered := make([]string, 0, sentCount)
	for i := 0; i < sentCount; i++ {
		recordAttempt(ctx, s, toSend[i].ID, true, "")
		delivered = append(delivered, toSend[i].ID)
	}
	for i := sentCount; i < len(toSend); i++ {
		recordAttempt(ctx, s, toSend[i].ID, false, errText)
	}

	if len(delivered) > 0 {
		metrics.AlertsFiredTotal.Add(float64(len(delivered)))
		metrics.NotificationLastSuccessTimestamp.Set(float64(time.Now().Unix()))
		metrics.AlertsFiredByWatch.WithLabelValues(watch.Name).Add(float64(len(delivered)))
		if markErr := s.MarkAlertsNotified(ctx, delivered); markErr != nil {
			slog.Default().Warn("failed to mark alerts notified",
				"watch", watch.Name, "delivered", len(delivered), "error", markErr,
			)
		}
	}

	if sendErr != nil {
		return fmt.Errorf("sending batch alert: %w", sendErr)
	}
	return nil
}

// recordAttempt logs an InsertNotificationAttempt failure but does not
// propagate it — losing the audit row should not unwind a successful
// send. The metric counter still increments so we can monitor write
// loss independently.
func recordAttempt(ctx context.Context, s store.Store, alertID string, succeeded bool, errText string) {
	if attemptErr := s.InsertNotificationAttempt(ctx, alertID, succeeded, 0, errText); attemptErr != nil {
		slog.Default().Warn("failed to record notification attempt",
			"alert_id", alertID, "error", attemptErr,
		)
	}
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
