// Package domain defines the core business types for the server price tracker.
package domain

import (
	"encoding/json"
	"slices"
	"time"
)

// ComponentType represents the category of server hardware.
type ComponentType string

// Component type constants.
const (
	ComponentRAM    ComponentType = "ram"
	ComponentDrive  ComponentType = "drive"
	ComponentServer ComponentType = "server"
	ComponentCPU    ComponentType = "cpu"
	ComponentNIC    ComponentType = "nic"
	ComponentOther  ComponentType = "other"
)

// Condition represents normalized listing condition.
type Condition string

// Condition constants.
const (
	ConditionNew         Condition = "new"
	ConditionLikeNew     Condition = "like_new"
	ConditionUsedWorking Condition = "used_working"
	ConditionForParts    Condition = "for_parts"
	ConditionUnknown     Condition = "unknown"
)

// ListingType represents the eBay listing format.
type ListingType string

// Listing type constants.
const (
	ListingAuction   ListingType = "auction"
	ListingBuyItNow  ListingType = "buy_it_now"
	ListingBestOffer ListingType = "best_offer"
)

// Listing represents a processed eBay listing with extracted attributes.
type Listing struct {
	ID       string `json:"id"                  db:"id"`
	EbayID   string `json:"ebay_item_id"        db:"ebay_item_id"`
	Title    string `json:"title"               db:"title"`
	ItemURL  string `json:"item_url"            db:"item_url"`
	ImageURL string `json:"image_url,omitempty" db:"image_url"`

	// Pricing
	Price        float64     `json:"price"                   db:"price"`
	Currency     string      `json:"currency"                db:"currency"`
	ShippingCost *float64    `json:"shipping_cost,omitempty" db:"shipping_cost"`
	ListingType  ListingType `json:"listing_type"            db:"listing_type"`

	// Seller
	SellerName        string  `json:"seller_name"           db:"seller_name"`
	SellerFeedback    int     `json:"seller_feedback_score" db:"seller_feedback_score"`
	SellerFeedbackPct float64 `json:"seller_feedback_pct"   db:"seller_feedback_pct"`
	SellerTopRated    bool    `json:"seller_top_rated"      db:"seller_top_rated"`

	// Extracted data
	ComponentType        ComponentType  `json:"component_type"          db:"component_type"`
	ConditionRaw         string         `json:"condition_raw,omitempty" db:"condition_raw"`
	ConditionNorm        Condition      `json:"condition_norm"          db:"condition_norm"`
	Quantity             int            `json:"quantity"                db:"quantity"`
	Attributes           map[string]any `json:"attributes"              db:"attributes"`
	ExtractionConfidence float64        `json:"extraction_confidence"   db:"extraction_confidence"`
	ProductKey           string         `json:"product_key,omitempty"   db:"product_key"`

	// Scoring
	Score          *int            `json:"score,omitempty"           db:"score"`
	ScoreBreakdown json.RawMessage `json:"score_breakdown,omitempty" db:"score_breakdown"`

	// Timestamps
	ListedAt     *time.Time `json:"listed_at,omitempty"      db:"listed_at"`
	SoldAt       *time.Time `json:"sold_at,omitempty"        db:"sold_at"`
	SoldPrice    *float64   `json:"sold_price,omitempty"     db:"sold_price"`
	AuctionEndAt *time.Time `json:"auction_end_at,omitempty" db:"auction_end_at"`
	FirstSeenAt  time.Time  `json:"first_seen_at"            db:"first_seen_at"`
	UpdatedAt    time.Time  `json:"updated_at"               db:"updated_at"`
}

// UnitPrice returns the per-unit price including shipping.
func (l *Listing) UnitPrice() float64 {
	total := l.Price
	if l.ShippingCost != nil {
		total += *l.ShippingCost
	}
	if l.Quantity > 1 {
		return total / float64(l.Quantity)
	}
	return total
}

// Watch represents a saved search with alert configuration.
type Watch struct {
	ID             string        `json:"id"                       db:"id"`
	Name           string        `json:"name"                     db:"name"`
	SearchQuery    string        `json:"search_query"             db:"search_query"`
	CategoryID     string        `json:"category_id,omitempty"    db:"category_id"`
	ComponentType  ComponentType `json:"component_type"           db:"component_type"`
	Filters        WatchFilters  `json:"filters"                  db:"filters"`
	ScoreThreshold int           `json:"score_threshold"          db:"score_threshold"`
	Enabled        bool          `json:"enabled"                  db:"enabled"`
	LastPolledAt   *time.Time    `json:"last_polled_at,omitempty" db:"last_polled_at"`
	CreatedAt      time.Time     `json:"created_at"               db:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"               db:"updated_at"`
}

// JobRun records a single execution of a scheduled job.
type JobRun struct {
	ID           string     `json:"id"                      db:"id"`
	JobName      string     `json:"job_name"                db:"job_name"`
	StartedAt    time.Time  `json:"started_at"              db:"started_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"  db:"completed_at"`
	Status       string     `json:"status"                  db:"status"`
	ErrorText    string     `json:"error_text,omitempty"    db:"error_text"`
	RowsAffected *int       `json:"rows_affected,omitempty" db:"rows_affected"`
}

// ExtractionJob represents a pending LLM extraction task.
type ExtractionJob struct {
	ID         string    `json:"id"          db:"id"`
	ListingID  string    `json:"listing_id"  db:"listing_id"`
	Priority   int       `json:"priority"    db:"priority"`
	EnqueuedAt time.Time `json:"enqueued_at" db:"enqueued_at"`
	Attempts   int       `json:"attempts"    db:"attempts"`
}

// SystemState holds a precomputed snapshot of aggregate system metrics.
type SystemState struct {
	WatchesTotal                 int `json:"watches_total"                  db:"watches_total"`
	WatchesEnabled               int `json:"watches_enabled"                db:"watches_enabled"`
	ListingsTotal                int `json:"listings_total"                 db:"listings_total"`
	ListingsUnextracted          int `json:"listings_unextracted"           db:"listings_unextracted"`
	ListingsUnscored             int `json:"listings_unscored"              db:"listings_unscored"`
	AlertsPending                int `json:"alerts_pending"                 db:"alerts_pending"`
	BaselinesTotal               int `json:"baselines_total"                db:"baselines_total"`
	BaselinesWarm                int `json:"baselines_warm"                 db:"baselines_warm"`
	BaselinesCold                int `json:"baselines_cold"                 db:"baselines_cold"`
	ProductKeysNoBaseline        int `json:"product_keys_no_baseline"       db:"product_keys_no_baseline"`
	ListingsIncompleteExtraction int `json:"listings_incomplete_extraction" db:"listings_incomplete_extraction"`
	ExtractionQueueDepth         int `json:"extraction_queue_depth"         db:"extraction_queue_depth"`
}

// WatchFilters defines the structured filtering criteria.
type WatchFilters struct {
	// Price
	PriceMax *float64 `json:"price_max,omitempty"`
	PriceMin *float64 `json:"price_min,omitempty"`

	// Seller
	SellerMinFeedback    *int     `json:"seller_min_feedback,omitempty"`
	SellerMinFeedbackPct *float64 `json:"seller_min_feedback_pct,omitempty"`
	SellerTopRatedOnly   bool     `json:"seller_top_rated_only,omitempty"`

	// Condition
	Conditions []Condition `json:"conditions,omitempty"`

	// Component-specific attribute filters (flexible)
	// These match against the extracted attributes JSON.
	// Supports exact match, min/max ranges.
	AttributeFilters map[string]AttributeFilter `json:"attribute_filters,omitempty"`
}

// AttributeFilter supports exact match or range filtering on extracted attributes.
type AttributeFilter struct {
	Equals any      `json:"eq,omitempty"`  // exact match
	Min    *float64 `json:"min,omitempty"` // >= this value
	Max    *float64 `json:"max,omitempty"` // <= this value
	In     []any    `json:"in,omitempty"`  // value in set
}

// Match checks if a listing's attributes satisfy this filter.
func (f *WatchFilters) Match(l *Listing) bool {
	if !f.matchPrice(l) {
		return false
	}
	if !f.matchSeller(l) {
		return false
	}
	if !f.matchCondition(l) {
		return false
	}
	return f.matchAttributes(l)
}

func (f *WatchFilters) matchPrice(l *Listing) bool {
	unitPrice := l.UnitPrice()
	if f.PriceMax != nil && unitPrice > *f.PriceMax {
		return false
	}
	if f.PriceMin != nil && unitPrice < *f.PriceMin {
		return false
	}
	return true
}

func (f *WatchFilters) matchSeller(l *Listing) bool {
	if f.SellerMinFeedback != nil && l.SellerFeedback < *f.SellerMinFeedback {
		return false
	}
	if f.SellerMinFeedbackPct != nil && l.SellerFeedbackPct < *f.SellerMinFeedbackPct {
		return false
	}
	if f.SellerTopRatedOnly && !l.SellerTopRated {
		return false
	}
	return true
}

func (f *WatchFilters) matchCondition(l *Listing) bool {
	if len(f.Conditions) == 0 {
		return true
	}
	return slices.Contains(f.Conditions, l.ConditionNorm)
}

func (f *WatchFilters) matchAttributes(l *Listing) bool {
	for key, filter := range f.AttributeFilters {
		val, ok := l.Attributes[key]
		if !ok {
			return false
		}
		if !matchAttribute(val, filter) {
			return false
		}
	}
	return true
}

func matchAttribute(val any, f AttributeFilter) bool {
	if f.Equals != nil {
		return val == f.Equals
	}
	if f.In != nil {
		return slices.Contains(f.In, val)
	}
	// Range checks for numeric values
	numVal, ok := toFloat64(val)
	if !ok {
		return true // non-numeric, can't range check
	}
	if f.Min != nil && numVal < *f.Min {
		return false
	}
	if f.Max != nil && numVal > *f.Max {
		return false
	}
	return true
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// PriceBaseline holds percentile statistics for a normalized product key.
type PriceBaseline struct {
	ID          string    `json:"id"           db:"id"`
	ProductKey  string    `json:"product_key"  db:"product_key"`
	SampleCount int       `json:"sample_count" db:"sample_count"`
	P10         float64   `json:"p10"          db:"p10"`
	P25         float64   `json:"p25"          db:"p25"`
	P50         float64   `json:"p50"          db:"p50"`
	P75         float64   `json:"p75"          db:"p75"`
	P90         float64   `json:"p90"          db:"p90"`
	Mean        float64   `json:"mean"         db:"mean"`
	UpdatedAt   time.Time `json:"updated_at"   db:"updated_at"`
}

// Alert represents a triggered notification.
type Alert struct {
	ID         string     `json:"id"                    db:"id"`
	WatchID    string     `json:"watch_id"              db:"watch_id"`
	ListingID  string     `json:"listing_id"            db:"listing_id"`
	Score      int        `json:"score"                 db:"score"`
	Notified   bool       `json:"notified"              db:"notified"`
	NotifiedAt *time.Time `json:"notified_at,omitempty" db:"notified_at"`
	CreatedAt  time.Time  `json:"created_at"            db:"created_at"`
}

// ScoreBreakdown details the per-factor scores for a listing.
type ScoreBreakdown struct {
	Price     float64 `json:"price"`
	Seller    float64 `json:"seller"`
	Condition float64 `json:"condition"`
	Quantity  float64 `json:"quantity"`
	Quality   float64 `json:"quality"`
	Time      float64 `json:"time"`
	Total     int     `json:"total"`
}
