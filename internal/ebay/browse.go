package ebay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
)

const (
	defaultBrowseURL   = "https://api.ebay.com/buy/browse/v1/item_summary/search"
	defaultMarketplace = "EBAY_US"
)

// BrowseClient implements EbayClient using the eBay Browse API.
type BrowseClient struct {
	tokens      TokenProvider
	browseURL   string
	marketplace string
	client      *http.Client
	rateLimiter *RateLimiter
}

// BrowseOption configures the BrowseClient.
type BrowseOption func(*BrowseClient)

// WithBrowseURL overrides the default Browse API endpoint.
func WithBrowseURL(u string) BrowseOption {
	return func(c *BrowseClient) {
		c.browseURL = u
	}
}

// WithMarketplace overrides the default marketplace.
func WithMarketplace(m string) BrowseOption {
	return func(c *BrowseClient) {
		c.marketplace = m
	}
}

// WithBrowseHTTPClient overrides the default HTTP client.
func WithBrowseHTTPClient(hc *http.Client) BrowseOption {
	return func(c *BrowseClient) {
		c.client = hc
	}
}

// WithRateLimiter injects a rate limiter that controls per-second and daily
// API call limits. When set, every Search() call goes through Wait() first.
func WithRateLimiter(r *RateLimiter) BrowseOption {
	return func(c *BrowseClient) {
		c.rateLimiter = r
	}
}

// NewBrowseClient creates a new eBay Browse API client.
func NewBrowseClient(tokens TokenProvider, opts ...BrowseOption) *BrowseClient {
	c := &BrowseClient{
		tokens:      tokens,
		browseURL:   defaultBrowseURL,
		marketplace: defaultMarketplace,
		client:      &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type browseAPIResponse struct {
	ItemSummaries []ItemSummary `json:"itemSummaries"`
	Total         int           `json:"total"`
	Offset        int           `json:"offset"`
	Limit         int           `json:"limit"`
	Next          string        `json:"next"`
}

// Search implements EbayClient.Search by querying the Browse API.
func (c *BrowseClient) Search(
	ctx context.Context,
	req SearchRequest,
) (*SearchResponse, error) {
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			if errors.Is(err, ErrDailyLimitReached) {
				metrics.EbayDailyLimitHits.Inc()
			}
			return nil, fmt.Errorf("rate limit: %w", err)
		}
		metrics.EbayAPICallsTotal.Inc()
		metrics.EbayDailyUsage.Set(float64(c.rateLimiter.DailyCount()))
	}

	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting auth token: %w", err)
	}

	u := c.buildSearchURL(req)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set(
		"X-EBAY-C-MARKETPLACE-ID",
		c.marketplace,
	)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing search request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"eBay API error (status %d): %s",
			resp.StatusCode,
			string(body),
		)
	}

	var apiResp browseAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing search response: %w", err)
	}

	return &SearchResponse{
		Items:   apiResp.ItemSummaries,
		Total:   apiResp.Total,
		Offset:  apiResp.Offset,
		Limit:   apiResp.Limit,
		HasMore: apiResp.Next != "",
	}, nil
}

func (c *BrowseClient) buildSearchURL(req SearchRequest) string {
	params := url.Values{}
	params.Set("q", req.Query)

	if req.CategoryID != "" {
		params.Set("category_ids", req.CategoryID)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	params.Set("limit", strconv.Itoa(limit))

	if req.Offset > 0 {
		params.Set("offset", strconv.Itoa(req.Offset))
	}

	if req.Sort != "" {
		params.Set("sort", req.Sort)
	}

	for k, v := range req.Filters {
		params.Set(k, v)
	}

	return c.browseURL + "?" + params.Encode()
}
