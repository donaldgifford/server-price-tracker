package engine

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	ptestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/config"
	ebayMocks "github.com/donaldgifford/server-price-tracker/internal/ebay/mocks"
	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	notifyMocks "github.com/donaldgifford/server-price-tracker/internal/notify/mocks"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	extractMocks "github.com/donaldgifford/server-price-tracker/pkg/extract/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func testWatch() *domain.Watch {
	return &domain.Watch{
		ID:             "w1",
		Name:           "DDR4 ECC REG",
		SearchQuery:    "DDR4 ECC REG 32GB",
		ComponentType:  domain.ComponentRAM,
		ScoreThreshold: 75,
		Enabled:        true,
	}
}

func testListingForAlert(id string) *domain.Listing {
	return &domain.Listing{
		ID:       id,
		Title:    "Samsung 32GB DDR4",
		ItemURL:  "https://ebay.com/itm/" + id,
		Price:    45.99,
		Quantity: 1,
	}
}

func TestProcessAlerts_NoPending(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	ms.EXPECT().
		ListPendingAlerts(mock.Anything).
		Return(nil, nil).
		Once()

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)
}

func TestProcessAlerts_SingleAlert(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()
	ms.EXPECT().HasSuccessfulNotification(mock.Anything, "a1").Return(false, nil).Once()
	ms.EXPECT().GetListingByID(mock.Anything, "l1").Return(testListingForAlert("l1"), nil).Once()
	mn.EXPECT().SendAlert(mock.Anything, mock.Anything).Return(nil).Once()
	ms.EXPECT().
		InsertNotificationAttempt(mock.Anything, "a1", true, 0, "").
		Return(nil).Once()
	ms.EXPECT().MarkAlertNotified(mock.Anything, "a1").Return(nil).Once()

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)
}

func TestProcessAlerts_NotifyFails_NotMarked(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()
	ms.EXPECT().HasSuccessfulNotification(mock.Anything, "a1").Return(false, nil).Once()
	ms.EXPECT().GetListingByID(mock.Anything, "l1").Return(&domain.Listing{
		ID: "l1", Title: "Test Listing", Price: 45.99, Quantity: 1,
	}, nil).Once()
	mn.EXPECT().SendAlert(mock.Anything, mock.Anything).Return(errors.New("discord 429")).Once()
	ms.EXPECT().
		InsertNotificationAttempt(mock.Anything, "a1", false, 0, "discord 429").
		Return(nil).Once()
	// MarkAlertNotified should NOT be called when send fails.

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err) // ProcessAlerts logs errors, doesn't return them
}

func TestProcessAlerts_BatchAlert(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	// 5 alerts for same watch → batch threshold met.
	alerts := make([]domain.Alert, 5)
	for i := range alerts {
		alerts[i] = domain.Alert{
			ID:        "a" + string(rune('1'+i)),
			WatchID:   "w1",
			ListingID: "l" + string(rune('1'+i)),
			Score:     80 + i,
		}
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()

	for i := range alerts {
		ms.EXPECT().
			HasSuccessfulNotification(mock.Anything, alerts[i].ID).
			Return(false, nil).Once()
		ms.EXPECT().
			GetListingByID(mock.Anything, alerts[i].ListingID).
			Return(&domain.Listing{
				ID:       alerts[i].ListingID,
				Title:    "Listing " + string(rune('A'+i)),
				Price:    float64(40 + i),
				Quantity: 1,
			}, nil).Once()
	}

	mn.EXPECT().SendBatchAlert(mock.Anything, mock.Anything, "DDR4 ECC REG").Return(len(alerts), nil).Once()

	for i := range alerts {
		ms.EXPECT().
			InsertNotificationAttempt(mock.Anything, alerts[i].ID, true, 0, "").
			Return(nil).Once()
	}

	alertIDs := make([]string, 5)
	for i := range alertIDs {
		alertIDs[i] = alerts[i].ID
	}
	ms.EXPECT().MarkAlertsNotified(mock.Anything, alertIDs).Return(nil).Once()

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)
}

func TestProcessAlerts_IndividualAlerts(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	// 3 alerts for same watch → below batch threshold.
	alerts := make([]domain.Alert, 3)
	for i := range alerts {
		alerts[i] = domain.Alert{
			ID:        "a" + string(rune('1'+i)),
			WatchID:   "w1",
			ListingID: "l" + string(rune('1'+i)),
			Score:     80 + i,
		}
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()

	for i := range alerts {
		ms.EXPECT().
			HasSuccessfulNotification(mock.Anything, alerts[i].ID).
			Return(false, nil).Once()
		ms.EXPECT().
			GetListingByID(mock.Anything, alerts[i].ListingID).
			Return(&domain.Listing{
				ID: alerts[i].ListingID, Title: "Listing", Price: 45.0, Quantity: 1,
			}, nil).Once()
		mn.EXPECT().SendAlert(mock.Anything, mock.Anything).Return(nil).Once()
		ms.EXPECT().
			InsertNotificationAttempt(mock.Anything, alerts[i].ID, true, 0, "").
			Return(nil).Once()
		ms.EXPECT().MarkAlertNotified(mock.Anything, alerts[i].ID).Return(nil).Once()
	}

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)
}

func TestProcessAlerts_StoreError(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	ms.EXPECT().
		ListPendingAlerts(mock.Anything).
		Return(nil, errors.New("db error")).
		Once()

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing pending alerts")
}

func TestGroupByWatch(t *testing.T) {
	t.Parallel()

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1"},
		{ID: "a2", WatchID: "w1"},
		{ID: "a3", WatchID: "w2"},
	}

	grouped := groupByWatch(alerts)
	assert.Len(t, grouped, 2)
	assert.Len(t, grouped["w1"], 2)
	assert.Len(t, grouped["w2"], 1)
}

func TestProcessAlerts_SetsSuccessTimestamp(t *testing.T) {
	// Not parallel: reads global Prometheus gauge.
	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()
	ms.EXPECT().HasSuccessfulNotification(mock.Anything, "a1").Return(false, nil).Once()
	ms.EXPECT().GetListingByID(mock.Anything, "l1").Return(&domain.Listing{
		ID: "l1", Title: "Test", Price: 45.99, Quantity: 1,
	}, nil).Once()
	mn.EXPECT().SendAlert(mock.Anything, mock.Anything).Return(nil).Once()
	ms.EXPECT().InsertNotificationAttempt(mock.Anything, "a1", true, 0, "").Return(nil).Once()
	ms.EXPECT().MarkAlertNotified(mock.Anything, "a1").Return(nil).Once()

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)

	ts := ptestutil.ToFloat64(metrics.NotificationLastSuccessTimestamp)
	assert.Greater(t, ts, float64(0), "NotificationLastSuccessTimestamp should be set")
}

func TestProcessAlerts_SetsFailureTimestamp(t *testing.T) {
	// Not parallel: reads global Prometheus gauge.
	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()
	ms.EXPECT().HasSuccessfulNotification(mock.Anything, "a1").Return(false, nil).Once()
	ms.EXPECT().GetListingByID(mock.Anything, "l1").Return(&domain.Listing{
		ID: "l1", Title: "Test", Price: 45.99, Quantity: 1,
	}, nil).Once()
	mn.EXPECT().SendAlert(mock.Anything, mock.Anything).Return(errors.New("discord 429")).Once()
	ms.EXPECT().
		InsertNotificationAttempt(mock.Anything, "a1", false, 0, "discord 429").
		Return(nil).Once()

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)

	ts := ptestutil.ToFloat64(metrics.NotificationLastFailureTimestamp)
	assert.Greater(t, ts, float64(0), "NotificationLastFailureTimestamp should be set")
}

func TestProcessAlerts_IncrementsAlertsFiredByWatch(t *testing.T) {
	// Not parallel: reads global Prometheus counter.
	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
		{ID: "a2", WatchID: "w1", ListingID: "l2", Score: 90},
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()

	for _, a := range alerts {
		ms.EXPECT().HasSuccessfulNotification(mock.Anything, a.ID).Return(false, nil).Once()
		ms.EXPECT().GetListingByID(mock.Anything, a.ListingID).Return(&domain.Listing{
			ID: a.ListingID, Title: "Test", Price: 45.99, Quantity: 1,
		}, nil).Once()
		mn.EXPECT().SendAlert(mock.Anything, mock.Anything).Return(nil).Once()
		ms.EXPECT().InsertNotificationAttempt(mock.Anything, a.ID, true, 0, "").Return(nil).Once()
		ms.EXPECT().MarkAlertNotified(mock.Anything, a.ID).Return(nil).Once()
	}

	before := ptestutil.ToFloat64(metrics.AlertsFiredByWatch.WithLabelValues("DDR4 ECC REG"))

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)

	after := ptestutil.ToFloat64(metrics.AlertsFiredByWatch.WithLabelValues("DDR4 ECC REG"))
	assert.InDelta(t, 2, after-before, 0.1, "AlertsFiredByWatch should increment by 2")
}

// --- New Phase 2 tests ---

func TestProcessAlerts_SkipsAlreadyNotified(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()
	// Already successfully notified — skip the send entirely.
	ms.EXPECT().HasSuccessfulNotification(mock.Anything, "a1").Return(true, nil).Once()
	// SendAlert, InsertNotificationAttempt, MarkAlertNotified should NOT be called.

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)
}

func TestProcessAlerts_RecordsFailedAttempt(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()
	ms.EXPECT().HasSuccessfulNotification(mock.Anything, "a1").Return(false, nil).Once()
	ms.EXPECT().GetListingByID(mock.Anything, "l1").Return(testListingForAlert("l1"), nil).Once()
	mn.EXPECT().SendAlert(mock.Anything, mock.Anything).Return(errors.New("webhook timeout")).Once()
	// Must record a failed attempt.
	ms.EXPECT().
		InsertNotificationAttempt(mock.Anything, "a1", false, 0, "webhook timeout").
		Return(nil).Once()
	// MarkAlertNotified is not expected when the send fails.

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)
}

func TestProcessAlerts_RecordsSuccessfulAttempt(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()
	ms.EXPECT().HasSuccessfulNotification(mock.Anything, "a1").Return(false, nil).Once()
	ms.EXPECT().GetListingByID(mock.Anything, "l1").Return(testListingForAlert("l1"), nil).Once()
	mn.EXPECT().SendAlert(mock.Anything, mock.Anything).Return(nil).Once()
	// Must record a successful attempt.
	ms.EXPECT().
		InsertNotificationAttempt(mock.Anything, "a1", true, 0, "").
		Return(nil).Once()
	ms.EXPECT().MarkAlertNotified(mock.Anything, "a1").Return(nil).Once()

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)
}

func TestProcessAlerts_WatchDeletedSkipped(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "deleted-watch", ListingID: "l1", Score: 85},
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	// Watch has been deleted — GetWatch returns error.
	ms.EXPECT().GetWatch(mock.Anything, "deleted-watch").
		Return(nil, errors.New("not found")).Once()
	// sendAlerts must NOT be called when watch is missing.

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)
}

func TestSendBatch_PartialListingsMissing(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	// 5 alerts (meets batch threshold); middle listing is missing.
	alerts := make([]domain.Alert, 5)
	for i := range alerts {
		alerts[i] = domain.Alert{
			ID:        "a" + string(rune('1'+i)),
			WatchID:   "w1",
			ListingID: "l" + string(rune('1'+i)),
			Score:     80,
		}
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()

	for i := range alerts {
		ms.EXPECT().
			HasSuccessfulNotification(mock.Anything, alerts[i].ID).
			Return(false, nil).Once()
		if i == 2 {
			// Middle listing is gone — skip from batch silently.
			ms.EXPECT().
				GetListingByID(mock.Anything, alerts[i].ListingID).
				Return(nil, errors.New("listing removed")).Once()
		} else {
			ms.EXPECT().
				GetListingByID(mock.Anything, alerts[i].ListingID).
				Return(&domain.Listing{
					ID:       alerts[i].ListingID,
					Title:    "Listing",
					Price:    45.0,
					Quantity: 1,
				}, nil).Once()
		}
	}

	// Batch sent with 4 items (the missing one is excluded).
	mn.EXPECT().SendBatchAlert(mock.Anything, mock.Anything, "DDR4 ECC REG").Return(4, nil).Once()

	// InsertNotificationAttempt only for the 4 items that were included.
	for i := range alerts {
		if i != 2 {
			ms.EXPECT().
				InsertNotificationAttempt(mock.Anything, alerts[i].ID, true, 0, "").
				Return(nil).Once()
		}
	}

	includedIDs := []string{alerts[0].ID, alerts[1].ID, alerts[3].ID, alerts[4].ID}
	ms.EXPECT().MarkAlertsNotified(mock.Anything, includedIDs).Return(nil).Once()

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)
}

func TestSendBatch_SendFails_AttemptsRecorded(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	// 5 alerts for the same watch.
	alerts := make([]domain.Alert, 5)
	for i := range alerts {
		alerts[i] = domain.Alert{
			ID:        "a" + string(rune('1'+i)),
			WatchID:   "w1",
			ListingID: "l" + string(rune('1'+i)),
			Score:     80,
		}
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()

	for i := range alerts {
		ms.EXPECT().
			HasSuccessfulNotification(mock.Anything, alerts[i].ID).
			Return(false, nil).Once()
		ms.EXPECT().
			GetListingByID(mock.Anything, alerts[i].ListingID).
			Return(&domain.Listing{
				ID: alerts[i].ListingID, Title: "Listing", Price: 45.0, Quantity: 1,
			}, nil).Once()
	}

	sendErr := errors.New("discord 429")
	mn.EXPECT().SendBatchAlert(mock.Anything, mock.Anything, "DDR4 ECC REG").
		Return(0, sendErr).Once()

	// Attempts recorded as failed for all included alerts.
	for i := range alerts {
		ms.EXPECT().
			InsertNotificationAttempt(mock.Anything, alerts[i].ID, false, 0, "discord 429").
			Return(nil).Once()
	}
	// MarkAlertsNotified must NOT be called when send fails.

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err) // ProcessAlerts absorbs per-watch errors
}

// TestProcessAlerts_BatchPartialFailure verifies the partial-success
// path: SendBatchAlert reports 3 of 5 alerts delivered before erroring
// out. The first 3 must be marked notified with succeeded=true; the
// last 2 get succeeded=false attempt rows. Per-ID accounting is the
// resolved Q8 outcome.
func TestProcessAlerts_BatchPartialFailure(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := make([]domain.Alert, 5)
	for i := range alerts {
		alerts[i] = domain.Alert{
			ID:        "a" + string(rune('1'+i)),
			WatchID:   "w1",
			ListingID: "l" + string(rune('1'+i)),
			Score:     80,
		}
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()

	for i := range alerts {
		ms.EXPECT().
			HasSuccessfulNotification(mock.Anything, alerts[i].ID).
			Return(false, nil).Once()
		ms.EXPECT().
			GetListingByID(mock.Anything, alerts[i].ListingID).
			Return(&domain.Listing{
				ID: alerts[i].ListingID, Title: "Listing", Price: 45.0, Quantity: 1,
			}, nil).Once()
	}

	const sentCount = 3
	sendErr := errors.New("chunk 2/2: discord 429")
	mn.EXPECT().SendBatchAlert(mock.Anything, mock.Anything, "DDR4 ECC REG").
		Return(sentCount, sendErr).Once()

	for i := 0; i < sentCount; i++ {
		ms.EXPECT().
			InsertNotificationAttempt(mock.Anything, alerts[i].ID, true, 0, "").
			Return(nil).Once()
	}
	for i := sentCount; i < len(alerts); i++ {
		ms.EXPECT().
			InsertNotificationAttempt(mock.Anything, alerts[i].ID, false, 0, sendErr.Error()).
			Return(nil).Once()
	}

	deliveredIDs := []string{alerts[0].ID, alerts[1].ID, alerts[2].ID}
	ms.EXPECT().MarkAlertsNotified(mock.Anything, deliveredIDs).Return(nil).Once()

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err) // ProcessAlerts absorbs per-watch errors
}

func TestEvaluateAlert_CooldownZero_SkipsRecentCheck(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	score := 85
	listing := &domain.Listing{ID: "l1", Score: &score}
	watch := testWatch() // threshold is 75

	// Cooldown=0 (test default): HasRecentAlert must NOT be called.
	// CreateAlert is called (partial unique index prevents duplicate pending alerts).
	ms.EXPECT().
		CreateAlert(mock.Anything, mock.MatchedBy(func(a *domain.Alert) bool {
			return a.WatchID == "w1" && a.ListingID == "l1"
		})).
		Return(nil).Once()

	eng := newTestEngine(ms, me, mx, mn)
	eng.evaluateAlert(context.Background(), watch, listing)
}

func TestEvaluateAlert_CooldownActive_SkipsRecentListing(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	score := 85
	listing := &domain.Listing{ID: "l1", Score: &score}
	watch := testWatch()

	// HasRecentAlert returns true — alert should be skipped.
	ms.EXPECT().
		HasRecentAlert(mock.Anything, "w1", "l1", 24*time.Hour).
		Return(true, nil).Once()
	// CreateAlert must NOT be called.

	eng := newTestEngine(ms, me, mx, mn)
	eng.alertsConfig = config.AlertsConfig{
		ReAlertsCooldown: 24 * time.Hour,
	}
	eng.evaluateAlert(context.Background(), watch, listing)
}

func TestProcessAlerts_HasSuccessfulNotificationError(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()
	// HasSuccessfulNotification returns an error — sendSingle propagates it.
	ms.EXPECT().
		HasSuccessfulNotification(mock.Anything, "a1").
		Return(false, errors.New("db error")).Once()
	// sendSingle returns the error; ProcessAlerts increments failure metric and continues.

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err) // ProcessAlerts absorbs per-watch errors
}

func TestSendBatch_AllAlreadyNotified(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	// 5 alerts for same watch (meets batch threshold), but all already notified.
	alerts := make([]domain.Alert, 5)
	for i := range alerts {
		alerts[i] = domain.Alert{
			ID:        "a" + string(rune('1'+i)),
			WatchID:   "w1",
			ListingID: "l" + string(rune('1'+i)),
			Score:     80,
		}
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	ms.EXPECT().GetWatch(mock.Anything, "w1").Return(testWatch(), nil).Once()

	for i := range alerts {
		ms.EXPECT().
			HasSuccessfulNotification(mock.Anything, alerts[i].ID).
			Return(true, nil).Once()
	}
	// SendBatchAlert must NOT be called — all payloads were filtered out.

	err := ProcessAlerts(context.Background(), ms, mn, AlertProcessingConfig{})
	require.NoError(t, err)
}

func TestEvaluateAlert_CooldownActive_AllowsAfterWindow(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	score := 85
	listing := &domain.Listing{ID: "l1", Score: &score}
	watch := testWatch()

	// HasRecentAlert returns false — cooldown has expired, alert is allowed.
	ms.EXPECT().
		HasRecentAlert(mock.Anything, "w1", "l1", 24*time.Hour).
		Return(false, nil).Once()
	ms.EXPECT().CreateAlert(mock.Anything, mock.Anything).Return(nil).Once()

	eng := newTestEngine(ms, me, mx, mn)
	eng.alertsConfig = config.AlertsConfig{
		ReAlertsCooldown: 24 * time.Hour,
	}
	eng.evaluateAlert(context.Background(), watch, listing)
}

// === IMPL-0015 Phase 6: summary mode ===

func TestBuildSummaryPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		alerts         []domain.Alert
		listings       map[string]*domain.Listing
		alertsURLBase  string
		wantTitle      string
		wantTopScore   int
		wantURL        string
		wantFieldNames []string
	}{
		{
			name:           "empty pool",
			alerts:         nil,
			listings:       map[string]*domain.Listing{},
			alertsURLBase:  "https://spt.example.com",
			wantTitle:      "0 new alerts (top score 0)",
			wantTopScore:   0,
			wantURL:        "https://spt.example.com/alerts",
			wantFieldNames: []string{},
		},
		{
			name: "mixed component types",
			alerts: []domain.Alert{
				{ID: "a1", Score: 85},
				{ID: "a2", Score: 92},
				{ID: "a3", Score: 78},
			},
			listings: map[string]*domain.Listing{
				"a1": {ComponentType: domain.ComponentRAM},
				"a2": {ComponentType: domain.ComponentServer},
				"a3": {ComponentType: domain.ComponentRAM},
			},
			alertsURLBase:  "https://spt.example.com",
			wantTitle:      "3 new alerts (top score 92)",
			wantTopScore:   92,
			wantURL:        "https://spt.example.com/alerts",
			wantFieldNames: []string{"ram", "server"}, // sorted
		},
		{
			name: "no alerts URL base omits link",
			alerts: []domain.Alert{
				{ID: "a1", Score: 80},
			},
			listings: map[string]*domain.Listing{
				"a1": {ComponentType: domain.ComponentRAM},
			},
			alertsURLBase:  "",
			wantTitle:      "1 new alerts (top score 80)",
			wantTopScore:   80,
			wantURL:        "",
			wantFieldNames: []string{"ram"},
		},
		{
			name: "missing listing skips count",
			alerts: []domain.Alert{
				{ID: "a1", Score: 88},
				{ID: "a2", Score: 70},
			},
			listings: map[string]*domain.Listing{
				"a1": {ComponentType: domain.ComponentNIC},
			},
			alertsURLBase:  "https://spt.example.com",
			wantTitle:      "2 new alerts (top score 88)",
			wantTopScore:   88,
			wantURL:        "https://spt.example.com/alerts",
			wantFieldNames: []string{"nic"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := BuildSummaryPayload(tt.alerts, tt.listings, tt.alertsURLBase)
			assert.Equal(t, "Summary", payload.WatchName)
			assert.Equal(t, tt.wantTitle, payload.ListingTitle)
			assert.Equal(t, tt.wantTopScore, payload.Score)
			assert.Equal(t, tt.wantURL, payload.EbayURL)

			gotNames := make([]string, 0, len(payload.SummaryFields))
			for _, f := range payload.SummaryFields {
				gotNames = append(gotNames, f.Name)
			}
			assert.Equal(t, tt.wantFieldNames, gotNames)
		})
	}
}

func TestProcessAlerts_SummaryMode_SingleEmbed(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	const n = 50
	alerts := make([]domain.Alert, n)
	for i := range alerts {
		alerts[i] = domain.Alert{
			ID:        "a" + strconv.Itoa(i),
			WatchID:   "w1",
			ListingID: "l" + strconv.Itoa(i),
			Score:     75 + (i % 25),
		}
	}

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	for i := range alerts {
		ms.EXPECT().
			GetListingByID(mock.Anything, alerts[i].ListingID).
			Return(&domain.Listing{ID: alerts[i].ListingID, ComponentType: domain.ComponentRAM}, nil).
			Once()
	}
	mn.EXPECT().SendAlert(mock.Anything, mock.Anything).Return(nil).Once()
	for i := range alerts {
		ms.EXPECT().
			InsertNotificationAttempt(mock.Anything, alerts[i].ID, true, 0, "").
			Return(nil).Once()
	}
	expectedIDs := make([]string, n)
	for i := range alerts {
		expectedIDs[i] = alerts[i].ID
	}
	ms.EXPECT().MarkAlertsNotified(mock.Anything, expectedIDs).Return(nil).Once()

	cfg := AlertProcessingConfig{SummaryOnly: true, AlertsURLBase: "https://spt.example.com"}
	err := ProcessAlerts(context.Background(), ms, mn, cfg)
	require.NoError(t, err)
}

func TestProcessAlerts_SummaryMode_NoNewAlerts(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	cfg := AlertProcessingConfig{SummaryOnly: true}
	err := ProcessAlerts(context.Background(), ms, mn, cfg)
	require.NoError(t, err)
}

func TestProcessAlerts_SummaryMode_SendFailure(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
		{ID: "a2", WatchID: "w1", ListingID: "l2", Score: 92},
	}
	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(alerts, nil).Once()
	for i := range alerts {
		ms.EXPECT().
			GetListingByID(mock.Anything, alerts[i].ListingID).
			Return(&domain.Listing{ID: alerts[i].ListingID, ComponentType: domain.ComponentRAM}, nil).
			Once()
	}
	sendErr := errors.New("discord 500")
	mn.EXPECT().SendAlert(mock.Anything, mock.Anything).Return(sendErr).Once()
	for i := range alerts {
		ms.EXPECT().
			InsertNotificationAttempt(mock.Anything, alerts[i].ID, false, 0, sendErr.Error()).
			Return(nil).Once()
	}
	// MarkAlertsNotified MUST NOT be called.

	cfg := AlertProcessingConfig{SummaryOnly: true}
	err := ProcessAlerts(context.Background(), ms, mn, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sending summary alert")
}
