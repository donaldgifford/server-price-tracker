//go:build integration

package ebay_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
)

// TestBrowseClient_Integration requires live eBay API credentials.
// Run with: go test -tags=integration -run TestBrowseClient_Integration ./internal/ebay/...
//
// Required environment variables:
//   - EBAY_APP_ID: eBay application ID (client ID)
//   - EBAY_CERT_ID: eBay certificate ID (client secret)
func TestBrowseClient_Integration(t *testing.T) {
	appID := os.Getenv("EBAY_APP_ID")
	certID := os.Getenv("EBAY_CERT_ID")

	if appID == "" || certID == "" {
		t.Skip("EBAY_APP_ID and EBAY_CERT_ID must be set for integration tests")
	}

	tokens := ebay.NewOAuthTokenProvider(appID, certID)
	client := ebay.NewBrowseClient(tokens)

	resp, err := client.Search(context.Background(), ebay.SearchRequest{
		Query: "32GB DDR4 ECC",
		Limit: 3,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Positive(t, resp.Total)
	assert.NotEmpty(t, resp.Items)

	for _, item := range resp.Items {
		assert.NotEmpty(t, item.ItemID)
		assert.NotEmpty(t, item.Title)
		assert.NotEmpty(t, item.Price.Value)
	}
}
