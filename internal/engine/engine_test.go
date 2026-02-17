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

// expectCountMethods sets up Maybe expectations for all Store count methods
// so that tests exercising RunIngestion (which calls SyncStateMetrics) don't
// fail on unexpected calls. Returns zeroes for all counts.
func expectCountMethods(ms *storeMocks.MockStore) {
	ms.EXPECT().CountWatches(mock.Anything).Return(0, 0, nil).Maybe()
	ms.EXPECT().CountListings(mock.Anything).Return(0, nil).Maybe()
	ms.EXPECT().CountUnextractedListings(mock.Anything).Return(0, nil).Maybe()
	ms.EXPECT().CountUnscoredListings(mock.Anything).Return(0, nil).Maybe()
	ms.EXPECT().CountPendingAlerts(mock.Anything).Return(0, nil).Maybe()
	ms.EXPECT().CountBaselinesByMaturity(mock.Anything).Return(0, 0, nil).Maybe()
	ms.EXPECT().CountProductKeysWithoutBaseline(mock.Anything).Return(0, nil).Maybe()
	ms.EXPECT().CountIncompleteExtractions(mock.Anything).Return(0, nil).Maybe()
	ms.EXPECT().CountIncompleteExtractionsByType(mock.Anything).Return(nil, nil).Maybe()
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
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, "Item 1", mock.Anything).
		Return(domain.ComponentRAM, map[string]any{}, nil).Once()
	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, mock.Anything, "ram", mock.Anything, 0.9, mock.Anything).
		Return(nil).Once()
	ms.EXPECT().GetBaseline(mock.Anything, mock.Anything).Return(nil, pgx.ErrNoRows).Once()
	ms.EXPECT().
		UpdateScore(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()

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
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, mock.Anything, mock.Anything).
		Return(domain.ComponentRAM, map[string]any{"capacity_gb": 32.0}, nil).
		Times(2)
	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, mock.Anything, "ram", mock.Anything, 0.9, mock.Anything).
		Return(nil).Times(2)
	ms.EXPECT().GetBaseline(mock.Anything, mock.Anything).Return(nil, pgx.ErrNoRows).Times(2)
	ms.EXPECT().
		UpdateScore(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Times(2)

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

func TestRunIngestion_ObservesExtractionDuration(t *testing.T) {
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
		{ItemID: "ext-dur-1", Title: "Test RAM", Price: ebay.ItemPrice{Value: "30.00", Currency: "USD"}},
	}
	me.EXPECT().Search(mock.Anything, mock.Anything).
		Return(&ebay.SearchResponse{Items: items}, nil).Once()
	ms.EXPECT().UpsertListing(mock.Anything, mock.Anything).Return(nil).Once()

	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, "Test RAM", mock.Anything).
		Return(domain.ComponentRAM, map[string]any{"capacity_gb": 32.0}, nil).Once()
	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, mock.Anything, "ram", mock.Anything, 0.9, mock.Anything).
		Return(nil).Once()
	ms.EXPECT().GetBaseline(mock.Anything, mock.Anything).Return(nil, pgx.ErrNoRows).Once()
	ms.EXPECT().UpdateScore(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	ms.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	before := getHistogramSampleCount(metrics.ExtractionDuration)

	err := eng.RunIngestion(context.Background())
	require.NoError(t, err)

	after := getHistogramSampleCount(metrics.ExtractionDuration)
	assert.Greater(t, after, before, "ExtractionDuration histogram sample count should increase after extraction")
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

func TestSyncStateMetrics_SetsAllGauges(t *testing.T) {
	// Not parallel: uses global Prometheus gauges that can race with other tests.
	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	ms.EXPECT().CountWatches(mock.Anything).Return(5, 3, nil).Once()
	ms.EXPECT().CountListings(mock.Anything).Return(100, nil).Once()
	ms.EXPECT().CountUnextractedListings(mock.Anything).Return(10, nil).Once()
	ms.EXPECT().CountUnscoredListings(mock.Anything).Return(5, nil).Once()
	ms.EXPECT().CountPendingAlerts(mock.Anything).Return(2, nil).Once()
	ms.EXPECT().CountBaselinesByMaturity(mock.Anything).Return(3, 12, nil).Once()
	ms.EXPECT().CountProductKeysWithoutBaseline(mock.Anything).Return(7, nil).Once()

	eng := NewEngine(ms, me, mx, mn,
		WithLogger(quietLogger()),
	)

	eng.SyncStateMetrics(context.Background())

	assert.InDelta(t, 5, ptestutil.ToFloat64(metrics.WatchesTotal), 0.1)
	assert.InDelta(t, 3, ptestutil.ToFloat64(metrics.WatchesEnabled), 0.1)
	assert.InDelta(t, 100, ptestutil.ToFloat64(metrics.ListingsTotal), 0.1)
	assert.InDelta(t, 10, ptestutil.ToFloat64(metrics.ListingsUnextracted), 0.1)
	assert.InDelta(t, 5, ptestutil.ToFloat64(metrics.ListingsUnscored), 0.1)
	assert.InDelta(t, 2, ptestutil.ToFloat64(metrics.AlertsPending), 0.1)
	assert.InDelta(t, 3, ptestutil.ToFloat64(metrics.BaselinesCold), 0.1)
	assert.InDelta(t, 12, ptestutil.ToFloat64(metrics.BaselinesWarm), 0.1)
	assert.InDelta(t, 15, ptestutil.ToFloat64(metrics.BaselinesTotal), 0.1)
	assert.InDelta(t, 7, ptestutil.ToFloat64(metrics.ProductKeysNoBaseline), 0.1)
}

func TestSyncStateMetrics_StoreErrorDoesNotPanic(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)

	// All count methods return errors.
	ms.EXPECT().CountWatches(mock.Anything).Return(0, 0, errors.New("db error")).Once()
	ms.EXPECT().CountListings(mock.Anything).Return(0, errors.New("db error")).Once()
	ms.EXPECT().CountUnextractedListings(mock.Anything).Return(0, errors.New("db error")).Once()
	ms.EXPECT().CountUnscoredListings(mock.Anything).Return(0, errors.New("db error")).Once()
	ms.EXPECT().CountPendingAlerts(mock.Anything).Return(0, errors.New("db error")).Once()
	ms.EXPECT().CountBaselinesByMaturity(mock.Anything).Return(0, 0, errors.New("db error")).Once()
	ms.EXPECT().CountProductKeysWithoutBaseline(mock.Anything).Return(0, errors.New("db error")).Once()

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

	// ClassifyAndExtract for each listing.
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, listings[0].Title, mock.Anything).
		Return(domain.ComponentRAM, map[string]any{
			"generation": "DDR4", "capacity_gb": float64(32), "speed_mhz": 2666,
			"ecc": true, "registered": true, "condition": "used_working",
		}, nil).
		Once()
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, listings[1].Title, mock.Anything).
		Return(domain.ComponentRAM, map[string]any{
			"generation": "DDR4", "capacity_gb": float64(16), "speed_mhz": 2400,
			"ecc": true, "registered": true, "condition": "used_working",
		}, nil).
		Once()

	// UpdateListingExtraction for each.
	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, "id1", "ram", mock.Anything, 0.9, mock.Anything).
		Return(nil).
		Once()
	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, "id2", "ram", mock.Anything, 0.9, mock.Anything).
		Return(nil).
		Once()

	// ScoreListing: GetBaseline + UpdateScore for each.
	ms.EXPECT().GetBaseline(mock.Anything, mock.Anything).Return(nil, pgx.ErrNoRows).Times(2)
	ms.EXPECT().UpdateScore(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)

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
		{ID: "id1", EbayID: "e1", Title: "Samsung 32GB DDR4 PC4-21300"},
		{ID: "id2", EbayID: "e2", Title: "Bad listing"},
		{ID: "id3", EbayID: "e3", Title: "Hynix 16GB DDR4 PC4-19200"},
	}

	ms.EXPECT().
		ListIncompleteExtractions(mock.Anything, "", 100).
		Return(listings, nil).
		Once()

	// First succeeds, second fails, third succeeds.
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, listings[0].Title, mock.Anything).
		Return(domain.ComponentRAM, map[string]any{
			"generation": "DDR4", "capacity_gb": float64(32), "speed_mhz": 2666,
			"ecc": true, "registered": true, "condition": "used_working",
		}, nil).
		Once()
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, listings[1].Title, mock.Anything).
		Return("", nil, errors.New("extraction failed")).
		Once()
	mx.EXPECT().
		ClassifyAndExtract(mock.Anything, listings[2].Title, mock.Anything).
		Return(domain.ComponentRAM, map[string]any{
			"generation": "DDR4", "capacity_gb": float64(16), "speed_mhz": 2400,
			"ecc": true, "registered": true, "condition": "used_working",
		}, nil).
		Once()

	ms.EXPECT().
		UpdateListingExtraction(mock.Anything, mock.Anything, "ram", mock.Anything, 0.9, mock.Anything).
		Return(nil).
		Times(2)

	ms.EXPECT().GetBaseline(mock.Anything, mock.Anything).Return(nil, pgx.ErrNoRows).Times(2)
	ms.EXPECT().UpdateScore(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)

	count, err := eng.RunReExtraction(context.Background(), "", 0) // default limit
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
