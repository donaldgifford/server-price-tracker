package rules

// RecordingRules returns a PrometheusRule CR containing pre-computed rate
// expressions used by dashboards and alert rules.
func RecordingRules() PrometheusRule {
	return PrometheusRule{
		APIVersion: "monitoring.coreos.com/v1",
		Kind:       "PrometheusRule",
		Metadata: PrometheusRuleMetadata{
			Name: "spt-recording-rules",
			Labels: map[string]string{
				"prometheus": "system-rules-prometheus",
			},
		},
		Spec: PrometheusRuleSpec{
			Groups: []RuleGroup{
				{
					Name: "spt-recording",
					Rules: []Rule{
						{
							Record: "spt:http_requests:rate5m",
							Expr:   `sum(rate(spt_http_requests_total[5m]))`,
						},
						{
							Record: "spt:http_errors:rate5m",
							Expr:   `sum(rate(spt_http_requests_total{status=~"5.."}[5m]))`,
						},
						{
							Record: "spt:ingestion_listings:rate5m",
							Expr:   `sum(rate(spt_ingestion_listings_total[5m]))`,
						},
						{
							Record: "spt:ingestion_errors:rate5m",
							Expr:   `sum(rate(spt_ingestion_errors_total[5m]))`,
						},
						{
							Record: "spt:extraction_failures:rate5m",
							Expr:   `sum(rate(spt_extraction_failures_total[5m]))`,
						},
						{
							Record: "spt:ebay_api_calls:rate5m",
							Expr:   `sum(rate(spt_ebay_api_calls_total[5m]))`,
						},
						{
							Record: "spt:notification_duration:p95_5m",
							Expr:   `histogram_quantile(0.95, sum(rate(spt_notification_duration_seconds_bucket[5m])) by (le))`,
						},
					},
				},
			},
		},
	}
}
