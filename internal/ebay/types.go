package ebay

// ItemSummary represents a single item from the eBay Browse API search response.
type ItemSummary struct {
	ItemID          string           `json:"itemId"`
	Title           string           `json:"title"`
	Price           ItemPrice        `json:"price"`
	ItemWebURL      string           `json:"itemWebUrl"`
	Image           *ItemImage       `json:"image,omitempty"`
	Seller          *ItemSeller      `json:"seller,omitempty"`
	Condition       string           `json:"condition"`
	ConditionID     string           `json:"conditionId"`
	BuyingOptions   []string         `json:"buyingOptions"`
	ShippingOptions []ShippingOption `json:"shippingOptions,omitempty"`
	ItemEndDate     string           `json:"itemEndDate,omitempty"`
	Categories      []ItemCategory   `json:"categories,omitempty"`

	TopRatedBuyingExperience bool `json:"topRatedBuyingExperience"`
}

// ItemPrice holds eBay price information.
type ItemPrice struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

// ItemImage holds eBay image information.
type ItemImage struct {
	ImageURL string `json:"imageUrl"`
}

// ItemSeller holds eBay seller information.
type ItemSeller struct {
	Username           string `json:"username"`
	FeedbackScore      int    `json:"feedbackScore"`
	FeedbackPercentage string `json:"feedbackPercentage"`
}

// ShippingOption holds eBay shipping information.
type ShippingOption struct {
	ShippingCost *ItemPrice `json:"shippingCost,omitempty"`
}

// ItemCategory holds eBay category information.
type ItemCategory struct {
	CategoryID string `json:"categoryId"`
}
