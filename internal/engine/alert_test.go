package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	notifyMocks "github.com/donaldgifford/server-price-tracker/internal/notify/mocks"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
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

func TestProcessAlerts_NoPending(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	ms.EXPECT().
		ListPendingAlerts(mock.Anything).
		Return(nil, nil).
		Once()

	err := ProcessAlerts(context.Background(), ms, mn)
	require.NoError(t, err)
}

func TestProcessAlerts_SingleAlert(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
	}

	ms.EXPECT().
		ListPendingAlerts(mock.Anything).
		Return(alerts, nil).
		Once()

	ms.EXPECT().
		GetWatch(mock.Anything, "w1").
		Return(testWatch(), nil).
		Once()

	ms.EXPECT().
		GetListingByID(mock.Anything, "l1").
		Return(&domain.Listing{
			ID:       "l1",
			Title:    "Samsung 32GB DDR4",
			ItemURL:  "https://ebay.com/itm/l1",
			Price:    45.99,
			Quantity: 1,
		}, nil).
		Once()

	mn.EXPECT().
		SendAlert(mock.Anything, mock.Anything).
		Return(nil).
		Once()

	ms.EXPECT().
		MarkAlertNotified(mock.Anything, "a1").
		Return(nil).
		Once()

	err := ProcessAlerts(context.Background(), ms, mn)
	require.NoError(t, err)
}

func TestProcessAlerts_NotifyFails_NotMarked(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	mn := notifyMocks.NewMockNotifier(t)

	alerts := []domain.Alert{
		{ID: "a1", WatchID: "w1", ListingID: "l1", Score: 85},
	}

	ms.EXPECT().
		ListPendingAlerts(mock.Anything).
		Return(alerts, nil).
		Once()

	ms.EXPECT().
		GetWatch(mock.Anything, "w1").
		Return(testWatch(), nil).
		Once()

	ms.EXPECT().
		GetListingByID(mock.Anything, "l1").
		Return(&domain.Listing{
			ID:       "l1",
			Title:    "Test Listing",
			Price:    45.99,
			Quantity: 1,
		}, nil).
		Once()

	mn.EXPECT().
		SendAlert(mock.Anything, mock.Anything).
		Return(errors.New("discord 429")).
		Once()

	// MarkAlertNotified should NOT be called when send fails.

	err := ProcessAlerts(context.Background(), ms, mn)
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

	ms.EXPECT().
		ListPendingAlerts(mock.Anything).
		Return(alerts, nil).
		Once()

	ms.EXPECT().
		GetWatch(mock.Anything, "w1").
		Return(testWatch(), nil).
		Once()

	// 5 GetListingByID calls.
	for i := range alerts {
		ms.EXPECT().
			GetListingByID(mock.Anything, alerts[i].ListingID).
			Return(&domain.Listing{
				ID:       alerts[i].ListingID,
				Title:    "Listing " + string(rune('A'+i)),
				Price:    float64(40 + i),
				Quantity: 1,
			}, nil).
			Once()
	}

	mn.EXPECT().
		SendBatchAlert(mock.Anything, mock.Anything, "DDR4 ECC REG").
		Return(nil).
		Once()

	alertIDs := make([]string, 5)
	for i := range alertIDs {
		alertIDs[i] = alerts[i].ID
	}

	ms.EXPECT().
		MarkAlertsNotified(mock.Anything, alertIDs).
		Return(nil).
		Once()

	err := ProcessAlerts(context.Background(), ms, mn)
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

	ms.EXPECT().
		ListPendingAlerts(mock.Anything).
		Return(alerts, nil).
		Once()

	ms.EXPECT().
		GetWatch(mock.Anything, "w1").
		Return(testWatch(), nil).
		Once()

	for i := range alerts {
		ms.EXPECT().
			GetListingByID(mock.Anything, alerts[i].ListingID).
			Return(&domain.Listing{
				ID:       alerts[i].ListingID,
				Title:    "Listing",
				Price:    45.0,
				Quantity: 1,
			}, nil).
			Once()

		mn.EXPECT().
			SendAlert(mock.Anything, mock.Anything).
			Return(nil).
			Once()

		ms.EXPECT().
			MarkAlertNotified(mock.Anything, alerts[i].ID).
			Return(nil).
			Once()
	}

	err := ProcessAlerts(context.Background(), ms, mn)
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

	err := ProcessAlerts(context.Background(), ms, mn)
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
