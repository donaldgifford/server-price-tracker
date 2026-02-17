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

	NotificationFailuresTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "notification_failures_total",
		Help:      "Total number of notification send failures.",
	})
)
