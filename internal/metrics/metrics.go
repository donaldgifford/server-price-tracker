// Package metrics defines Prometheus metrics for server-price-tracker.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "spt"

// HTTP metrics.
var (
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "http_request_duration_seconds",
		Help:      "Duration of HTTP requests in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path", "status"})

	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests.",
	}, []string{"method", "path", "status"})
)

// Health metrics.
var (
	HealthzUp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "healthz_up",
		Help:      "Health check status (1 = ok, 0 = failing).",
	})

	ReadyzUp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "readyz_up",
		Help:      "Readiness check status (1 = ready, 0 = not ready).",
	})
)

// Ingestion metrics.
var (
	IngestionListingsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "ingestion_listings_total",
		Help:      "Total number of listings ingested.",
	})

	IngestionErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "ingestion_errors_total",
		Help:      "Total number of ingestion errors.",
	})

	IngestionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "ingestion_duration_seconds",
		Help:      "Duration of ingestion cycles in seconds.",
		Buckets:   prometheus.DefBuckets,
	})
)

// Extraction metrics.
var (
	ExtractionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "extraction_duration_seconds",
		Help:      "Duration of LLM extraction calls in seconds.",
		Buckets:   prometheus.DefBuckets,
	})

	ExtractionFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "extraction_failures_total",
		Help:      "Total number of extraction failures.",
	})
)

// LLM token metrics.
//
// Records billed tokens (input + output) for every successful Generate call,
// including calls whose response later failed parse or validation — the
// metric reflects what was billed, not what produced useful work. Use
// extraction_failures_total for the failed-call view.
var (
	ExtractionTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "extraction_tokens_total",
		Help: "Total LLM tokens billed by extraction calls, " +
			"labeled by backend, model, and direction (input/output). " +
			"Includes tokens from calls whose response failed parse or validation.",
	}, []string{"backend", "model", "direction"})

	ExtractionTokensPerRequest = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "extraction_tokens_per_request",
		Help:      "Distribution of total tokens (input+output) per LLM call, by backend and model.",
		Buckets:   []float64{50, 100, 250, 500, 1000, 2000, 5000, 10000, 20000},
	}, []string{"backend", "model"})
)

// Extraction quality metrics.
var (
	ListingsIncompleteExtraction = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "listings_incomplete_extraction",
		Help:      "Listings with incomplete extraction data (e.g., missing speed for RAM).",
	})

	ExtractionQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "extraction_queue_depth",
		Help:      "Number of pending extraction jobs in the queue.",
	})
)

// Scoring metrics.
var (
	ScoringDistribution = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "scoring_distribution",
		Help:      "Distribution of computed listing scores.",
		Buckets:   prometheus.LinearBuckets(0, 10, 11), // 0, 10, 20, ..., 100
	})

	ScoringWithBaselineTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "scoring_with_baseline_total",
		Help:      "Total listings scored with a warm baseline (>= MinBaselineSamples).",
	})

	ScoringColdStartTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "scoring_cold_start_total",
		Help:      "Total listings scored without a warm baseline (cold start neutral price).",
	})
)

// Baseline metrics.
var (
	BaselinesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "baselines_total",
		Help:      "Total number of price baselines.",
	})

	BaselinesCold = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "baselines_cold",
		Help:      "Baselines with fewer than MinBaselineSamples (cold start).",
	})

	BaselinesWarm = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "baselines_warm",
		Help:      "Baselines with >= MinBaselineSamples (sufficient for price scoring).",
	})

	ProductKeysNoBaseline = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "product_keys_no_baseline",
		Help:      "Distinct product keys in listings without any price baseline.",
	})
)

// eBay API metrics.
var (
	EbayAPICallsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "ebay_api_calls_total",
		Help:      "Total cumulative eBay API calls.",
	})

	EbayDailyLimitHits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "ebay_daily_limit_hits_total",
		Help:      "Total number of times the daily eBay API limit was reached.",
	})
)

// eBay rate limit metrics (from Analytics API).
var (
	EbayRateLimit = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "ebay_rate_limit",
		Help:      "Total eBay API calls allowed in the current quota window.",
	})

	EbayRateRemaining = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "ebay_rate_remaining",
		Help:      "eBay API calls remaining in the current quota window.",
	})

	EbayRateResetTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "ebay_rate_reset_timestamp",
		Help:      "Unix epoch seconds when the eBay API quota window resets.",
	})
)

// Alert metrics.
var (
	AlertsFiredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "alerts_fired_total",
		Help:      "Total number of alerts fired.",
	})

	AlertsFiredByWatch = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "alerts_fired_by_watch",
		Help:      "Alerts fired broken down by watch name. Cardinality bounded by number of watches (typically <20).",
	}, []string{"watch"})

	// AlertsCreatedTotal counts alert rows inserted by the engine, labeled
	// by component type. Distinct from AlertsFiredTotal (which increments
	// on successful Discord delivery) — this reflects the engine's
	// decision-to-alert independent of notifier outcomes, which is the
	// signal we want when measuring whether DESIGN-0011's noise reduction
	// actually moved the needle.
	AlertsCreatedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "alerts_created_total",
		Help: "Alerts inserted into the alerts table by the engine, " +
			"labeled by component type. Increments before notification " +
			"delivery so it reflects engine decisions, not Discord outcomes.",
	}, []string{"component_type"})

	NotificationFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "notification_failures_total",
		Help:      "Total number of notification send failures.",
	})
)

// Notification metrics.
var (
	NotificationDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "notification_duration_seconds",
		Help:      "Discord webhook HTTP POST latency in seconds.",
		Buckets:   []float64{0.1, 0.25, 0.5, 1, 2, 5, 10},
	})

	NotificationLastSuccessTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "notification_last_success_timestamp",
		Help:      "Unix epoch of the last successful notification delivery.",
	})

	NotificationLastFailureTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "notification_last_failure_timestamp",
		Help:      "Unix epoch of the last notification delivery failure.",
	})
)

// System state metrics.
var (
	WatchesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "watches_total",
		Help:      "Total number of watches.",
	})

	WatchesEnabled = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "watches_enabled",
		Help:      "Number of enabled watches.",
	})

	ListingsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "listings_total",
		Help:      "Total listings in the database.",
	})

	ListingsUnextracted = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "listings_unextracted",
		Help:      "Listings without LLM extraction.",
	})

	ListingsUnscored = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "listings_unscored",
		Help:      "Listings without a computed score.",
	})

	AlertsPending = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "alerts_pending",
		Help:      "Alerts not yet sent as notifications.",
	})
)

// Scheduler metrics.
var (
	IngestionLastSuccessTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "ingestion_last_success_timestamp",
		Help:      "Unix epoch of the last successful ingestion cycle.",
	})

	BaselineLastRefreshTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "baseline_last_refresh_timestamp",
		Help:      "Unix epoch of the last successful baseline refresh.",
	})
)

// Alert review UI metrics (DESIGN-0010 / IMPL-0015 Phase 4).
//
// Surfaces operator-visible activity (dismissals, manual retries) and
// query-shape health (latency, table size). Wired into a dashgen panel
// so we know when alert-list latency starts climbing and the
// pg_stat_statements follow-up needs prioritizing.
var (
	AlertsDismissedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "alerts_dismissed_total",
		Help:      "Total number of alerts dismissed via the alert review UI.",
	})

	AlertsQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "alerts_query_duration_seconds",
		Help:      "Latency of alert review store queries by operation.",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
	}, []string{"query"})

	AlertsTableRows = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "alerts_table_rows",
		Help:      "Total alerts matching the most recent list query (after filters).",
	})

	NotificationAttemptsInsertedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "notification_attempts_inserted_total",
		Help:      "notification_attempts rows inserted, labeled by outcome.",
	}, []string{"result"})
)

// Discord rate-limit metrics (DESIGN-0009 / IMPL-0015 Phase 5).
//
// Track the in-process bucket state derived from Discord's
// X-RateLimit-* response headers so chunked sends reflect upstream
// capacity rather than blindly hammering the webhook.
var (
	DiscordRateLimitRemaining = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "discord_rate_limit_remaining",
		Help:      "Last observed X-RateLimit-Remaining for the Discord webhook bucket.",
	})

	DiscordRateLimitWaitsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "discord_rate_limit_waits_total",
		Help:      "Number of times we slept before posting to wait out a Discord bucket reset.",
	})

	Discord429Total = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "discord_429_total",
		Help:      "Discord 429 responses by global flag.",
	}, []string{"global"})

	DiscordChunksSentTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "discord_chunks_sent_total",
		Help:      "Total chunks (one HTTP POST each) sent to Discord webhooks.",
	})
)
