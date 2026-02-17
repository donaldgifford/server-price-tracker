package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	ptestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func testListing(productKey string) *domain.Listing {
	return &domain.Listing{
		ID:                "listing-1",
		EbayID:            "ebay-123",
		Title:             "Samsung 32GB DDR4 ECC REG",
		Price:             45.99,
		Currency:          "USD",
		ListingType:       domain.ListingBuyItNow,
		SellerFeedback:    5432,
		SellerFeedbackPct: 99.8,
		SellerTopRated:    true,
		ConditionNorm:     domain.ConditionUsedWorking,
		Quantity:          1,
		ProductKey:        productKey,
		ImageURL:          "https://example.com/img.jpg",
	}
}

func TestScoreListing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		listing   *domain.Listing
		setupMock func(*storeMocks.MockStore)
		wantErr   bool
		errMsg    string
	}{
		{
			name:    "listing with baseline scores correctly",
			listing: testListing("ram:ddr4:ecc_reg:32gb:2666"),
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					GetBaseline(mock.Anything, "ram:ddr4:ecc_reg:32gb:2666").
					Return(&domain.PriceBaseline{
						ProductKey:  "ram:ddr4:ecc_reg:32gb:2666",
						SampleCount: 50,
						P10:         20.0,
						P25:         35.0,
						P50:         50.0,
						P75:         65.0,
						P90:         80.0,
						Mean:        48.0,
					}, nil).
					Once()
				m.EXPECT().
					UpdateScore(mock.Anything, "listing-1", mock.AnythingOfType("int"), mock.Anything).
					Return(nil).
					Once()
			},
		},
		{
			name:    "listing without baseline uses cold start",
			listing: testListing("ram:ddr4:ecc_reg:32gb:2666"),
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					GetBaseline(mock.Anything, "ram:ddr4:ecc_reg:32gb:2666").
					Return(nil, pgx.ErrNoRows).
					Once()
				m.EXPECT().
					UpdateScore(mock.Anything, "listing-1", mock.AnythingOfType("int"), mock.Anything).
					Return(nil).
					Once()
			},
		},
		{
			name:    "listing with empty product key skips scoring",
			listing: testListing(""),
			setupMock: func(_ *storeMocks.MockStore) {
				// No mock calls expected.
			},
		},
		{
			name:    "GetBaseline error propagates",
			listing: testListing("ram:ddr4:ecc_reg:32gb:2666"),
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					GetBaseline(mock.Anything, "ram:ddr4:ecc_reg:32gb:2666").
					Return(nil, errors.New("db connection lost")).
					Once()
			},
			wantErr: true,
			errMsg:  "getting baseline",
		},
		{
			name:    "UpdateScore error propagates",
			listing: testListing("ram:ddr4:ecc_reg:32gb:2666"),
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					GetBaseline(mock.Anything, "ram:ddr4:ecc_reg:32gb:2666").
					Return(nil, pgx.ErrNoRows).
					Once()
				m.EXPECT().
					UpdateScore(mock.Anything, "listing-1", mock.AnythingOfType("int"), mock.Anything).
					Return(errors.New("write failed")).
					Once()
			},
			wantErr: true,
			errMsg:  "write failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := storeMocks.NewMockStore(t)
			tt.setupMock(mockStore)

			err := ScoreListing(context.Background(), mockStore, tt.listing)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRescoreListings(t *testing.T) {
	t.Parallel()

	mockStore := storeMocks.NewMockStore(t)

	listings := []domain.Listing{
		*testListing("ram:ddr4:32gb"),
		*testListing("ram:ddr4:16gb"),
	}
	listings[0].ID = "l1"
	listings[1].ID = "l2"

	mockStore.EXPECT().
		ListUnscoredListings(mock.Anything, 100).
		Return(listings, nil).
		Once()

	// Both listings get baseline lookups and score updates.
	mockStore.EXPECT().
		GetBaseline(mock.Anything, "ram:ddr4:32gb").
		Return(nil, pgx.ErrNoRows).
		Once()
	mockStore.EXPECT().
		UpdateScore(mock.Anything, "l1", mock.AnythingOfType("int"), mock.Anything).
		Return(nil).
		Once()

	mockStore.EXPECT().
		GetBaseline(mock.Anything, "ram:ddr4:16gb").
		Return(nil, pgx.ErrNoRows).
		Once()
	mockStore.EXPECT().
		UpdateScore(mock.Anything, "l2", mock.AnythingOfType("int"), mock.Anything).
		Return(nil).
		Once()

	scored, err := RescoreListings(context.Background(), mockStore, 100)
	require.NoError(t, err)
	assert.Equal(t, 2, scored)
}

func TestRescoreByProductKey(t *testing.T) {
	t.Parallel()

	mockStore := storeMocks.NewMockStore(t)

	listings := []domain.Listing{
		*testListing("ram:ddr4:32gb"),
		*testListing("ram:ddr4:32gb"),
		*testListing("ram:ddr4:32gb"),
	}
	listings[0].ID = "l1"
	listings[1].ID = "l2"
	listings[2].ID = "l3"

	mockStore.EXPECT().
		ListListings(mock.Anything, mock.Anything).
		Return(listings, 3, nil).
		Once()

	for _, l := range listings {
		mockStore.EXPECT().
			GetBaseline(mock.Anything, "ram:ddr4:32gb").
			Return(nil, pgx.ErrNoRows).
			Once()
		mockStore.EXPECT().
			UpdateScore(mock.Anything, l.ID, mock.AnythingOfType("int"), mock.Anything).
			Return(nil).
			Once()
	}

	scored, err := RescoreByProductKey(context.Background(), mockStore, "ram:ddr4:32gb")
	require.NoError(t, err)
	assert.Equal(t, 3, scored)
}

func TestScoreAll_ContinuesOnError(t *testing.T) {
	t.Parallel()

	mockStore := storeMocks.NewMockStore(t)

	listings := []domain.Listing{
		*testListing("key-a"),
		*testListing("key-b"),
		*testListing("key-c"),
	}
	listings[0].ID = "l1"
	listings[1].ID = "l2"
	listings[2].ID = "l3"

	// l1 scores successfully.
	mockStore.EXPECT().
		GetBaseline(mock.Anything, "key-a").
		Return(nil, pgx.ErrNoRows).
		Once()
	mockStore.EXPECT().
		UpdateScore(mock.Anything, "l1", mock.AnythingOfType("int"), mock.Anything).
		Return(nil).
		Once()

	// l2 fails on GetBaseline.
	mockStore.EXPECT().
		GetBaseline(mock.Anything, "key-b").
		Return(nil, errors.New("transient error")).
		Once()

	// l3 scores successfully.
	mockStore.EXPECT().
		GetBaseline(mock.Anything, "key-c").
		Return(nil, pgx.ErrNoRows).
		Once()
	mockStore.EXPECT().
		UpdateScore(mock.Anything, "l3", mock.AnythingOfType("int"), mock.Anything).
		Return(nil).
		Once()

	scored, err := scoreAll(context.Background(), mockStore, listings)
	assert.Equal(t, 2, scored)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transient error")
}

func TestBuildListingData(t *testing.T) {
	t.Parallel()

	shipping := 5.99
	l := &domain.Listing{
		Price:             45.99,
		ShippingCost:      &shipping,
		Quantity:          2,
		SellerFeedback:    1000,
		SellerFeedbackPct: 99.5,
		SellerTopRated:    true,
		ConditionNorm:     domain.ConditionNew,
		ListingType:       domain.ListingAuction,
		ImageURL:          "https://example.com/img.jpg",
		Attributes:        map[string]any{"manufacturer": "Samsung"},
	}

	data := buildListingData(l)
	assert.InDelta(t, (45.99+5.99)/2, data.UnitPrice, 0.01)
	assert.Equal(t, 1000, data.SellerFeedback)
	assert.InDelta(t, 99.5, data.SellerFeedbackPct, 0.01)
	assert.True(t, data.SellerTopRated)
	assert.Equal(t, "new", data.Condition)
	assert.Equal(t, 2, data.Quantity)
	assert.True(t, data.HasImages)
	assert.True(t, data.HasItemSpecifics)
	assert.True(t, data.IsAuction)
}

func TestRescoreAll(t *testing.T) {
	t.Parallel()

	mockStore := storeMocks.NewMockStore(t)

	listings := []domain.Listing{
		*testListing("key-1"),
		*testListing("key-2"),
	}
	listings[0].ID = "l1"
	listings[1].ID = "l2"

	mockStore.EXPECT().
		ListListings(mock.Anything, mock.Anything).
		Return(listings, 2, nil).
		Once()

	mockStore.EXPECT().
		GetBaseline(mock.Anything, "key-1").
		Return(nil, pgx.ErrNoRows).
		Once()
	mockStore.EXPECT().
		UpdateScore(mock.Anything, "l1", mock.AnythingOfType("int"), mock.Anything).
		Return(nil).
		Once()

	mockStore.EXPECT().
		GetBaseline(mock.Anything, "key-2").
		Return(nil, pgx.ErrNoRows).
		Once()
	mockStore.EXPECT().
		UpdateScore(mock.Anything, "l2", mock.AnythingOfType("int"), mock.Anything).
		Return(nil).
		Once()

	scored, err := RescoreAll(context.Background(), mockStore)
	require.NoError(t, err)
	assert.Equal(t, 2, scored)
}

func TestRescoreAll_StoreError(t *testing.T) {
	t.Parallel()

	mockStore := storeMocks.NewMockStore(t)

	mockStore.EXPECT().
		ListListings(mock.Anything, mock.Anything).
		Return(nil, 0, errors.New("connection refused")).
		Once()

	scored, err := RescoreAll(context.Background(), mockStore)
	require.Error(t, err)
	assert.Equal(t, 0, scored)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestRescoreListings_StoreError(t *testing.T) {
	t.Parallel()

	mockStore := storeMocks.NewMockStore(t)

	mockStore.EXPECT().
		ListUnscoredListings(mock.Anything, 50).
		Return(nil, errors.New("db error")).
		Once()

	scored, err := RescoreListings(context.Background(), mockStore, 50)
	require.Error(t, err)
	assert.Equal(t, 0, scored)
}

func TestRescoreByProductKey_StoreError(t *testing.T) {
	t.Parallel()

	mockStore := storeMocks.NewMockStore(t)

	mockStore.EXPECT().
		ListListings(mock.Anything, mock.Anything).
		Return(nil, 0, errors.New("db error")).
		Once()

	scored, err := RescoreByProductKey(context.Background(), mockStore, "key-1")
	require.Error(t, err)
	assert.Equal(t, 0, scored)
}

func TestScoreListing_IncrementsWarmBaselineCounter(t *testing.T) {
	before := ptestutil.ToFloat64(metrics.ScoringWithBaselineTotal)

	mockStore := storeMocks.NewMockStore(t)
	mockStore.EXPECT().
		GetBaseline(mock.Anything, "ram:ddr4:ecc_reg:32gb:2666").
		Return(&domain.PriceBaseline{
			ProductKey:  "ram:ddr4:ecc_reg:32gb:2666",
			SampleCount: 50,
			P10:         20.0,
			P25:         35.0,
			P50:         50.0,
			P75:         65.0,
			P90:         80.0,
		}, nil).
		Once()
	mockStore.EXPECT().
		UpdateScore(mock.Anything, "listing-1", mock.AnythingOfType("int"), mock.Anything).
		Return(nil).
		Once()

	err := ScoreListing(context.Background(), mockStore, testListing("ram:ddr4:ecc_reg:32gb:2666"))
	require.NoError(t, err)

	after := ptestutil.ToFloat64(metrics.ScoringWithBaselineTotal)
	assert.InDelta(t, 1, after-before, 0.1, "ScoringWithBaselineTotal should increment by 1")
}

func TestScoreListing_IncrementsColdStartCounter(t *testing.T) {
	before := ptestutil.ToFloat64(metrics.ScoringColdStartTotal)

	mockStore := storeMocks.NewMockStore(t)
	mockStore.EXPECT().
		GetBaseline(mock.Anything, "ram:ddr4:ecc_reg:32gb:2666").
		Return(nil, pgx.ErrNoRows).
		Once()
	mockStore.EXPECT().
		UpdateScore(mock.Anything, "listing-1", mock.AnythingOfType("int"), mock.Anything).
		Return(nil).
		Once()

	err := ScoreListing(context.Background(), mockStore, testListing("ram:ddr4:ecc_reg:32gb:2666"))
	require.NoError(t, err)

	after := ptestutil.ToFloat64(metrics.ScoringColdStartTotal)
	assert.InDelta(t, 1, after-before, 0.1, "ScoringColdStartTotal should increment by 1")
}
