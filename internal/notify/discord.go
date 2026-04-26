package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
)

const (
	colorGreen  = 0x2ECC71 // score 90+
	colorYellow = 0xF1C40F // score 80-89
	colorOrange = 0xE67E22 // score 75-79

	// maxEmbedsPerMessage is Discord's hard cap on embeds per webhook message.
	maxEmbedsPerMessage = 10

	// max429Attempts bounds the per-chunk retry loop on 429 responses
	// — one initial post + one retry after honoring Retry-After.
	max429Attempts = 2
)

// errGlobal429 marks a 429 response carrying X-RateLimit-Global=true.
// Global limits apply across the entire webhook; we don't retry locally
// — surface the error so the caller can decide whether to back off
// further (e.g., trip a circuit breaker upstream).
var errGlobal429 = errors.New("discord: global rate limit hit")

// DiscordNotifier implements Notifier via Discord webhook.
type DiscordNotifier struct {
	webhookURL      string
	client          *http.Client
	rateLimit       *rateLimitState
	interChunkDelay time.Duration
}

// NewDiscordNotifier creates a new DiscordNotifier.
func NewDiscordNotifier(webhookURL string, opts ...DiscordOption) *DiscordNotifier {
	d := &DiscordNotifier{
		webhookURL: webhookURL,
		client:     http.DefaultClient,
		rateLimit:  newRateLimitState(),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// DiscordOption configures a DiscordNotifier.
type DiscordOption func(*DiscordNotifier)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) DiscordOption {
	return func(d *DiscordNotifier) {
		d.client = c
	}
}

// WithInterChunkDelay sets a defensive sleep between chunks beyond what
// the bucket headers require. Default is 0 (no extra delay).
func WithInterChunkDelay(delay time.Duration) DiscordOption {
	return func(d *DiscordNotifier) {
		d.interChunkDelay = delay
	}
}

// discordWebhookPayload is the Discord webhook JSON structure.
type discordWebhookPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string              `json:"title"`
	URL         string              `json:"url,omitempty"`
	Color       int                 `json:"color"`
	Description string              `json:"description,omitempty"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
	Thumbnail   *discordThumbnail   `json:"thumbnail,omitempty"`
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordThumbnail struct {
	URL string `json:"url"`
}

// SendAlert sends a single alert as a Discord embed.
func (d *DiscordNotifier) SendAlert(ctx context.Context, alert *AlertPayload) error {
	embed := buildEmbed(alert)
	payload := discordWebhookPayload{
		Embeds: []discordEmbed{embed},
	}
	return d.post(ctx, payload)
}

// SendBatchAlert chunks alerts into Discord-compliant batches (<=10
// embeds each), waits for the bucket to refill between chunks, and
// returns the count of alerts that landed plus the first error
// encountered (if any). Partial success is the common case during
// 429s — the engine uses `sent` to mark only the delivered IDs as
// notified.
func (d *DiscordNotifier) SendBatchAlert(
	ctx context.Context,
	alerts []AlertPayload,
	_ string,
) (int, error) {
	chunks := chunkAlerts(alerts, maxEmbedsPerMessage)
	sent := 0
	for i, chunk := range chunks {
		if _, err := d.rateLimit.waitForBucket(ctx); err != nil {
			return sent, fmt.Errorf("waiting for discord bucket on chunk %d/%d: %w",
				i+1, len(chunks), err)
		}
		if i > 0 && d.interChunkDelay > 0 {
			if err := sleepCtx(ctx, d.interChunkDelay); err != nil {
				return sent, fmt.Errorf("inter-chunk delay on chunk %d/%d: %w",
					i+1, len(chunks), err)
			}
		}

		embeds := make([]discordEmbed, 0, len(chunk))
		for j := range chunk {
			embeds = append(embeds, buildEmbed(&chunk[j]))
		}

		if err := d.post(ctx, discordWebhookPayload{Embeds: embeds}); err != nil {
			return sent, fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), err)
		}
		sent += len(chunk)
		metrics.DiscordChunksSentTotal.Inc()
	}
	return sent, nil
}

func buildEmbed(alert *AlertPayload) discordEmbed {
	embed := discordEmbed{
		Title: fmt.Sprintf("Deal Alert: %s", alert.ListingTitle),
		URL:   alert.EbayURL,
		Color: scoreColor(alert.Score),
		Fields: []discordEmbedField{
			{Name: "Score", Value: fmt.Sprintf("%d/100", alert.Score), Inline: true},
			{Name: "Price", Value: alert.Price, Inline: true},
			{Name: "Unit Price", Value: alert.UnitPrice, Inline: true},
			{Name: "Seller", Value: alert.Seller, Inline: true},
			{Name: "Condition", Value: alert.Condition, Inline: true},
			{Name: "Type", Value: alert.ComponentType, Inline: true},
		},
	}

	if alert.ImageURL != "" {
		embed.Thumbnail = &discordThumbnail{URL: alert.ImageURL}
	}

	return embed
}

func scoreColor(score int) int {
	switch {
	case score >= 90:
		return colorGreen
	case score >= 80:
		return colorYellow
	default:
		return colorOrange
	}
}

func (d *DiscordNotifier) post(ctx context.Context, payload discordWebhookPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling discord payload: %w", err)
	}

	for attempt := 1; attempt <= max429Attempts; attempt++ {
		retry, postErr := d.postOnce(ctx, body, attempt)
		if postErr == nil {
			return nil
		}
		if !retry {
			return postErr
		}
	}
	return fmt.Errorf("discord: rate limited after %d attempts", max429Attempts)
}

// postOnce executes one HTTP POST. The boolean return signals whether a
// retry is appropriate (true on a non-global 429 with a fresh
// Retry-After window). Any other error path returns retry=false.
func (d *DiscordNotifier) postOnce(ctx context.Context, body []byte, attempt int) (bool, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		d.webhookURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return false, fmt.Errorf("creating discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := d.client.Do(req)
	metrics.NotificationDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		return false, fmt.Errorf("sending discord webhook: %w", err)
	}
	defer resp.Body.Close()

	d.rateLimit.update(resp)

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return d.handle429(ctx, resp, attempt)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return false, fmt.Errorf("discord returned %d (body unreadable)", resp.StatusCode)
		}
		return false, fmt.Errorf("discord returned %d: %s", resp.StatusCode, respBody)
	}
	return false, nil
}

func (*DiscordNotifier) handle429(ctx context.Context, resp *http.Response, attempt int) (bool, error) {
	global := resp.Header.Get("X-RateLimit-Global") == "true"
	metrics.Discord429Total.WithLabelValues(strconv.FormatBool(global)).Inc()
	if global {
		return false, errGlobal429
	}
	if attempt >= max429Attempts {
		return false, fmt.Errorf("discord rate limited (429), retries exhausted")
	}

	wait := parseRetryAfter(resp)
	if wait <= 0 {
		return false, fmt.Errorf("discord rate limited (429) with no usable Retry-After")
	}
	if err := sleepCtx(ctx, wait); err != nil {
		return false, fmt.Errorf("waiting on discord 429 Retry-After: %w", err)
	}
	return true, fmt.Errorf("discord rate limited (429), retrying after %s", wait)
}

func parseRetryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(secs * float64(time.Second))
		}
	}
	if v := resp.Header.Get("X-RateLimit-Reset-After"); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return 0
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
