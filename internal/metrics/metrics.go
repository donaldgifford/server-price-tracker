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
	SchedulerNextIngestionTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "scheduler_next_ingestion_timestamp",
		Help:      "Unix epoch of the next scheduled ingestion run.",
	})

	SchedulerNextBaselineTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "scheduler_next_baseline_timestamp",
		Help:      "Unix epoch of the next scheduled baseline refresh.",
	})

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
