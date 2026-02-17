package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
)

func testAlert(score int) AlertPayload {
	return AlertPayload{
		WatchName:     "DDR4 ECC REG",
		ListingTitle:  "Samsung 32GB DDR4 ECC REG",
		EbayURL:       "https://www.ebay.com/itm/123456789",
		ImageURL:      "https://i.ebayimg.com/images/g/test/s-l1600.jpg",
		Price:         "$45.99",
		UnitPrice:     "$45.99",
		Score:         score,
		Seller:        "server_parts_inc (5432, 99.8%)",
		Condition:     "used_working",
		ComponentType: "ram",
	}
}

func TestDiscordNotifier_SendAlert(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		alert      AlertPayload
		statusCode int
		wantErr    bool
		errMsg     string
		wantColor  int
	}{
		{
			name:       "valid alert sends embed",
			alert:      testAlert(85),
			statusCode: http.StatusNoContent,
			wantColor:  colorYellow,
		},
		{
			name:       "score 92 uses green color",
			alert:      testAlert(92),
			statusCode: http.StatusNoContent,
			wantColor:  colorGreen,
		},
		{
			name:       "score 85 uses yellow color",
			alert:      testAlert(85),
			statusCode: http.StatusNoContent,
			wantColor:  colorYellow,
		},
		{
			name:       "score 76 uses orange color",
			alert:      testAlert(76),
			statusCode: http.StatusNoContent,
			wantColor:  colorOrange,
		},
		{
			name:       "discord returns 429 rate limited",
			alert:      testAlert(85),
			statusCode: http.StatusTooManyRequests,
			wantErr:    true,
			errMsg:     "rate limited",
		},
		{
			name:       "discord returns 400 error",
			alert:      testAlert(85),
			statusCode: http.StatusBadRequest,
			wantErr:    true,
			errMsg:     "discord returned 400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var received discordWebhookPayload

			srv := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					assert.Equal(t, http.MethodPost, r.Method)

					err := json.NewDecoder(r.Body).Decode(&received)
					assert.NoError(t, err)

					w.WriteHeader(tt.statusCode)
				}),
			)
			defer srv.Close()

			d := NewDiscordNotifier(srv.URL)
			err := d.SendAlert(context.Background(), &tt.alert)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}

			require.NoError(t, err)
			require.Len(t, received.Embeds, 1)

			embed := received.Embeds[0]
			assert.Equal(t, tt.wantColor, embed.Color)
			assert.Contains(t, embed.Title, tt.alert.ListingTitle)
			assert.Equal(t, tt.alert.EbayURL, embed.URL)
			assert.NotNil(t, embed.Thumbnail)
			assert.Equal(t, tt.alert.ImageURL, embed.Thumbnail.URL)

			// Verify fields.
			fieldMap := make(map[string]string)
			for _, f := range embed.Fields {
				fieldMap[f.Name] = f.Value
			}
			assert.Equal(t, fmt.Sprintf("%d/100", tt.alert.Score), fieldMap["Score"])
			assert.Equal(t, tt.alert.Price, fieldMap["Price"])
			assert.Equal(t, tt.alert.Seller, fieldMap["Seller"])
		})
	}
}

func TestDiscordNotifier_SendAlert_NoImage(t *testing.T) {
	t.Parallel()

	var received discordWebhookPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := json.NewDecoder(r.Body).Decode(&received)
		assert.NoError(t, err)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	alert := testAlert(90)
	alert.ImageURL = ""

	d := NewDiscordNotifier(srv.URL)
	err := d.SendAlert(context.Background(), &alert)
	require.NoError(t, err)

	require.Len(t, received.Embeds, 1)
	assert.Nil(t, received.Embeds[0].Thumbnail)
}

func TestDiscordNotifier_SendBatchAlert(t *testing.T) {
	t.Parallel()

	var received discordWebhookPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := json.NewDecoder(r.Body).Decode(&received)
		assert.NoError(t, err)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	alerts := make([]AlertPayload, 3)
	for i := range alerts {
		alerts[i] = testAlert(80 + i)
	}

	d := NewDiscordNotifier(srv.URL)
	err := d.SendBatchAlert(context.Background(), alerts, "DDR4 Watch")
	require.NoError(t, err)

	assert.Len(t, received.Embeds, 3)
}

func TestDiscordNotifier_NetworkError(t *testing.T) {
	t.Parallel()

	d := NewDiscordNotifier("http://127.0.0.1:1") // nothing listening
	alert := testAlert(85)
	err := d.SendAlert(context.Background(), &alert)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sending discord webhook")
}

func TestDiscordNotifier_InvalidWebhookURL(t *testing.T) {
	t.Parallel()

	// Edge case: Discord webhook with malformed URL.
	d := NewDiscordNotifier("://not-a-valid-url")
	alert := testAlert(85)
	err := d.SendAlert(context.Background(), &alert)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating discord request")
}

func TestWithHTTPClient(t *testing.T) {
	t.Parallel()

	custom := &http.Client{}
	d := NewDiscordNotifier("https://example.com", WithHTTPClient(custom))
	assert.Same(t, custom, d.client)
}

func getNotificationHistogramSampleCount() uint64 {
	ch := make(chan prometheus.Metric, 1)
	metrics.NotificationDuration.Collect(ch)
	m := <-ch
	pb := &dto.Metric{}
	_ = m.Write(pb)
	return pb.GetHistogram().GetSampleCount()
}

func TestSendAlert_ObservesNotificationDuration(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	before := getNotificationHistogramSampleCount()

	d := NewDiscordNotifier(srv.URL)
	err := d.SendAlert(context.Background(), &AlertPayload{
		WatchName:    "test",
		ListingTitle: "Test",
		Score:        85,
	})
	require.NoError(t, err)

	after := getNotificationHistogramSampleCount()
	assert.Greater(t, after, before, "NotificationDuration histogram sample count should increase")
}
