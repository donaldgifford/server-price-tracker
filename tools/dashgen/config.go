package main

import "errors"

// KnownMetrics is the set of metric names exported by server-price-tracker
// plus recording rule names referenced in dashboards and alerts.
var KnownMetrics = map[string]bool{
	// HTTP metrics.
	"spt_http_request_duration_seconds": true,
	"spt_http_requests_total":           true,

	// Health metrics.
	"spt_healthz_up": true,
	"spt_readyz_up":  true,

	// Ingestion metrics.
	"spt_ingestion_listings_total":   true,
	"spt_ingestion_errors_total":     true,
	"spt_ingestion_duration_seconds": true,

	// Extraction metrics.
	"spt_extraction_duration_seconds": true,
	"spt_extraction_failures_total":   true,

	// Scoring metrics.
	"spt_scoring_distribution": true,

	// eBay API metrics.
	"spt_ebay_api_calls_total":        true,
	"spt_ebay_daily_limit_hits_total": true,

	// eBay rate limit metrics (from Analytics API).
	"spt_ebay_rate_limit":           true,
	"spt_ebay_rate_remaining":       true,
	"spt_ebay_rate_reset_timestamp": true,

	// Alert metrics.
	"spt_alerts_fired_total":          true,
	"spt_notification_failures_total": true,

	// Recording rules.
	"spt:http_requests:rate5m":       true,
	"spt:http_errors:rate5m":         true,
	"spt:ingestion_listings:rate5m":  true,
	"spt:ingestion_errors:rate5m":    true,
	"spt:extraction_failures:rate5m": true,
	"spt:ebay_api_calls:rate5m":      true,

	// Standard Prometheus metrics referenced in dashboards.
	"up":                         true,
	"process_start_time_seconds": true,
}

// Config controls which artifacts the generator produces and where they go.
type Config struct {
	OutputDir        string
	DashboardEnabled bool
	RulesEnabled     bool
}

// DefaultConfig returns a Config that generates all artifacts into ../../deploy
// (relative to tools/dashgen/).
func DefaultConfig() Config {
	return Config{
		OutputDir:        "../../deploy",
		DashboardEnabled: true,
		RulesEnabled:     true,
	}
}

// Validate checks that the config is usable.
func (c Config) Validate() error {
	if c.OutputDir == "" {
		return errors.New("output directory must be set")
	}
	if !c.DashboardEnabled && !c.RulesEnabled {
		return errors.New("at least one of dashboard or rules must be enabled")
	}
	return nil
}
