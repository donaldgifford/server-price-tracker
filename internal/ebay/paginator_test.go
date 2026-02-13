package ebay_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	ebayMocks "github.com/donaldgifford/server-price-tracker/internal/ebay/mocks"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestPaginator_Paginate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		isFirstRun  bool
		maxPages    int
		setupMocks  func(*ebayMocks.MockEbayClient, *storeMocks.MockStore)
		wantNew     int
		wantPages   int
		wantStopped string
		wantErr     bool
	}{
		{
			name: "stops when known listing found",
			setupMocks: func(
				ec *ebayMocks.MockEbayClient,
				ms *storeMocks.MockStore,
			) {
				ec.EXPECT().
					Search(mock.Anything, mock.Anything).
					Return(&ebay.SearchResponse{
						Items: []ebay.ItemSummary{
							{
								ItemID:     "new-1",
								Title:      "New Item",
								Price:      ebay.ItemPrice{Value: "10.00", Currency: "USD"},
								ItemWebURL: "https://ebay.com/1",
							},
							{
								ItemID:     "known-1",
								Title:      "Known Item",
								Price:      ebay.ItemPrice{Value: "20.00", Currency: "USD"},
								ItemWebURL: "https://ebay.com/2",
							},
							{
								ItemID:     "new-2",
								Title:      "After Known",
								Price:      ebay.ItemPrice{Value: "30.00", Currency: "USD"},
								ItemWebURL: "https://ebay.com/3",
							},
						},
						HasMore: true,
					}, nil).Once()

				ms.EXPECT().GetListing(mock.Anything, "new-1").Return(nil, nil).Once()
				ms.EXPECT().
					GetListing(mock.Anything, "known-1").
					Return(&domain.Listing{EbayID: "known-1"}, nil).
					Once()
			},
			wantNew:     1,
			wantPages:   1,
			wantStopped: "known_listing",
		},
		{
			name:     "stops at max pages",
			maxPages: 2,
			setupMocks: func(
				ec *ebayMocks.MockEbayClient,
				ms *storeMocks.MockStore,
			) {
				// Page 1
				ec.EXPECT().
					Search(mock.Anything, mock.MatchedBy(func(r ebay.SearchRequest) bool {
						return r.Offset == 0
					})).
					Return(&ebay.SearchResponse{
						Items: []ebay.ItemSummary{
							{
								ItemID:     "p1-1",
								Title:      "Page1 Item",
								Price:      ebay.ItemPrice{Value: "10.00", Currency: "USD"},
								ItemWebURL: "https://ebay.com/1",
							},
						},
						HasMore: true,
					}, nil).Once()

				// Page 2
				ec.EXPECT().
					Search(mock.Anything, mock.MatchedBy(func(r ebay.SearchRequest) bool {
						return r.Offset > 0
					})).
					Return(&ebay.SearchResponse{
						Items: []ebay.ItemSummary{
							{
								ItemID:     "p2-1",
								Title:      "Page2 Item",
								Price:      ebay.ItemPrice{Value: "20.00", Currency: "USD"},
								ItemWebURL: "https://ebay.com/2",
							},
						},
						HasMore: true,
					}, nil).Once()

				ms.EXPECT().
					GetListing(mock.Anything, mock.Anything).
					Return(nil, nil)
			},
			wantNew:     2,
			wantPages:   2,
			wantStopped: "max_pages",
		},
		{
			name: "stops when no more results",
			setupMocks: func(
				ec *ebayMocks.MockEbayClient,
				ms *storeMocks.MockStore,
			) {
				ec.EXPECT().
					Search(mock.Anything, mock.Anything).
					Return(&ebay.SearchResponse{
						Items: []ebay.ItemSummary{
							{
								ItemID:     "only-1",
								Title:      "Only Item",
								Price:      ebay.ItemPrice{Value: "10.00", Currency: "USD"},
								ItemWebURL: "https://ebay.com/1",
							},
						},
						HasMore: false,
					}, nil).Once()

				ms.EXPECT().
					GetListing(mock.Anything, "only-1").
					Return(nil, nil).
					Once()
			},
			wantNew:     1,
			wantPages:   1,
			wantStopped: "no_more_results",
		},
		{
			name: "stops when empty response",
			setupMocks: func(
				ec *ebayMocks.MockEbayClient,
				_ *storeMocks.MockStore,
			) {
				ec.EXPECT().
					Search(mock.Anything, mock.Anything).
					Return(&ebay.SearchResponse{
						Items:   []ebay.ItemSummary{},
						HasMore: false,
					}, nil).Once()
			},
			wantNew:     0,
			wantPages:   1,
			wantStopped: "no_more_results",
		},
		{
			name:       "first run caps at 5 pages",
			isFirstRun: true,
			maxPages:   10,
			setupMocks: func(
				ec *ebayMocks.MockEbayClient,
				ms *storeMocks.MockStore,
			) {
				// Return results for 5 pages, all with HasMore=true.
				ec.EXPECT().
					Search(mock.Anything, mock.Anything).
					Return(&ebay.SearchResponse{
						Items: []ebay.ItemSummary{
							{
								ItemID:     "item",
								Title:      "Item",
								Price:      ebay.ItemPrice{Value: "10.00", Currency: "USD"},
								ItemWebURL: "https://ebay.com/1",
							},
						},
						HasMore: true,
					}, nil).Times(5)

				ms.EXPECT().
					GetListing(mock.Anything, mock.Anything).
					Return(nil, nil)
			},
			wantNew:     5,
			wantPages:   5,
			wantStopped: "max_pages",
		},
		{
			name: "eBay client error",
			setupMocks: func(
				ec *ebayMocks.MockEbayClient,
				_ *storeMocks.MockStore,
			) {
				ec.EXPECT().
					Search(mock.Anything, mock.Anything).
					Return(nil, errors.New("connection refused")).Once()
			},
			wantErr: true,
		},
		{
			name: "store GetListing error continues processing",
			setupMocks: func(
				ec *ebayMocks.MockEbayClient,
				ms *storeMocks.MockStore,
			) {
				ec.EXPECT().
					Search(mock.Anything, mock.Anything).
					Return(&ebay.SearchResponse{
						Items: []ebay.ItemSummary{
							{
								ItemID:     "err-1",
								Title:      "Error Item",
								Price:      ebay.ItemPrice{Value: "10.00", Currency: "USD"},
								ItemWebURL: "https://ebay.com/1",
							},
							{
								ItemID:     "ok-1",
								Title:      "OK Item",
								Price:      ebay.ItemPrice{Value: "20.00", Currency: "USD"},
								ItemWebURL: "https://ebay.com/2",
							},
						},
						HasMore: false,
					}, nil).Once()

				ms.EXPECT().
					GetListing(mock.Anything, "err-1").
					Return(nil, errors.New("db error")).
					Once()
				ms.EXPECT().
					GetListing(mock.Anything, "ok-1").
					Return(nil, nil).
					Once()
			},
			wantNew:     2,
			wantPages:   1,
			wantStopped: "no_more_results",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockClient := ebayMocks.NewMockEbayClient(t)
			mockStore := storeMocks.NewMockStore(t)

			tt.setupMocks(mockClient, mockStore)

			maxPages := tt.maxPages
			if maxPages == 0 {
				maxPages = 10
			}

			paginator := ebay.NewPaginator(
				mockClient,
				mockStore,
				ebay.WithPageSize(200),
				ebay.WithMaxPages(maxPages),
			)

			result, err := paginator.Paginate(
				context.Background(),
				ebay.SearchRequest{Query: "test"},
				tt.isFirstRun,
			)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Len(t, result.NewListings, tt.wantNew)
			assert.Equal(t, tt.wantPages, result.PagesUsed)
			assert.Equal(t, tt.wantStopped, result.StoppedAt)
		})
	}
}
