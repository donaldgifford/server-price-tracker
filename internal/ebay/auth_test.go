package ebay_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
)

// tokenJSON returns a valid eBay OAuth2 token response as JSON bytes.
func tokenJSON(token string) []byte {
	return []byte(fmt.Sprintf(
		`{"access_token":%q,"expires_in":7200,"token_type":"Application Access Token"}`,
		token,
	))
}

func TestOAuthTokenProvider_Token(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		wantToken  string
		errContain string
	}{
		{
			name: "successful token fetch",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(tokenJSON("test-token-123"))
			},
			wantToken: "test-token-123",
		},
		{
			name: "server returns 401",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(
					[]byte(
						`{"error":"invalid_client","error_description":"client authentication failed"}`,
					),
				)
			},
			wantErr:    true,
			errContain: "status 401",
		},
		{
			name: "server returns 500",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:    true,
			errContain: "status 500",
		},
		{
			name: "server returns invalid JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("not json"))
			},
			wantErr:    true,
			errContain: "parsing token response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			provider := ebay.NewOAuthTokenProvider(
				"test-app-id",
				"test-cert-id",
				ebay.WithTokenURL(srv.URL),
			)

			token, err := provider.Token(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContain)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestOAuthTokenProvider_TokenCaching(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(tokenJSON("cached-token"))
		}),
	)
	defer srv.Close()

	provider := ebay.NewOAuthTokenProvider(
		"test-app-id",
		"test-cert-id",
		ebay.WithTokenURL(srv.URL),
	)

	// First call should hit the server.
	token1, err := provider.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "cached-token", token1)
	assert.Equal(t, int32(1), callCount.Load())

	// Second call should return cached token (no HTTP call).
	token2, err := provider.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "cached-token", token2)
	assert.Equal(t, int32(1), callCount.Load())
}

func TestOAuthTokenProvider_TokenRefreshOnExpiry(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	now := time.Now()

	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(tokenJSON("refreshed-token"))
		}),
	)
	defer srv.Close()

	currentTime := now
	var mu sync.Mutex

	provider := ebay.NewOAuthTokenProvider(
		"test-app-id",
		"test-cert-id",
		ebay.WithTokenURL(srv.URL),
		ebay.WithNowFunc(func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return currentTime
		}),
	)

	// First call fetches token.
	_, err := provider.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	// Advance time past expiry (7200s - 60s buffer = 7140s).
	mu.Lock()
	currentTime = now.Add(7200 * time.Second)
	mu.Unlock()

	// This call should refresh.
	_, err = provider.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load())
}

func TestOAuthTokenProvider_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount.Add(1)
			time.Sleep(10 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(tokenJSON("concurrent-token"))
		}),
	)
	defer srv.Close()

	provider := ebay.NewOAuthTokenProvider(
		"test-app-id",
		"test-cert-id",
		ebay.WithTokenURL(srv.URL),
	)

	const goroutines = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			token, err := provider.Token(context.Background())
			assert.NoError(t, err)
			assert.Equal(t, "concurrent-token", token)
		}()
	}

	wg.Wait()

	// With mutex, only a few calls should happen at most.
	assert.Less(t, callCount.Load(), int32(goroutines))
}

func TestOAuthTokenProvider_RequestFormat(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request format.
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(
				t,
				"application/x-www-form-urlencoded",
				r.Header.Get("Content-Type"),
			)

			// Verify Authorization header is Basic auth.
			auth := r.Header.Get("Authorization")
			assert.Contains(t, auth, "Basic ")

			// Verify form body.
			assert.NoError(t, r.ParseForm())
			assert.Equal(t, "client_credentials", r.FormValue("grant_type"))
			assert.Contains(t, r.FormValue("scope"), "api.ebay.com")

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(tokenJSON("format-test-token"))
		}),
	)
	defer srv.Close()

	provider := ebay.NewOAuthTokenProvider(
		"my-app-id",
		"my-cert-id",
		ebay.WithTokenURL(srv.URL),
	)

	token, err := provider.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "format-test-token", token)
}
