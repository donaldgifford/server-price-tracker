package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
)

const (
	colorGreen  = 0x2ECC71 // score 90+
	colorYellow = 0xF1C40F // score 80-89
	colorOrange = 0xE67E22 // score 75-79

	// maxEmbedsPerMessage is Discord's hard cap on embeds per webhook message.
	maxEmbedsPerMessage = 10
)

// DiscordNotifier implements Notifier via Discord webhook.
type DiscordNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewDiscordNotifier creates a new DiscordNotifier.
func NewDiscordNotifier(webhookURL string, opts ...DiscordOption) *DiscordNotifier {
	d := &DiscordNotifier{
		webhookURL: webhookURL,
		client:     http.DefaultClient,
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

// SendBatchAlert sends multiple alerts as a single Discord message.
//
// Discord caps embeds at maxEmbedsPerMessage per webhook payload. When the
// caller passes more alerts than the cap, we reserve one slot for the summary
// embed so the total stays at the cap (e.g., 9 alerts + 1 summary = 10).
func (d *DiscordNotifier) SendBatchAlert(
	ctx context.Context,
	alerts []AlertPayload,
	watchName string,
) error {
	embeds := make([]discordEmbed, 0, maxEmbedsPerMessage)

	alertCap := len(alerts)
	if alertCap > maxEmbedsPerMessage {
		alertCap = maxEmbedsPerMessage - 1
	}

	for i := range alertCap {
		embeds = append(embeds, buildEmbed(&alerts[i]))
	}

	if len(alerts) > maxEmbedsPerMessage {
		embeds = append(embeds, discordEmbed{
			Title:       fmt.Sprintf("... and %d more alerts for %s", len(alerts)-alertCap, watchName),
			Color:       colorYellow,
			Description: "Check the dashboard for the full list.",
		})
	}

	payload := discordWebhookPayload{Embeds: embeds}
	return d.post(ctx, payload)
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

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		d.webhookURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("creating discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := d.client.Do(req)
	metrics.NotificationDuration.Observe(time.Since(start).Seconds())

	if err != nil {
		return fmt.Errorf("sending discord webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("discord rate limited (429)")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("discord returned %d (body unreadable)", resp.StatusCode)
		}
		return fmt.Errorf("discord returned %d: %s", resp.StatusCode, respBody)
	}

	return nil
}
