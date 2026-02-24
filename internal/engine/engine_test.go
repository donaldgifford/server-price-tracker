package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	ptestutil "github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	ebayMocks "github.com/donaldgifford/server-price-tracker/internal/ebay/mocks"
	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	notifyMocks "github.com/donaldgifford/server-price-tracker/internal/notify/mocks"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	extractMocks "github.com/donaldgifford/server-price-tracker/pkg/extract/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// quietLogger returns a logger that discards output for tests.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// expectCountMethods sets up Maybe expectations for the GetSystemState call and
// UpdateWatchLastPolled so that tests exercising RunIngestion (which calls
// SyncStateMetrics) don't fail on unexpected calls.
func expectCountMethods(ms *storeMocks.MockStore) {
	ms.EXPECT().GetSystemState(mock.Anything).Return(&domain.SystemState{}, nil).Maybe()
	ms.EXPECT().UpdateWatchLastPolled(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
}

func newTestEngine(
	s *storeMocks.MockStore,
	e *ebayMocks.MockEbayClient,
	ex *extractMocks.MockExtractor,
	n *notifyMocks.MockNotifier,
) *Engine {
	expectCountMethods(s)
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
	assert.Equal(t, defaultMaxCallsPerCycle, eng.maxCallsPerCycle)
	assert.NotNil(t, eng.log)
}

func TestNewEngine_WithOptions(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	l := quietLogger()
	p := ebay.NewPaginator(me, ms)
	eng := NewEngine(ms, me, mx, mn,
		WithLogger(l),
		WithBaselineWindowDays(30),
		WithStaggerOffset(5*time.Second),
		WithPaginator(p),
		WithMaxCallsPerCycle(10),
	)

	assert.Equal(t, 30, eng.baselineWindowDays)
	assert.Equal(t, 5*time.Second, eng.staggerOffset)
	assert.Equal(t, 10, eng.maxCallsPerCycle)
	assert.Same(t, l, eng.log)
	assert.Same(t, p, eng.paginator)
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

	// 6 upserts + 6 enqueues.
	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Times(6)
	ms.EXPECT().EnqueueExtraction(mock.Anything, mock.Anything, 0).Return(nil).Times(6)

	// No alerts (listings have no score yet).
	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

func TestRunIngestion_EnqueuesAllListings(t *testing.T) {
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
			ItemID: "item-1",
			Title:  "Listing 1",
			Price:  ebay.ItemPrice{Value: "30.00", Currency: "USD"},
		},
		{
			ItemID: "item-2",
			Title:  "Listing 2",
			Price:  ebay.ItemPrice{Value: "40.00", Currency: "USD"},
		},
		{
			ItemID: "item-3",
			Title:  "Listing 3",
			Price:  ebay.ItemPrice{Value: "50.00", Currency: "USD"},
		},
	}
	me.EXPECT().
		Search(mock.Anything, mock.Anything).
		Return(&ebay.SearchResponse{Items: items}, nil).
		Once()

	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Times(3)
	ms.EXPECT().EnqueueExtraction(mock.Anything, mock.Anything, 0).Return(nil).Times(3)
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
	ms.EXPECT().EnqueueExtraction(mock.Anything, mock.Anything, 0).Return(nil).Once()

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

	// RescoreAll calls ListListingsCursor; empty first page terminates loop.
	ms.EXPECT().
		ListListingsCursor(mock.Anything, "", 200).
		Return(nil, nil).
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
		ListListingsCursor(mock.Anything, "", 200).
		Return(nil, errors.New("db error")).
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

	ms.EXPECT().EnqueueExtraction(mock.Anything, mock.Anything, 0).Return(nil).Once()

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

func TestRunIngestion_DailyLimitHit(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	watches := []domain.Watch{
		{ID: "w1", Name: "Watch 1", SearchQuery: "DDR4", ScoreThreshold: 80, Enabled: true},
		{ID: "w2", Name: "Watch 2", SearchQuery: "SSD", ScoreThreshold: 80, Enabled: true},
	}
	ms.EXPECT().ListWatches(mock.Anything, true).Return(watches, nil).Once()

	// First watch: Search returns ErrDailyLimitReached.
	me.EXPECT().
		Search(mock.Anything, ebay.SearchRequest{Query: "DDR4"}).
		Return(nil, fmt.Errorf("rate limit: %w", ebay.ErrDailyLimitReached)).
		Once()

	// Second watch should NOT be processed.
	// Alert processing should still run.
	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

func TestRunIngestion_CycleBudgetExhausted(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	expectCountMethods(ms)
	eng := NewEngine(ms, me, mx, mn,
		WithLogger(quietLogger()),
		WithStaggerOffset(0),
		WithMaxCallsPerCycle(1),
	)

	watches := []domain.Watch{
		{ID: "w1", Name: "Watch 1", SearchQuery: "DDR4", ScoreThreshold: 80, Enabled: true},
		{ID: "w2", Name: "Watch 2", SearchQuery: "SSD", ScoreThreshold: 80, Enabled: true},
	}
	ms.EXPECT().ListWatches(mock.Anything, true).Return(watches, nil).Once()

	// Only first watch should be processed (maxCallsPerCycle=1).
	me.EXPECT().
		Search(mock.Anything, ebay.SearchRequest{Query: "DDR4"}).
		Return(&ebay.SearchResponse{Items: []ebay.ItemSummary{
			{ItemID: "item1", Title: "Item 1", Price: ebay.ItemPrice{Value: "10", Currency: "USD"}},
		}}, nil).
		Once()

	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Once()
	ms.EXPECT().EnqueueExtraction(mock.Anything, mock.Anything, 0).Return(nil).Once()

	// Alert processing should still run.
	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

func TestRunIngestion_WithPaginator(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	expectCountMethods(ms)

	paginator := ebay.NewPaginator(me, ms,
		ebay.WithPaginatorLogger(quietLogger()),
		ebay.WithMaxPages(1),
	)

	eng := NewEngine(ms, me, mx, mn,
		WithLogger(quietLogger()),
		WithStaggerOffset(0),
		WithPaginator(paginator),
	)

	watches := []domain.Watch{
		{ID: "w1", Name: "Watch 1", SearchQuery: "DDR4", ScoreThreshold: 80, Enabled: true},
	}
	ms.EXPECT().ListWatches(mock.Anything, true).Return(watches, nil).Once()

	// Paginator calls Search with pageSize=200.
	items := []ebay.ItemSummary{
		{ItemID: "item1", Title: "DDR4 32GB", Price: ebay.ItemPrice{Value: "45", Currency: "USD"}},
		{ItemID: "item2", Title: "DDR4 16GB", Price: ebay.ItemPrice{Value: "25", Currency: "USD"}},
	}
	me.EXPECT().
		Search(mock.Anything, mock.MatchedBy(func(req ebay.SearchRequest) bool {
			return req.Query == "DDR4" && req.Limit == 200
		})).
		Return(&ebay.SearchResponse{Items: items, HasMore: false}, nil).
		Once()

	// Paginator calls GetListing for dedup check on each item.
	ms.EXPECT().GetListing(mock.Anything, "item1").Return(nil, nil).Once()
	ms.EXPECT().GetListing(mock.Anything, "item2").Return(nil, nil).Once()

	// Standard ingestion flow for 2 new listings.
	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Times(2)
	ms.EXPECT().EnqueueExtraction(mock.Anything, mock.Anything, 0).Return(nil).Times(2)

	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

// analyticsResponse is a valid eBay Analytics API response for testing.
const analyticsResponse = `{
	"rateLimits": [{
		"apiContext": "buy",
		"apiName": "Browse",
		"apiVersion": "v1",
		"resources": [{
			"name": "buy.browse",
			"rates": [{
				"count": 200,
				"limit": 5000,
				"remaining": 4800,
				"reset": "2026-02-17T08:00:00.000Z",
				"timeWindow": 86400
			}]
		}]
	}]
}`

func getHistogramSampleCount(h prometheus.Histogram) uint64 {
	ch := make(chan prometheus.Metric, 1)
	h.Collect(ch)
	m := <-ch
	pb := &dto.Metric{}
	_ = m.Write(pb)
	return pb.GetHistogram().GetSampleCount()
}

func TestRunIngestion_DoesNotExtractInline(t *testing.T) {
	// Not parallel: checks global ExtractionDuration histogram count.
	// Worker tests running in parallel would increment the same counter.

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
		{ItemID: "inline-1", Title: "Test RAM", Price: ebay.ItemPrice{Value: "30.00", Currency: "USD"}},
	}
	me.EXPECT().Search(mock.Anything, mock.Anything).
		Return(&ebay.SearchResponse{Items: items}, nil).Once()
	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Once()
	ms.EXPECT().EnqueueExtraction(mock.Anything, mock.Anything, 0).Return(nil).Once()
	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	before := getHistogramSampleCount(metrics.ExtractionDuration)

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)

	after := getHistogramSampleCount(metrics.ExtractionDuration)
	assert.Equal(t, before, after, "RunIngestion must not extract inline — extraction is async via worker pool")
}

func TestSyncQuota_SetsMetricsAndSyncsRateLimiter(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, analyticsResponse)
	}))
	defer srv.Close()

	tp := ebayMocks.NewMockTokenProvider(t)
	tp.EXPECT().Token(mock.Anything).Return("test-token", nil)

	ac := ebay.NewAnalyticsClient(tp,
		ebay.WithAnalyticsURL(srv.URL),
	)
	rl := ebay.NewRateLimiter(5, 10, 5000)

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	eng := NewEngine(ms, me, mx, mn,
		WithLogger(quietLogger()),
		WithAnalyticsClient(ac),
		WithRateLimiter(rl),
	)

	ms.EXPECT().
		PersistRateLimiterState(mock.Anything, 200, 5000, mock.AnythingOfType("time.Time")).
		Return(nil).
		Once()

	eng.SyncQuota(context.Background())

	// Verify Prometheus metrics were set.
	assert.InDelta(t, 5000, ptestutil.ToFloat64(metrics.EbayRateLimit), 0.1)
	assert.InDelta(t, 4800, ptestutil.ToFloat64(metrics.EbayRateRemaining), 0.1)

	expectedReset := time.Date(2026, 2, 17, 8, 0, 0, 0, time.UTC)
	assert.InDelta(t, float64(expectedReset.Unix()),
		ptestutil.ToFloat64(metrics.EbayRateResetTimestamp), 0.1)

	// Verify rate limiter was synced.
	assert.Equal(t, int64(200), rl.DailyCount())
	assert.Equal(t, int64(5000), rl.MaxDaily())
	assert.Equal(t, int64(4800), rl.Remaining())
	assert.Equal(t, expectedReset, rl.ResetAt())
}

func TestSyncQuota_AnalyticsFailureDoesNotPanic(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error": "server error"}`)
	}))
	defer srv.Close()

	tp := ebayMocks.NewMockTokenProvider(t)
	tp.EXPECT().Token(mock.Anything).Return("test-token", nil)

	ac := ebay.NewAnalyticsClient(tp,
		ebay.WithAnalyticsURL(srv.URL),
	)
	rl := ebay.NewRateLimiter(5, 10, 5000)

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	eng := NewEngine(ms, me, mx, mn,
		WithLogger(quietLogger()),
		WithAnalyticsClient(ac),
		WithRateLimiter(rl),
	)

	// Should not panic; rate limiter should be unchanged.
	eng.SyncQuota(context.Background())

	assert.Equal(t, int64(0), rl.DailyCount())
	assert.Equal(t, int64(5000), rl.MaxDaily())
}

func TestSyncQuota_NilAnalyticsClientIsNoOp(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	eng := NewEngine(ms, me, mx, mn,
		WithLogger(quietLogger()),
	)

	// Should return immediately without error.
	eng.SyncQuota(context.Background())
}

func TestSyncStateMetrics_UsesGetSystemState(t *testing.T) {
	// Not parallel: uses global Prometheus gauges that can race with other tests.
	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	state := &domain.SystemState{
		WatchesTotal:   10,
		WatchesEnabled: 8,
		ListingsTotal:  500,
	}
	ms.EXPECT().GetSystemState(mock.Anything).Return(state, nil).Once()

	eng := NewEngine(ms, me, mx, mn,
		WithLogger(quietLogger()),
	)

	eng.SyncStateMetrics(context.Background())
}

func TestSyncStateMetrics_GetSystemStateError(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	ms.EXPECT().GetSystemState(mock.Anything).Return(nil, errors.New("db error")).Once()

	eng := NewEngine(ms, me, mx, mn,
		WithLogger(quietLogger()),
	)

	// Should not panic; errors are logged.
	eng.SyncStateMetrics(context.Background())
}

func TestRunReExtraction_Success(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	listings := []domain.Listing{
		{ID: "id1", EbayID: "e1", Title: "Samsung 32GB DDR4 PC4-21300", ProductKey: "ram:ddr4:ecc_reg:32gb:0"},
		{ID: "id2", EbayID: "e2", Title: "Hynix 16GB DDR4 PC4-19200", ProductKey: "ram:ddr4:ecc_reg:16gb:0"},
	}

	ms.EXPECT().
		ListIncompleteExtractions(mock.Anything, "ram", 50).
		Return(listings, nil).
		Once()

	// Enqueue with priority 1 (re-extract).
	ms.EXPECT().EnqueueExtraction(mock.Anything, "id1", 1).Return(nil).Once()
	ms.EXPECT().EnqueueExtraction(mock.Anything, "id2", 1).Return(nil).Once()

	count, err := eng.RunReExtraction(context.Background(), "ram", 50)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestRunReExtraction_PartialFailure(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	listings := []domain.Listing{
		{ID: "id1", EbayID: "e1", Title: "Listing 1"},
		{ID: "id2", EbayID: "e2", Title: "Listing 2"},
		{ID: "id3", EbayID: "e3", Title: "Listing 3"},
	}

	ms.EXPECT().
		ListIncompleteExtractions(mock.Anything, "", 100).
		Return(listings, nil).
		Once()

	// First enqueue succeeds, second fails, third succeeds.
	ms.EXPECT().EnqueueExtraction(mock.Anything, "id1", 1).Return(nil).Once()
	ms.EXPECT().
		EnqueueExtraction(mock.Anything, "id2", 1).
		Return(errors.New("db error")).Once()
	ms.EXPECT().EnqueueExtraction(mock.Anything, "id3", 1).Return(nil).Once()

	count, err := eng.RunReExtraction(context.Background(), "", 0)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestRunReExtraction_NoListings(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	ms.EXPECT().
		ListIncompleteExtractions(mock.Anything, "", 100).
		Return(nil, nil).
		Once()

	count, err := eng.RunReExtraction(context.Background(), "", 0)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRunIngestion_WritesLastPolledAt(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	watch := domain.Watch{
		ID: "w-poll", Name: "Poll Watch", SearchQuery: "test query", Enabled: true,
	}
	ms.EXPECT().ListWatches(mock.Anything, true).Return([]domain.Watch{watch}, nil).Once()
	me.EXPECT().Search(mock.Anything, mock.Anything).Return(&ebay.SearchResponse{}, nil).Once()
	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)

	// UpdateWatchLastPolled must have been called for the processed watch.
	ms.AssertNumberOfCalls(t, "UpdateWatchLastPolled", 1)
}

func TestRunReExtraction_DefaultLimit(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	ms.EXPECT().
		ListIncompleteExtractions(mock.Anything, "", 100).
		Return(nil, nil).
		Once()

	count, err := eng.RunReExtraction(context.Background(), "", 0)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRunIngestion_EnqueuesNotExtracts(t *testing.T) {
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

	me.EXPECT().Search(mock.Anything, mock.Anything).
		Return(&ebay.SearchResponse{Items: []ebay.ItemSummary{
			{ItemID: "i1", Title: "Test RAM", Price: ebay.ItemPrice{Value: "30.00", Currency: "USD"}},
		}}, nil).Once()

	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Once()
	// EnqueueExtraction must be called — ClassifyAndExtract must NOT be called.
	ms.EXPECT().EnqueueExtraction(mock.Anything, mock.Anything, 0).Return(nil).Once()
	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	// mx has NO expectations — ClassifyAndExtract should never be called inline.
	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)
}

func TestStartExtractionWorkers_ProcessesJob(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	job := domain.ExtractionJob{
		ID:        "job-1",
		ListingID: "listing-1",
	}
	listing := testListing("ram:ddr4:32gb")
	listing.ID = "listing-1"

	// First dequeue returns the job; subsequent calls return empty (worker idles).
	ms.EXPECT().
		DequeueExtractions(mock.Anything, mock.AnythingOfType("string"), 1).
		Return([]domain.ExtractionJob{job}, nil).Once()
	ms.EXPECT().
		DequeueExtractions(mock.Anything, mock.AnythingOfType("string"), 1).
		Return(nil, nil).Maybe()

	ms.EXPECT().
		GetListingByID(mock.Anything, "listing-1").
		Return(listing, nil).Once()

	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, listing.Title, mock.Anything).
		Return(domain.ComponentRAM, map[string]any{"speed_mhz": 2666}, nil).Once()

	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, "listing-1", "ram", mock.Anything, 0.9, mock.AnythingOfType("string")).
		Return(nil).Once()

	ms.EXPECT().
		GetBaseline(mock.Anything, mock.AnythingOfType("string")).
		Return(nil, pgx.ErrNoRows).Once()
	ms.EXPECT().
		UpdateScore(mock.Anything, "listing-1", mock.AnythingOfType("int"), mock.Anything).
		Return(nil).Once()

	done := make(chan struct{})
	ms.EXPECT().
		CompleteExtractionJob(mock.Anything, "job-1", "").
		Run(func(_ context.Context, _ string, _ string) {
			close(done)
		}).
		Return(nil).Once()

	ctx, cancel := context.WithCancel(context.Background())

	// Use newTestEngine but override workerCount directly.
	eng := newTestEngine(ms, me, mx, mn)
	eng.workerCount = 1
	eng.StartExtractionWorkers(ctx)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: worker did not process the job")
	}
	cancel()
}

func TestRunExtractionWorker_HandlesExtractError(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	job := domain.ExtractionJob{ID: "job-2", ListingID: "listing-2"}
	listing := &domain.Listing{ID: "listing-2", EbayID: "e2", Title: "Failing Listing"}

	ms.EXPECT().
		DequeueExtractions(mock.Anything, mock.AnythingOfType("string"), 1).
		Return([]domain.ExtractionJob{job}, nil).Once()
	ms.EXPECT().
		DequeueExtractions(mock.Anything, mock.AnythingOfType("string"), 1).
		Return(nil, nil).Maybe()

	ms.EXPECT().
		GetListingByID(mock.Anything, "listing-2").
		Return(listing, nil).Once()

	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, "Failing Listing", mock.Anything).
		Return(domain.ComponentType(""), nil, errors.New("LLM timeout")).Once()

	done := make(chan struct{})
	ms.EXPECT().
		CompleteExtractionJob(mock.Anything, "job-2", "LLM timeout").
		Run(func(_ context.Context, _ string, _ string) {
			close(done)
		}).
		Return(nil).Once()

	ctx, cancel := context.WithCancel(context.Background())

	eng := newTestEngine(ms, me, mx, mn)
	go eng.runExtractionWorker(ctx, "worker-0")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: worker did not complete the failed job")
	}
	cancel()
}
