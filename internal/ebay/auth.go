package ebay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultTokenURL = "https://api.ebay.com/identity/v1/oauth2/token" //nolint:gosec // not a credential
	refreshBuffer   = 60 * time.Second
)

// OAuthTokenProvider implements TokenProvider using the eBay OAuth2
// client credentials flow. It caches tokens and refreshes automatically
// when expired or within 60 seconds of expiry. Thread-safe via mutex.
type OAuthTokenProvider struct {
	appID    string
	certID   string
	tokenURL string
	client   *http.Client
	scopes   string

	mu      sync.Mutex
	token   string
	expiry  time.Time
	nowFunc func() time.Time // for testing
}

// OAuthOption configures the OAuthTokenProvider.
type OAuthOption func(*OAuthTokenProvider)

// WithTokenURL overrides the default eBay token endpoint.
func WithTokenURL(u string) OAuthOption {
	return func(p *OAuthTokenProvider) {
		p.tokenURL = u
	}
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(c *http.Client) OAuthOption {
	return func(p *OAuthTokenProvider) {
		p.client = c
	}
}

// WithNowFunc overrides the time function for testing.
func WithNowFunc(f func() time.Time) OAuthOption {
	return func(p *OAuthTokenProvider) {
		p.nowFunc = f
	}
}

// NewOAuthTokenProvider creates a new eBay OAuth2 token provider.
func NewOAuthTokenProvider(
	appID, certID string,
	opts ...OAuthOption,
) *OAuthTokenProvider {
	p := &OAuthTokenProvider{
		appID:    appID,
		certID:   certID,
		tokenURL: defaultTokenURL,
		client:   &http.Client{Timeout: 10 * time.Second},
		scopes:   "https://api.ebay.com/oauth/api_scope",
		nowFunc:  time.Now,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// Token returns a valid OAuth2 access token, refreshing if necessary.
func (p *OAuthTokenProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.token != "" && p.nowFunc().Before(p.expiry.Add(-refreshBuffer)) {
		return p.token, nil
	}

	return p.refreshLocked(ctx)
}

func (p *OAuthTokenProvider) refreshLocked(
	ctx context.Context,
) (string, error) {
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {p.scopes},
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.tokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("creating token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	creds := base64.StdEncoding.EncodeToString(
		[]byte(p.appID + ":" + p.certID),
	)
	req.Header.Set("Authorization", "Basic "+creds)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp tokenErrorResponse
		_ = json.Unmarshal(body, &errResp) //nolint:errcheck // best-effort error parsing
		return "", fmt.Errorf(
			"token request failed (status %d): %s - %s",
			resp.StatusCode,
			errResp.Error,
			errResp.ErrorDescription,
		)
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}

	p.token = tokenResp.AccessToken
	p.expiry = p.nowFunc().Add(
		time.Duration(tokenResp.ExpiresIn) * time.Second,
	)

	return p.token, nil
}
