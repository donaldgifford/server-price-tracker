package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	ebayMocks "github.com/donaldgifford/server-price-tracker/internal/ebay/mocks"
	notifyMocks "github.com/donaldgifford/server-price-tracker/internal/notify/mocks"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	extractMocks "github.com/donaldgifford/server-price-tracker/pkg/extract/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// quietLogger returns a logger that discards output for tests.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestEngine(
	s *storeMocks.MockStore,
	e *ebayMocks.MockEbayClient,
	ex *extractMocks.MockExtractor,
	n *notifyMocks.MockNotifier,
) *Engine {
	return NewEngine(s, e, ex, n,
		WithLogger(quietLogger()),
		WithStaggerOffset(0),
	)
}

func TestNewEngine_Defaults(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	eng := NewEngine(ms, me, mx, mn)
	assert.Equal(t, 90, eng.baselineWindowDays)
	assert.Equal(t, 30*time.Second, eng.staggerOffset)
	assert.NotNil(t, eng.log)
}

func TestNewEngine_WithOptions(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	l := quietLogger()
	eng := NewEngine(ms, me, mx, mn,
		WithLogger(l),
		WithBaselineWindowDays(30),
		WithStaggerOffset(5*time.Second),
	)

	assert.Equal(t, 30, eng.baselineWindowDays)
	assert.Equal(t, 5*time.Second, eng.staggerOffset)
	assert.Same(t, l, eng.log)
}

func TestRunIngestion_TwoWatchesThreeListingsEach(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	watches := []domain.Watch{
		{
			ID: "w1", Name: "Watch 1", SearchQuery: "DDR4 ECC",
			ScoreThreshold: 80, Enabled: true,
		},
		{
			ID: "w2", Name: "Watch 2", SearchQuery: "SSD Enterprise",
			ScoreThreshold: 80, Enabled: true,
		},
	}

	ms.EXPECT().ListWatches(mock.Anything, true).Return(watches, nil).Once()

	// Each watch returns 3 items.
	for _, w := range watches {
		items := make([]ebay.ItemSummary, 3)
		for j := range items {
			items[j] = ebay.ItemSummary{
				ItemID:    w.ID + "-item-" + string(rune('1'+j)),
				Title:     w.SearchQuery + " Listing",
				Price:     ebay.ItemPrice{Value: "49.99", Currency: "USD"},
				Condition: "Used",
			}
		}
		me.EXPECT().
			Search(mock.Anything, ebay.SearchRequest{
				Query:      w.SearchQuery,
				CategoryID: w.CategoryID,
			}).
			Return(&ebay.SearchResponse{Items: items}, nil).
			Once()
	}

	// 6 upserts, 6 extractions, 6 extraction updates, 6 score lookups.
	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Times(6)

	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, mock.Anything, mock.Anything).
		Return(domain.ComponentRAM, map[string]any{"capacity_gb": 32.0}, nil).
		Times(6)

	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, mock.Anything, "ram", mock.Anything, 0.9, mock.Anything).
		Return(nil).
		Times(6)

	// Scoring: GetBaseline + UpdateScore for each.
	ms.EXPECT().
		GetBaseline(mock.Anything, mock.Anything).
		Return(nil, pgx.ErrNoRows).
		Times(6)
	ms.EXPECT().
		UpdateScore(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).
		Times(6)

	// No alerts (score won't meet threshold with cold start).
	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

func TestRunIngestion_ExtractionFailsForOneListing(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	watches := []domain.Watch{
		{ID: "w1", Name: "Watch 1", SearchQuery: "DDR4", ScoreThreshold: 80, Enabled: true},
	}
	ms.EXPECT().ListWatches(mock.Anything, true).Return(watches, nil).Once()

	items := []ebay.ItemSummary{
		{
			ItemID: "good-1",
			Title:  "Good Listing 1",
			Price:  ebay.ItemPrice{Value: "30.00", Currency: "USD"},
		},
		{
			ItemID: "bad-1",
			Title:  "Bad Listing",
			Price:  ebay.ItemPrice{Value: "40.00", Currency: "USD"},
		},
		{
			ItemID: "good-2",
			Title:  "Good Listing 2",
			Price:  ebay.ItemPrice{Value: "50.00", Currency: "USD"},
		},
	}
	me.EXPECT().
		Search(mock.Anything, mock.Anything).
		Return(&ebay.SearchResponse{Items: items}, nil).
		Once()

	// All 3 upsert successfully.
	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Times(3)

	// Extraction: first succeeds, second fails, third succeeds.
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, "Good Listing 1", mock.Anything).
		Return(domain.ComponentRAM, map[string]any{"capacity_gb": 32.0}, nil).
		Once()
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, "Bad Listing", mock.Anything).
		Return(domain.ComponentType(""), nil, errors.New("LLM timeout")).
		Once()
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, "Good Listing 2", mock.Anything).
		Return(domain.ComponentRAM, map[string]any{"capacity_gb": 64.0}, nil).
		Once()

	// Only 2 extraction updates (bad listing skipped).
	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, mock.Anything, "ram", mock.Anything, 0.9, mock.Anything).
		Return(nil).
		Times(2)

	// 2 scorings.
	ms.EXPECT().GetBaseline(mock.Anything, mock.Anything).Return(nil, pgx.ErrNoRows).Times(2)
	ms.EXPECT().
		UpdateScore(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).
		Times(2)

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

func TestRunIngestion_EbayErrorForOneWatch(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	watches := []domain.Watch{
		{ID: "w1", Name: "Failing Watch", SearchQuery: "fail", ScoreThreshold: 80, Enabled: true},
		{ID: "w2", Name: "OK Watch", SearchQuery: "ok", ScoreThreshold: 80, Enabled: true},
	}
	ms.EXPECT().ListWatches(mock.Anything, true).Return(watches, nil).Once()

	// First watch: eBay error.
	me.EXPECT().
		Search(mock.Anything, ebay.SearchRequest{Query: "fail"}).
		Return(nil, errors.New("eBay 503")).
		Once()

	// Second watch: success with 1 item.
	me.EXPECT().
		Search(mock.Anything, ebay.SearchRequest{Query: "ok"}).
		Return(&ebay.SearchResponse{Items: []ebay.ItemSummary{
			{
				ItemID: "ok-1",
				Title:  "OK Item",
				Price:  ebay.ItemPrice{Value: "25.00", Currency: "USD"},
			},
		}}, nil).
		Once()

	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Once()
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, "OK Item", mock.Anything).
		Return(domain.ComponentRAM, map[string]any{}, nil).Once()
	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, mock.Anything, "ram", mock.Anything, 0.9, mock.Anything).
		Return(nil).Once()
	ms.EXPECT().GetBaseline(mock.Anything, mock.Anything).Return(nil, pgx.ErrNoRows).Once()
	ms.EXPECT().
		UpdateScore(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).
		Once()

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

func TestRunIngestion_ListWatchesError(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	ms.EXPECT().ListWatches(mock.Anything, true).Return(nil, errors.New("db error")).Once()

	err := eng.RunIngestion(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing watches")
}

func TestRunIngestion_ContextCancelled(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	watches := []domain.Watch{
		{ID: "w1", Name: "Watch 1", SearchQuery: "DDR4", Enabled: true},
	}
	ms.EXPECT().ListWatches(mock.Anything, true).Return(watches, nil).Once()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := eng.RunIngestion(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRunIngestion_NoWatches(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	ms.EXPECT().ListWatches(mock.Anything, true).Return(nil, nil).Once()
	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

func TestEvaluateAlert_ScoreAboveThreshold(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	score := 90
	listing := &domain.Listing{
		ID:    "l1",
		Score: &score,
	}
	watch := &domain.Watch{
		ID:             "w1",
		ScoreThreshold: 80,
	}

	ms.EXPECT().CreateAlert(mock.Anything, mock.MatchedBy(func(a *domain.Alert) bool {
		return a.WatchID == "w1" && a.ListingID == "l1" && a.Score == 90
	})).Return(nil).Once()

	eng.evaluateAlert(context.Background(), watch, listing)
}

func TestEvaluateAlert_ScoreBelowThreshold(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	score := 70
	listing := &domain.Listing{
		ID:    "l1",
		Score: &score,
	}
	watch := &domain.Watch{
		ID:             "w1",
		ScoreThreshold: 80,
	}

	// CreateAlert should NOT be called.
	eng.evaluateAlert(context.Background(), watch, listing)
}

func TestEvaluateAlert_NilScore(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	listing := &domain.Listing{
		ID:    "l1",
		Score: nil,
	}
	watch := &domain.Watch{
		ID:             "w1",
		ScoreThreshold: 80,
	}

	eng.evaluateAlert(context.Background(), watch, listing)
}

func TestEvaluateAlert_FiltersDontMatch(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	score := 90
	maxPrice := 30.0
	listing := &domain.Listing{
		ID:       "l1",
		Score:    &score,
		Price:    50.0,
		Quantity: 1,
	}
	watch := &domain.Watch{
		ID:             "w1",
		ScoreThreshold: 80,
		Filters:        domain.WatchFilters{PriceMax: &maxPrice},
	}

	// Score passes but filters don't → no alert.
	eng.evaluateAlert(context.Background(), watch, listing)
}

func TestEvaluateAlert_CreateAlertError(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	score := 90
	listing := &domain.Listing{
		ID:    "l1",
		Score: &score,
	}
	watch := &domain.Watch{
		ID:             "w1",
		ScoreThreshold: 80,
	}

	ms.EXPECT().
		CreateAlert(mock.Anything, mock.Anything).
		Return(errors.New("unique constraint")).
		Once()

	// Should not panic; error is logged.
	eng.evaluateAlert(context.Background(), watch, listing)
}

func TestRunBaselineRefresh_Success(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	ms.EXPECT().
		RecomputeAllBaselines(mock.Anything, 90).
		Return(nil).
		Once()

	// RescoreAll calls ListListings.
	ms.EXPECT().
		ListListings(mock.Anything, mock.Anything).
		Return(nil, 0, nil).
		Once()

	err := eng.RunBaselineRefresh(context.Background())
	require.NoError(t, err)
}

func TestRunBaselineRefresh_RecomputeError(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	ms.EXPECT().
		RecomputeAllBaselines(mock.Anything, 90).
		Return(errors.New("db error")).
		Once()

	err := eng.RunBaselineRefresh(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recomputing baselines")
}

func TestRunBaselineRefresh_RescoreError(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	ms.EXPECT().
		RecomputeAllBaselines(mock.Anything, 90).
		Return(nil).
		Once()

	ms.EXPECT().
		ListListings(mock.Anything, mock.Anything).
		Return(nil, 0, errors.New("db error")).
		Once()

	err := eng.RunBaselineRefresh(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "re-scoring")
}

func TestConvertItemSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		item    ebay.ItemSummary
		want    *domain.Listing
		checkFn func(*testing.T, *domain.Listing)
	}{
		{
			name: "full item with all fields",
			item: ebay.ItemSummary{
				ItemID:     "ebay-123",
				Title:      "Samsung 32GB DDR4 ECC",
				Price:      ebay.ItemPrice{Value: "45.99", Currency: "USD"},
				ItemWebURL: "https://ebay.com/itm/123",
				Image:      &ebay.ItemImage{ImageURL: "https://img.com/1.jpg"},
				Seller: &ebay.ItemSeller{
					Username:           "seller1",
					FeedbackScore:      5432,
					FeedbackPercentage: "99.8",
				},
				Condition:                "Used",
				TopRatedBuyingExperience: true,
				ShippingOptions: []ebay.ShippingOption{
					{ShippingCost: &ebay.ItemPrice{Value: "5.99", Currency: "USD"}},
				},
			},
			checkFn: func(t *testing.T, l *domain.Listing) {
				t.Helper()
				assert.Equal(t, "ebay-123", l.EbayID)
				assert.Equal(t, "Samsung 32GB DDR4 ECC", l.Title)
				assert.Equal(t, 45.99, l.Price)
				assert.Equal(t, "USD", l.Currency)
				assert.Equal(t, "https://ebay.com/itm/123", l.ItemURL)
				assert.Equal(t, "https://img.com/1.jpg", l.ImageURL)
				assert.Equal(t, "seller1", l.SellerName)
				assert.Equal(t, 5432, l.SellerFeedback)
				assert.InDelta(t, 99.8, l.SellerFeedbackPct, 0.01)
				assert.True(t, l.SellerTopRated)
				assert.Equal(t, "Used", l.ConditionRaw)
				assert.Equal(t, 1, l.Quantity)
				require.NotNil(t, l.ShippingCost)
				assert.InDelta(t, 5.99, *l.ShippingCost, 0.01)
			},
		},
		{
			name: "item without image",
			item: ebay.ItemSummary{
				ItemID: "ebay-456",
				Title:  "No Image Item",
				Price:  ebay.ItemPrice{Value: "20.00", Currency: "USD"},
			},
			checkFn: func(t *testing.T, l *domain.Listing) {
				t.Helper()
				assert.Equal(t, "ebay-456", l.EbayID)
				assert.Empty(t, l.ImageURL)
			},
		},
		{
			name: "item without seller",
			item: ebay.ItemSummary{
				ItemID: "ebay-789",
				Title:  "No Seller Item",
				Price:  ebay.ItemPrice{Value: "10.00", Currency: "USD"},
			},
			checkFn: func(t *testing.T, l *domain.Listing) {
				t.Helper()
				assert.Empty(t, l.SellerName)
				assert.Zero(t, l.SellerFeedback)
				assert.Zero(t, l.SellerFeedbackPct)
			},
		},
		{
			name: "item without shipping",
			item: ebay.ItemSummary{
				ItemID: "ebay-noshipping",
				Title:  "No Shipping Item",
				Price:  ebay.ItemPrice{Value: "15.00", Currency: "USD"},
			},
			checkFn: func(t *testing.T, l *domain.Listing) {
				t.Helper()
				assert.Nil(t, l.ShippingCost)
			},
		},
		{
			name: "item with invalid price",
			item: ebay.ItemSummary{
				ItemID: "ebay-badprice",
				Title:  "Bad Price Item",
				Price:  ebay.ItemPrice{Value: "not-a-number", Currency: "USD"},
			},
			checkFn: func(t *testing.T, l *domain.Listing) {
				t.Helper()
				assert.Zero(t, l.Price)
			},
		},
		{
			name: "item with invalid feedback percentage",
			item: ebay.ItemSummary{
				ItemID: "ebay-badfb",
				Title:  "Bad Feedback",
				Price:  ebay.ItemPrice{Value: "10.00", Currency: "USD"},
				Seller: &ebay.ItemSeller{
					Username:           "seller2",
					FeedbackScore:      100,
					FeedbackPercentage: "invalid",
				},
			},
			checkFn: func(t *testing.T, l *domain.Listing) {
				t.Helper()
				assert.Equal(t, "seller2", l.SellerName)
				assert.Equal(t, 100, l.SellerFeedback)
				assert.Zero(t, l.SellerFeedbackPct)
			},
		},
		{
			name: "item with invalid shipping cost",
			item: ebay.ItemSummary{
				ItemID: "ebay-badship",
				Title:  "Bad Shipping",
				Price:  ebay.ItemPrice{Value: "10.00", Currency: "USD"},
				ShippingOptions: []ebay.ShippingOption{
					{ShippingCost: &ebay.ItemPrice{Value: "free", Currency: "USD"}},
				},
			},
			checkFn: func(t *testing.T, l *domain.Listing) {
				t.Helper()
				assert.Nil(t, l.ShippingCost)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			results := ebay.ToListings([]ebay.ItemSummary{tt.item})
			require.Len(t, results, 1)
			tt.checkFn(t, &results[0])
		})
	}
}

func TestRunIngestion_UpsertFailsContinues(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	watches := []domain.Watch{
		{ID: "w1", Name: "W", SearchQuery: "test", Enabled: true},
	}
	ms.EXPECT().ListWatches(mock.Anything, true).Return(watches, nil).Once()

	items := []ebay.ItemSummary{
		{ItemID: "fail", Title: "Fail Item", Price: ebay.ItemPrice{Value: "10", Currency: "USD"}},
		{ItemID: "pass", Title: "Pass Item", Price: ebay.ItemPrice{Value: "20", Currency: "USD"}},
	}
	me.EXPECT().
		Search(mock.Anything, mock.Anything).
		Return(&ebay.SearchResponse{Items: items}, nil).
		Once()

	// First upsert fails, second succeeds.
	ms.EXPECT().UpsertListing(mock.Anything, mock.MatchedBy(func(l *domain.Listing) bool {
		return l.EbayID == "fail"
	})).Return(errors.New("db error")).Once()

	ms.EXPECT().UpsertListing(mock.Anything, mock.MatchedBy(func(l *domain.Listing) bool {
		return l.EbayID == "pass"
	})).Return(nil).Once()

	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, "Pass Item", mock.Anything).
		Return(domain.ComponentRAM, map[string]any{}, nil).Once()
	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, mock.Anything, "ram", mock.Anything, 0.9, mock.Anything).
		Return(nil).Once()
	ms.EXPECT().GetBaseline(mock.Anything, mock.Anything).Return(nil, pgx.ErrNoRows).Once()
	ms.EXPECT().
		UpdateScore(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).
		Once()

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

func TestRunIngestion_UpdateExtractionFails(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	watches := []domain.Watch{
		{ID: "w1", Name: "W", SearchQuery: "test", Enabled: true},
	}
	ms.EXPECT().ListWatches(mock.Anything, true).Return(watches, nil).Once()

	items := []ebay.ItemSummary{
		{ItemID: "item1", Title: "Item 1", Price: ebay.ItemPrice{Value: "10", Currency: "USD"}},
	}
	me.EXPECT().
		Search(mock.Anything, mock.Anything).
		Return(&ebay.SearchResponse{Items: items}, nil).
		Once()

	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Once()
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, "Item 1", mock.Anything).
		Return(domain.ComponentRAM, map[string]any{}, nil).Once()
	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, mock.Anything, "ram", mock.Anything, 0.9, mock.Anything).
		Return(errors.New("db error")).Once()

	// Scoring and alert eval should NOT be called.
	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

func TestRunIngestion_ScoringFails(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	watches := []domain.Watch{
		{ID: "w1", Name: "W", SearchQuery: "test", Enabled: true},
	}
	ms.EXPECT().ListWatches(mock.Anything, true).Return(watches, nil).Once()

	items := []ebay.ItemSummary{
		{ItemID: "item1", Title: "Item 1", Price: ebay.ItemPrice{Value: "10", Currency: "USD"}},
	}
	me.EXPECT().
		Search(mock.Anything, mock.Anything).
		Return(&ebay.SearchResponse{Items: items}, nil).
		Once()

	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Once()
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, "Item 1", mock.Anything).
		Return(domain.ComponentRAM, map[string]any{}, nil).Once()
	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, mock.Anything, "ram", mock.Anything, 0.9, mock.Anything).
		Return(nil).Once()

	// GetBaseline succeeds but UpdateScore fails.
	ms.EXPECT().GetBaseline(mock.Anything, mock.Anything).Return(nil, pgx.ErrNoRows).Once()
	ms.EXPECT().
		UpdateScore(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(errors.New("score db error")).Once()

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}
