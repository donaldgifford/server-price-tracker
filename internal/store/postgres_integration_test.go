//go:build integration

package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/donaldgifford/server-price-tracker/internal/store"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func setupPostgres(t *testing.T) *store.PostgresStore {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("spt_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, pgContainer.Terminate(ctx))
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	s, err := store.NewPostgresStore(ctx, connStr)
	require.NoError(t, err)

	t.Cleanup(func() {
		s.Close()
	})

	require.NoError(t, s.Migrate(ctx))

	return s
}

func testListing() *domain.Listing {
	now := time.Now().Truncate(time.Microsecond)
	return &domain.Listing{
		EbayID:            "123456789",
		Title:             "Samsung 32GB 2Rx4 PC4-2666V DDR4 ECC REG",
		ItemURL:           "https://www.ebay.com/itm/123456789",
		ImageURL:          "https://i.ebayimg.com/images/g/test/s-l1600.jpg",
		Price:             45.99,
		Currency:          "USD",
		ListingType:       domain.ListingBuyItNow,
		SellerName:        "server_parts_inc",
		SellerFeedback:    5432,
		SellerFeedbackPct: 99.8,
		SellerTopRated:    true,
		ConditionRaw:      "Used",
		ConditionNorm:     domain.ConditionUsedWorking,
		Quantity:          1,
		ListedAt:          &now,
	}
}

func TestPostgresStore_Ping(t *testing.T) {
	s := setupPostgres(t)
	require.NoError(t, s.Ping(context.Background()))
}

func TestPostgresStore_UpsertListing(t *testing.T) {
	s := setupPostgres(t)
	ctx := context.Background()

	t.Run("insert new listing", func(t *testing.T) {
		l := testListing()
		err := s.UpsertListing(ctx, l)
		require.NoError(t, err)
		assert.NotEmpty(t, l.ID)
		assert.False(t, l.FirstSeenAt.IsZero())
		assert.False(t, l.UpdatedAt.IsZero())
	})

	t.Run("upsert with changed price", func(t *testing.T) {
		l := testListing()
		l.EbayID = "upsert-test-1"
		err := s.UpsertListing(ctx, l)
		require.NoError(t, err)
		firstID := l.ID
		firstSeen := l.FirstSeenAt

		// Update with new price.
		l2 := testListing()
		l2.EbayID = "upsert-test-1"
		l2.Price = 39.99
		err = s.UpsertListing(ctx, l2)
		require.NoError(t, err)

		// Same ID, same first_seen_at, but updated price.
		assert.Equal(t, firstID, l2.ID)
		assert.Equal(t, firstSeen, l2.FirstSeenAt)

		// Verify via GetListing.
		got, err := s.GetListing(ctx, "upsert-test-1")
		require.NoError(t, err)
		assert.InDelta(t, 39.99, got.Price, 0.01)
	})
}

func TestPostgresStore_GetListing(t *testing.T) {
	s := setupPostgres(t)
	ctx := context.Background()

	t.Run("found", func(t *testing.T) {
		l := testListing()
		l.EbayID = "get-test-1"
		require.NoError(t, s.UpsertListing(ctx, l))

		got, err := s.GetListing(ctx, "get-test-1")
		require.NoError(t, err)
		assert.Equal(t, l.ID, got.ID)
		assert.Equal(t, "Samsung 32GB 2Rx4 PC4-2666V DDR4 ECC REG", got.Title)
	})

	t.Run("not found", func(t *testing.T) {
		_, err := s.GetListing(ctx, "nonexistent")
		assert.Error(t, err)
	})
}

func TestPostgresStore_GetListingByID(t *testing.T) {
	s := setupPostgres(t)
	ctx := context.Background()

	l := testListing()
	l.EbayID = "get-by-id-1"
	require.NoError(t, s.UpsertListing(ctx, l))

	got, err := s.GetListingByID(ctx, l.ID)
	require.NoError(t, err)
	assert.Equal(t, "get-by-id-1", got.EbayID)
}

func TestPostgresStore_UpdateListingExtraction(t *testing.T) {
	s := setupPostgres(t)
	ctx := context.Background()

	l := testListing()
	l.EbayID = "extract-test-1"
	require.NoError(t, s.UpsertListing(ctx, l))

	attrs := map[string]any{
		"manufacturer": "Samsung",
		"capacity_gb":  float64(32),
		"generation":   "DDR4",
		"ecc":          true,
		"registered":   true,
	}

	err := s.UpdateListingExtraction(ctx, l.ID, "ram", attrs, 0.95, "ram:ddr4:ecc_reg:32gb:2666")
	require.NoError(t, err)

	got, err := s.GetListingByID(ctx, l.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ComponentRAM, got.ComponentType)
	assert.InDelta(t, 0.95, got.ExtractionConfidence, 0.01)
	assert.Equal(t, "ram:ddr4:ecc_reg:32gb:2666", got.ProductKey)
	assert.Equal(t, "Samsung", got.Attributes["manufacturer"])
}

func TestPostgresStore_UpdateScore(t *testing.T) {
	s := setupPostgres(t)
	ctx := context.Background()

	l := testListing()
	l.EbayID = "score-test-1"
	require.NoError(t, s.UpsertListing(ctx, l))

	breakdown := json.RawMessage(`{"price":80,"seller":90,"condition":70}`)
	err := s.UpdateScore(ctx, l.ID, 82, breakdown)
	require.NoError(t, err)

	got, err := s.GetListingByID(ctx, l.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Score)
	assert.Equal(t, 82, *got.Score)
}

func TestPostgresStore_ListListings(t *testing.T) {
	s := setupPostgres(t)
	ctx := context.Background()

	// Insert several listings.
	for i := range 5 {
		l := testListing()
		l.EbayID = "list-test-" + string(rune('a'+i))
		l.Price = float64(40 + i*10)
		require.NoError(t, s.UpsertListing(ctx, l))
	}

	t.Run("no filters", func(t *testing.T) {
		q := &store.ListingQuery{Limit: 10}
		listings, total, err := s.ListListings(ctx, q)
		require.NoError(t, err)
		assert.Equal(t, 5, total)
		assert.Len(t, listings, 5)
	})

	t.Run("with limit and offset", func(t *testing.T) {
		q := &store.ListingQuery{Limit: 2, Offset: 0}
		listings, total, err := s.ListListings(ctx, q)
		require.NoError(t, err)
		assert.Equal(t, 5, total)
		assert.Len(t, listings, 2)
	})

	t.Run("pagination total count is correct", func(t *testing.T) {
		q := &store.ListingQuery{Limit: 1, Offset: 4}
		listings, total, err := s.ListListings(ctx, q)
		require.NoError(t, err)
		assert.Equal(t, 5, total)
		assert.Len(t, listings, 1)
	})
}

func TestPostgresStore_ListUnextractedListings(t *testing.T) {
	s := setupPostgres(t)
	ctx := context.Background()

	// Insert a listing without extraction.
	l := testListing()
	l.EbayID = "unextracted-1"
	require.NoError(t, s.UpsertListing(ctx, l))

	listings, err := s.ListUnextractedListings(ctx, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, listings)

	// Now extract it.
	err = s.UpdateListingExtraction(ctx, l.ID, "ram", map[string]any{}, 0.9, "ram:test")
	require.NoError(t, err)

	listings, err = s.ListUnextractedListings(ctx, 10)
	require.NoError(t, err)

	for _, listing := range listings {
		assert.NotEqual(t, l.ID, listing.ID, "extracted listing should not appear")
	}
}

func TestPostgresStore_ListUnscoredListings(t *testing.T) {
	s := setupPostgres(t)
	ctx := context.Background()

	// Insert and extract a listing.
	l := testListing()
	l.EbayID = "unscored-1"
	require.NoError(t, s.UpsertListing(ctx, l))
	require.NoError(
		t,
		s.UpdateListingExtraction(ctx, l.ID, "ram", map[string]any{}, 0.9, "ram:test"),
	)

	listings, err := s.ListUnscoredListings(ctx, 10)
	require.NoError(t, err)
	assert.NotEmpty(t, listings)

	// Score it.
	require.NoError(t, s.UpdateScore(ctx, l.ID, 75, json.RawMessage(`{}`)))

	listings, err = s.ListUnscoredListings(ctx, 10)
	require.NoError(t, err)

	for _, listing := range listings {
		assert.NotEqual(t, l.ID, listing.ID, "scored listing should not appear")
	}
}

func TestPostgresStore_WatchCRUD(t *testing.T) {
	s := setupPostgres(t)
	ctx := context.Background()

	// Create.
	w := &domain.Watch{
		Name:           "DDR4 ECC REG 32GB",
		SearchQuery:    "DDR4 ECC REG 32GB server memory",
		ComponentType:  domain.ComponentRAM,
		Filters:        domain.WatchFilters{},
		ScoreThreshold: 75,
		Enabled:        true,
	}
	err := s.CreateWatch(ctx, w)
	require.NoError(t, err)
	assert.NotEmpty(t, w.ID)

	// Get.
	got, err := s.GetWatch(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, "DDR4 ECC REG 32GB", got.Name)
	assert.Equal(t, domain.ComponentRAM, got.ComponentType)
	assert.True(t, got.Enabled)

	// Update.
	got.ScoreThreshold = 80
	got.Name = "DDR4 ECC 32GB (updated)"
	err = s.UpdateWatch(ctx, got)
	require.NoError(t, err)

	updated, err := s.GetWatch(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, 80, updated.ScoreThreshold)
	assert.Equal(t, "DDR4 ECC 32GB (updated)", updated.Name)

	// List all.
	watches, err := s.ListWatches(ctx, false)
	require.NoError(t, err)
	assert.Len(t, watches, 1)

	// Disable.
	err = s.SetWatchEnabled(ctx, w.ID, false)
	require.NoError(t, err)

	// List enabled only.
	watches, err = s.ListWatches(ctx, true)
	require.NoError(t, err)
	assert.Empty(t, watches)

	// Delete.
	err = s.DeleteWatch(ctx, w.ID)
	require.NoError(t, err)

	_, err = s.GetWatch(ctx, w.ID)
	assert.Error(t, err)
}

func TestPostgresStore_AlertLifecycle(t *testing.T) {
	s := setupPostgres(t)
	ctx := context.Background()

	// Set up watch and listing.
	w := &domain.Watch{
		Name:           "Test Watch",
		SearchQuery:    "test",
		ComponentType:  domain.ComponentRAM,
		Filters:        domain.WatchFilters{},
		ScoreThreshold: 50,
		Enabled:        true,
	}
	require.NoError(t, s.CreateWatch(ctx, w))

	l := testListing()
	l.EbayID = "alert-test-1"
	require.NoError(t, s.UpsertListing(ctx, l))

	// Create alert.
	a := &domain.Alert{
		WatchID:   w.ID,
		ListingID: l.ID,
		Score:     85,
	}
	err := s.CreateAlert(ctx, a)
	require.NoError(t, err)
	assert.NotEmpty(t, a.ID)

	// Duplicate alert should succeed silently (ON CONFLICT DO NOTHING).
	dup := &domain.Alert{
		WatchID:   w.ID,
		ListingID: l.ID,
		Score:     85,
	}
	err = s.CreateAlert(ctx, dup)
	require.NoError(t, err)

	// List pending.
	pending, err := s.ListPendingAlerts(ctx)
	require.NoError(t, err)
	assert.Len(t, pending, 1)

	// List by watch.
	byWatch, err := s.ListAlertsByWatch(ctx, w.ID, 10)
	require.NoError(t, err)
	assert.Len(t, byWatch, 1)

	// Mark notified.
	err = s.MarkAlertNotified(ctx, a.ID)
	require.NoError(t, err)

	pending, err = s.ListPendingAlerts(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestPostgresStore_MarkAlertsNotified(t *testing.T) {
	s := setupPostgres(t)
	ctx := context.Background()

	w := &domain.Watch{
		Name:           "Batch Test",
		SearchQuery:    "batch",
		ComponentType:  domain.ComponentDrive,
		Filters:        domain.WatchFilters{},
		ScoreThreshold: 50,
		Enabled:        true,
	}
	require.NoError(t, s.CreateWatch(ctx, w))

	var alertIDs []string
	for i := range 3 {
		l := testListing()
		l.EbayID = "batch-alert-" + string(rune('a'+i))
		require.NoError(t, s.UpsertListing(ctx, l))

		a := &domain.Alert{WatchID: w.ID, ListingID: l.ID, Score: 70 + i}
		require.NoError(t, s.CreateAlert(ctx, a))
		alertIDs = append(alertIDs, a.ID)
	}

	pending, err := s.ListPendingAlerts(ctx)
	require.NoError(t, err)
	assert.Len(t, pending, 3)

	err = s.MarkAlertsNotified(ctx, alertIDs)
	require.NoError(t, err)

	pending, err = s.ListPendingAlerts(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending)
}
