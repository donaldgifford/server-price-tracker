package ebay

import (
	"slices"
	"strconv"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ToListings converts eBay API item summaries into domain listings.
func ToListings(items []ItemSummary) []domain.Listing {
	listings := make([]domain.Listing, 0, len(items))
	for i := range items {
		listings = append(listings, toListing(&items[i]))
	}
	return listings
}

func toListing(item *ItemSummary) domain.Listing {
	l := domain.Listing{
		EbayID:      item.ItemID,
		Title:       item.Title,
		ItemURL:     item.ItemWebURL,
		Currency:    item.Price.Currency,
		ListingType: parseListingType(item.BuyingOptions),
		Quantity:    1,
	}

	// Price
	if p, err := strconv.ParseFloat(item.Price.Value, 64); err == nil {
		l.Price = p
	}

	// Image
	if item.Image != nil && item.Image.ImageURL != "" {
		l.ImageURL = item.Image.ImageURL
	}

	// Seller
	if item.Seller != nil {
		l.SellerName = item.Seller.Username
		l.SellerFeedback = item.Seller.FeedbackScore
		if pct, err := strconv.ParseFloat(
			item.Seller.FeedbackPercentage,
			64,
		); err == nil {
			l.SellerFeedbackPct = pct
		}
	}

	// Top-rated seller
	l.SellerTopRated = item.TopRatedBuyingExperience

	// Condition
	l.ConditionRaw = item.Condition

	// Shipping
	if len(item.ShippingOptions) > 0 {
		if sc := item.ShippingOptions[0].ShippingCost; sc != nil {
			if cost, err := strconv.ParseFloat(sc.Value, 64); err == nil {
				l.ShippingCost = &cost
			}
		}
	}

	return l
}

func parseListingType(buyingOptions []string) domain.ListingType {
	if slices.Contains(buyingOptions, "AUCTION") {
		return domain.ListingAuction
	}
	if slices.Contains(buyingOptions, "BEST_OFFER") {
		return domain.ListingBestOffer
	}
	return domain.ListingBuyItNow
}
