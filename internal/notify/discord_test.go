package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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
	sent, err := d.SendBatchAlert(context.Background(), alerts, "DDR4 Watch")
	require.NoError(t, err)
	assert.Equal(t, 3, sent)

	assert.Len(t, received.Embeds, 3)
}

// TestSendBatchAlert_Chunking exercises the chunked-send replacement
// for the old truncation+summary path. For every input size, the
// notifier should produce ceil(n/10) POSTs, each carrying at most 10
// embeds, and the cumulative embed count should equal n with zero
// summary inserts.
func TestSendBatchAlert_Chunking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		n           int
		wantPosts   int
		wantLast    int
		wantSentArg int
	}{
		{name: "empty batch", n: 0, wantPosts: 0, wantLast: 0, wantSentArg: 0},
		{name: "single alert", n: 1, wantPosts: 1, wantLast: 1, wantSentArg: 1},
		{name: "exactly one chunk", n: 10, wantPosts: 1, wantLast: 10, wantSentArg: 10},
		{name: "two chunks with remainder", n: 15, wantPosts: 2, wantLast: 5, wantSentArg: 15},
		{name: "two full chunks", n: 20, wantPosts: 2, wantLast: 10, wantSentArg: 20},
		{name: "deep overflow", n: 100, wantPosts: 10, wantLast: 10, wantSentArg: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var posts []discordWebhookPayload
			var mu sync.Mutex
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var p discordWebhookPayload
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&p))
				mu.Lock()
				posts = append(posts, p)
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			alerts := make([]AlertPayload, tt.n)
			for i := range alerts {
				alerts[i] = testAlert(85)
			}

			d := NewDiscordNotifier(srv.URL)
			sent, err := d.SendBatchAlert(context.Background(), alerts, "DDR4 Watch")
			require.NoError(t, err)
			assert.Equal(t, tt.wantSentArg, sent)

			require.Len(t, posts, tt.wantPosts)
			total := 0
			for i, p := range posts {
				assert.LessOrEqual(t, len(p.Embeds), maxEmbedsPerMessage,
					"chunk %d must not exceed Discord's %d-embed cap", i+1, maxEmbedsPerMessage)
				total += len(p.Embeds)
			}
			if tt.wantPosts > 0 {
				assert.Len(t, posts[tt.wantPosts-1].Embeds, tt.wantLast)
			}
			assert.Equal(t, tt.n, total)
		})
	}
}

// TestChunkAlerts is a pure helper test; covers boundary cases
// without touching HTTP at all.
func TestChunkAlerts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		n         int
		size      int
		wantCount int
		wantLast  int
	}{
		{name: "empty", n: 0, size: 10, wantCount: 0},
		{name: "single", n: 1, size: 10, wantCount: 1, wantLast: 1},
		{name: "exactly one chunk", n: 10, size: 10, wantCount: 1, wantLast: 10},
		{name: "remainder", n: 11, size: 10, wantCount: 2, wantLast: 1},
		{name: "two full chunks", n: 21, size: 10, wantCount: 3, wantLast: 1},
		{name: "100 alerts", n: 100, size: 10, wantCount: 10, wantLast: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			alerts := make([]AlertPayload, tt.n)
			chunks := chunkAlerts(alerts, tt.size)
			assert.Len(t, chunks, tt.wantCount)
			if tt.wantCount > 0 {
				assert.Len(t, chunks[len(chunks)-1], tt.wantLast)
			}
		})
	}
}

// TestSendBatchAlert_RateLimitWait exercises the bucket-driven sleep.
// Server's first response carries Remaining=0 and Reset-After=0.05 so
// the second POST must arrive at least 50ms after the first.
func TestSendBatchAlert_RateLimitWait(t *testing.T) {
	t.Parallel()

	var times []time.Time
	var mu sync.Mutex
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		posts++
		current := posts
		mu.Unlock()
		// Drain the body to keep keep-alive happy.
		_, _ = io.Copy(io.Discard, r.Body)

		if current == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset-After", "0.05")
		} else {
			w.Header().Set("X-RateLimit-Remaining", "5")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	alerts := make([]AlertPayload, 11)
	for i := range alerts {
		alerts[i] = testAlert(85)
	}

	d := NewDiscordNotifier(srv.URL)
	sent, err := d.SendBatchAlert(context.Background(), alerts, "DDR4 Watch")
	require.NoError(t, err)
	assert.Equal(t, 11, sent)

	require.Len(t, times, 2)
	gap := times[1].Sub(times[0])
	assert.GreaterOrEqual(t, gap, 45*time.Millisecond,
		"second POST must wait for bucket reset; got %s", gap)
}

// TestSendBatchAlert_429Retry verifies a non-global 429 with a usable
// Retry-After triggers a single retry that succeeds.
func TestSendBatchAlert_429Retry(t *testing.T) {
	t.Parallel()

	var posts int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		posts++
		current := posts
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)

		if current == 1 {
			w.Header().Set("Retry-After", "0.02")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	alerts := []AlertPayload{testAlert(85)}
	d := NewDiscordNotifier(srv.URL)
	sent, err := d.SendBatchAlert(context.Background(), alerts, "DDR4 Watch")
	require.NoError(t, err)
	assert.Equal(t, 1, sent)
	assert.Equal(t, 2, posts, "expected one 429 + one retry success")
}

// TestSendBatchAlert_429Global verifies a global 429 short-circuits
// without retrying and reports zero alerts sent.
func TestSendBatchAlert_429Global(t *testing.T) {
	t.Parallel()

	var posts int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		posts++
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("X-RateLimit-Global", "true")
		w.Header().Set("Retry-After", "0.02")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	alerts := []AlertPayload{testAlert(85)}
	d := NewDiscordNotifier(srv.URL)
	sent, err := d.SendBatchAlert(context.Background(), alerts, "DDR4 Watch")
	require.Error(t, err)
	assert.Equal(t, 0, sent)
	assert.Equal(t, 1, posts, "global 429 must not retry locally")
}

// TestSendBatchAlert_InterChunkDelay verifies WithInterChunkDelay
// inserts the requested gap even when the bucket has capacity.
func TestSendBatchAlert_InterChunkDelay(t *testing.T) {
	t.Parallel()

	var times []time.Time
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("X-RateLimit-Remaining", "5")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	alerts := make([]AlertPayload, 11)
	for i := range alerts {
		alerts[i] = testAlert(85)
	}

	d := NewDiscordNotifier(srv.URL, WithInterChunkDelay(25*time.Millisecond))
	sent, err := d.SendBatchAlert(context.Background(), alerts, "DDR4 Watch")
	require.NoError(t, err)
	assert.Equal(t, 11, sent)

	require.Len(t, times, 2)
	gap := times[1].Sub(times[0])
	assert.GreaterOrEqual(t, gap, 22*time.Millisecond,
		"inter-chunk delay must apply between chunks; got %s", gap)
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
