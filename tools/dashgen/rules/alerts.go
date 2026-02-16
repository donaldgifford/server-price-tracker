package rules

// AlertRules returns a PrometheusRule CR containing alert rules for
// server-price-tracker operational monitoring.
func AlertRules() PrometheusRule {
	return PrometheusRule{
		APIVersion: "monitoring.coreos.com/v1",
		Kind:       "PrometheusRule",
		Metadata: PrometheusRuleMetadata{
			Name: "spt-alerts",
			Labels: map[string]string{
				"prometheus": "system-rules-prometheus",
			},
		},
		Spec: PrometheusRuleSpec{
			Groups: []RuleGroup{
				{
					Name: "spt-alerts",
					Rules: []Rule{
						{
							Alert: "SptDown",
							Expr:  `absent(up{job="server-price-tracker"})`,
							For:   "2m",
							Labels: map[string]string{
								"severity": "critical",
							},
							Annotations: map[string]string{
								"summary":     "Server Price Tracker is down",
								"description": "The server-price-tracker job has been absent for more than 2 minutes.",
							},
						},
						{
							Alert: "SptReadinessDown",
							Expr:  `spt_readyz_up == 0`,
							For:   "2m",
							Labels: map[string]string{
								"severity": "critical",
							},
							Annotations: map[string]string{
								"summary":     "Server Price Tracker readiness check is failing",
								"description": "The readiness probe has been reporting not-ready for more than 2 minutes.",
							},
						},
						{
							Alert: "SptHighErrorRate",
							Expr:  `spt:http_errors:rate5m / spt:http_requests:rate5m > 0.05`,
							For:   "5m",
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "High HTTP error rate on Server Price Tracker",
								"description": "More than 5% of HTTP requests are returning 5xx errors over the last 5 minutes.",
							},
						},
						{
							Alert: "SptIngestionErrors",
							Expr:  `spt:ingestion_errors:rate5m > 0`,
							For:   "5m",
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Ingestion errors detected",
								"description": "The ingestion pipeline has been producing errors for more than 5 minutes.",
							},
						},
						{
							Alert: "SptExtractionFailures",
							Expr:  `spt:extraction_failures:rate5m > 0.1`,
							For:   "5m",
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "LLM extraction failure rate is elevated",
								"description": "Extraction failures are occurring at more than 0.1/s for the last 5 minutes.",
							},
						},
						{
							Alert: "SptEbayQuotaHigh",
							Expr:  `spt_ebay_daily_usage > 4000`,
							For:   "5m",
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "eBay API daily usage is above 80% of the quota",
								"description": "Daily eBay API usage has exceeded 4000 calls (limit is 5000).",
							},
						},
						{
							Alert: "SptEbayLimitReached",
							Expr:  `increase(spt_ebay_daily_limit_hits_total[5m]) > 0`,
							For:   "0m",
							Labels: map[string]string{
								"severity": "critical",
							},
							Annotations: map[string]string{
								"summary":     "eBay API daily limit has been reached",
								"description": "The eBay Browse API daily quota has been exhausted. Ingestion is paused until reset.",
							},
						},
						{
							Alert: "SptNotificationFailures",
							Expr:  `increase(spt_notification_failures_total[5m]) > 0`,
							For:   "1m",
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Notification delivery failures detected",
								"description": "One or more alert notifications (Discord webhooks) have failed to send.",
							},
						},
					},
				},
			},
		},
	}
}
