package ebay_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestToListings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []ebay.ItemSummary
		want  []domain.Listing
	}{
		{
			name:  "empty input returns empty slice",
			items: nil,
			want:  []domain.Listing{},
		},
		{
			name: "complete item converts all fields",
			items: []ebay.ItemSummary{
				completeItem(),
			},
			want: []domain.Listing{
				{
					EbayID:            "v1|123456|0",
					Title:             "Samsung 32GB DDR4 ECC REG",
					ItemURL:           "https://www.ebay.com/itm/123456",
					ImageURL:          "https://i.ebayimg.com/images/123.jpg",
					Price:             45.99,
					Currency:          "USD",
					ShippingCost:      floatPtr(5.99),
					ListingType:       domain.ListingBuyItNow,
					SellerName:        "server_parts_co",
					SellerFeedback:    5432,
					SellerFeedbackPct: 99.8,
					SellerTopRated:    true,
					ConditionRaw:      "Used",
					Quantity:          1,
				},
			},
		},
		{
			name: "item missing optional fields",
			items: []ebay.ItemSummary{
				{
					ItemID:     "v1|789|0",
					Title:      "Mystery RAM",
					Price:      ebay.ItemPrice{Value: "10.00", Currency: "USD"},
					ItemWebURL: "https://www.ebay.com/itm/789",
					// No image, no seller, no shipping
					BuyingOptions: []string{"FIXED_PRICE"},
				},
			},
			want: []domain.Listing{
				{
					EbayID:      "v1|789|0",
					Title:       "Mystery RAM",
					ItemURL:     "https://www.ebay.com/itm/789",
					Price:       10.00,
					Currency:    "USD",
					ListingType: domain.ListingBuyItNow,
					Quantity:    1,
				},
			},
		},
		{
			name: "auction listing type",
			items: []ebay.ItemSummary{
				{
					ItemID:        "v1|111|0",
					Title:         "Auction Item",
					Price:         ebay.ItemPrice{Value: "1.00", Currency: "USD"},
					ItemWebURL:    "https://www.ebay.com/itm/111",
					BuyingOptions: []string{"AUCTION"},
				},
			},
			want: []domain.Listing{
				{
					EbayID:      "v1|111|0",
					Title:       "Auction Item",
					ItemURL:     "https://www.ebay.com/itm/111",
					Price:       1.00,
					Currency:    "USD",
					ListingType: domain.ListingAuction,
					Quantity:    1,
				},
			},
		},
		{
			name: "best offer listing type",
			items: []ebay.ItemSummary{
				{
					ItemID:        "v1|222|0",
					Title:         "Best Offer Item",
					Price:         ebay.ItemPrice{Value: "50.00", Currency: "USD"},
					ItemWebURL:    "https://www.ebay.com/itm/222",
					BuyingOptions: []string{"FIXED_PRICE", "BEST_OFFER"},
				},
			},
			want: []domain.Listing{
				{
					EbayID:      "v1|222|0",
					Title:       "Best Offer Item",
					ItemURL:     "https://www.ebay.com/itm/222",
					Price:       50.00,
					Currency:    "USD",
					ListingType: domain.ListingBestOffer,
					Quantity:    1,
				},
			},
		},
		{
			name: "multiple items converted",
			items: []ebay.ItemSummary{
				{
					ItemID:        "v1|aaa|0",
					Title:         "Item A",
					Price:         ebay.ItemPrice{Value: "10.00", Currency: "USD"},
					ItemWebURL:    "https://www.ebay.com/itm/aaa",
					BuyingOptions: []string{"FIXED_PRICE"},
				},
				{
					ItemID:        "v1|bbb|0",
					Title:         "Item B",
					Price:         ebay.ItemPrice{Value: "20.00", Currency: "GBP"},
					ItemWebURL:    "https://www.ebay.com/itm/bbb",
					BuyingOptions: []string{"AUCTION"},
				},
			},
			want: []domain.Listing{
				{
					EbayID:      "v1|aaa|0",
					Title:       "Item A",
					ItemURL:     "https://www.ebay.com/itm/aaa",
					Price:       10.00,
					Currency:    "USD",
					ListingType: domain.ListingBuyItNow,
					Quantity:    1,
				},
				{
					EbayID:      "v1|bbb|0",
					Title:       "Item B",
					ItemURL:     "https://www.ebay.com/itm/bbb",
					Price:       20.00,
					Currency:    "GBP",
					ListingType: domain.ListingAuction,
					Quantity:    1,
				},
			},
		},
		{
			name: "invalid price value defaults to zero",
			items: []ebay.ItemSummary{
				{
					ItemID:        "v1|bad|0",
					Title:         "Bad Price",
					Price:         ebay.ItemPrice{Value: "not-a-number", Currency: "USD"},
					ItemWebURL:    "https://www.ebay.com/itm/bad",
					BuyingOptions: []string{"FIXED_PRICE"},
				},
			},
			want: []domain.Listing{
				{
					EbayID:      "v1|bad|0",
					Title:       "Bad Price",
					ItemURL:     "https://www.ebay.com/itm/bad",
					Price:       0,
					Currency:    "USD",
					ListingType: domain.ListingBuyItNow,
					Quantity:    1,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ebay.ToListings(tt.items)
			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				assert.Equal(t, tt.want[i].EbayID, got[i].EbayID)
				assert.Equal(t, tt.want[i].Title, got[i].Title)
				assert.Equal(t, tt.want[i].ItemURL, got[i].ItemURL)
				assert.Equal(t, tt.want[i].ImageURL, got[i].ImageURL)
				assert.InDelta(t, tt.want[i].Price, got[i].Price, 0.01)
				assert.Equal(t, tt.want[i].Currency, got[i].Currency)
				assert.Equal(t, tt.want[i].ListingType, got[i].ListingType)
				assert.Equal(t, tt.want[i].SellerName, got[i].SellerName)
				assert.Equal(t, tt.want[i].SellerFeedback, got[i].SellerFeedback)
				assert.InDelta(
					t,
					tt.want[i].SellerFeedbackPct,
					got[i].SellerFeedbackPct,
					0.01,
				)
				assert.Equal(t, tt.want[i].SellerTopRated, got[i].SellerTopRated)
				assert.Equal(t, tt.want[i].ConditionRaw, got[i].ConditionRaw)
				assert.Equal(t, tt.want[i].Quantity, got[i].Quantity)
				if tt.want[i].ShippingCost != nil {
					require.NotNil(t, got[i].ShippingCost)
					assert.InDelta(
						t,
						*tt.want[i].ShippingCost,
						*got[i].ShippingCost,
						0.01,
					)
				} else {
					assert.Nil(t, got[i].ShippingCost)
				}
			}
		})
	}
}

func completeItem() ebay.ItemSummary {
	return ebay.ItemSummary{
		ItemID:     "v1|123456|0",
		Title:      "Samsung 32GB DDR4 ECC REG",
		Price:      ebay.ItemPrice{Value: "45.99", Currency: "USD"},
		ItemWebURL: "https://www.ebay.com/itm/123456",
		Image:      &ebay.ItemImage{ImageURL: "https://i.ebayimg.com/images/123.jpg"},
		Seller: &ebay.ItemSeller{
			Username:           "server_parts_co",
			FeedbackScore:      5432,
			FeedbackPercentage: "99.8",
		},
		Condition:     "Used",
		BuyingOptions: []string{"FIXED_PRICE"},
		ShippingOptions: []ebay.ShippingOption{
			{ShippingCost: &ebay.ItemPrice{Value: "5.99", Currency: "USD"}},
		},
		TopRatedBuyingExperience: true,
	}
}

func floatPtr(f float64) *float64 {
	return &f
}
