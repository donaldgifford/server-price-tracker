package ebay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultAnalyticsURL = "https://api.ebay.com/developer/analytics/v1_beta/rate_limit/"

	// browseResourceName is the Analytics API resource name for Browse API
	// search calls (item_summary/search).
	browseResourceName = "buy.browse"
)

// rateLimitResponse is the top-level Analytics API response.
type rateLimitResponse struct {
	RateLimits []rateLimitEntry `json:"rateLimits"`
}

// rateLimitEntry represents one API context in the Analytics response.
type rateLimitEntry struct {
	APIContext string     `json:"apiContext"`
	APIName    string     `json:"apiName"`
	APIVersion string     `json:"apiVersion"`
	Resources  []resource `json:"resources"`
}

// resource represents one API resource with its rate limits.
type resource struct {
	Name  string      `json:"name"`
	Rates []quotaRate `json:"rates"`
}

// quotaRate holds the quota state for a single resource.
type quotaRate struct {
	Count      int64  `json:"count"`
	Limit      int64  `json:"limit"`
	Remaining  int64  `json:"remaining"`
	Reset      string `json:"reset"`
	TimeWindow int64  `json:"timeWindow"`
}

// QuotaState holds the parsed rate limit state for a single eBay API resource.
type QuotaState struct {
	Count      int64
	Limit      int64
	Remaining  int64
	ResetAt    time.Time
	TimeWindow time.Duration
}

// AnalyticsClient queries the eBay Developer Analytics API for rate limit state.
type AnalyticsClient struct {
	tokens       TokenProvider
	analyticsURL string
	client       *http.Client
}

// AnalyticsOption configures the AnalyticsClient.
type AnalyticsOption func(*AnalyticsClient)

// WithAnalyticsURL overrides the default Analytics API endpoint.
func WithAnalyticsURL(u string) AnalyticsOption {
	return func(c *AnalyticsClient) {
		c.analyticsURL = u
	}
}

// WithAnalyticsHTTPClient overrides the default HTTP client.
func WithAnalyticsHTTPClient(hc *http.Client) AnalyticsOption {
	return func(c *AnalyticsClient) {
		c.client = hc
	}
}

// NewAnalyticsClient creates a new eBay Analytics API client.
func NewAnalyticsClient(
	tokens TokenProvider,
	opts ...AnalyticsOption,
) *AnalyticsClient {
	c := &AnalyticsClient{
		tokens:       tokens,
		analyticsURL: defaultAnalyticsURL,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetBrowseQuota returns the current rate limit state for the Browse API
// search resource (buy.browse). It queries the Analytics API filtered to
// api_context=buy and api_name=browse.
func (c *AnalyticsClient) GetBrowseQuota(
	ctx context.Context,
) (*QuotaState, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting auth token: %w", err)
	}

	u, err := url.Parse(c.analyticsURL)
	if err != nil {
		return nil, fmt.Errorf("parsing analytics URL: %w", err)
	}

	q := u.Query()
	q.Set("api_context", "buy")
	q.Set("api_name", "browse")
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodGet, u.String(), http.NoBody,
	)
	if err != nil {
		return nil, fmt.Errorf("creating analytics request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing analytics request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading analytics response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"analytics API error (status %d): %s",
			resp.StatusCode,
			string(body),
		)
	}

	var apiResp rateLimitResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("parsing analytics response: %w", err)
	}

	return extractBrowseQuota(apiResp)
}

// extractBrowseQuota finds the buy.browse resource in the response and
// returns its quota state.
func extractBrowseQuota(resp rateLimitResponse) (*QuotaState, error) {
	for _, entry := range resp.RateLimits {
		for _, res := range entry.Resources {
			if res.Name != browseResourceName {
				continue
			}
			if len(res.Rates) == 0 {
				return nil, fmt.Errorf(
					"no rates found for resource %q", browseResourceName,
				)
			}

			r := res.Rates[0]

			resetAt, err := time.Parse(time.RFC3339, r.Reset)
			if err != nil {
				return nil, fmt.Errorf(
					"parsing reset time %q: %w", r.Reset, err,
				)
			}

			return &QuotaState{
				Count:      r.Count,
				Limit:      r.Limit,
				Remaining:  r.Remaining,
				ResetAt:    resetAt,
				TimeWindow: time.Duration(r.TimeWindow) * time.Second,
			}, nil
		}
	}

	return nil, fmt.Errorf("resource %q not found in analytics response", browseResourceName)
}
