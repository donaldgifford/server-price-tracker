package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// RequestRate returns a timeseries panel showing the HTTP request rate.
func RequestRate() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Request Rate").
		Description("HTTP requests per second").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(TSWidth).
		WithTarget(PromQuery(`spt:http_requests:rate5m`, "req/s", "A")).
		Unit("reqps").
		FillOpacity(10).
		LineWidth(2).
		Legend(TableLegend("mean", "max")).
		Tooltip(MultiTooltip()).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// LatencyPercentiles returns a timeseries panel showing p50, p95, and p99
// HTTP request latencies.
func LatencyPercentiles() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Latency Percentiles").
		Description("HTTP request duration percentiles").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(TSWidth).
		WithTarget(PromQuery(
			`histogram_quantile(0.50, sum(rate(spt_http_request_duration_seconds_bucket{job="server-price-tracker"}[5m])) by (le))`,
			"p50",
			"A",
		)).
		WithTarget(PromQuery(
			`histogram_quantile(0.95, sum(rate(spt_http_request_duration_seconds_bucket{job="server-price-tracker"}[5m])) by (le))`,
			"p95",
			"B",
		)).
		WithTarget(PromQuery(
			`histogram_quantile(0.99, sum(rate(spt_http_request_duration_seconds_bucket{job="server-price-tracker"}[5m])) by (le))`,
			"p99",
			"C",
		)).
		Unit("s").
		FillOpacity(10).
		LineWidth(2).
		Legend(TableLegend("mean", "max")).
		Tooltip(MultiTooltip()).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// ErrorRate returns a timeseries panel showing the HTTP 5xx error rate
// as a percentage.
func ErrorRate() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Error Rate %").
		Description("HTTP 5xx error rate as percentage of total requests").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(TSWidth).
		WithTarget(PromQuery(
			`spt:http_errors:rate5m / spt:http_requests:rate5m * 100`,
			"error %", "A",
		)).
		Unit("percent").
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenYellowRed(1, 5)).
		ColorScheme(ColorSchemeThresholds()).
		DrawStyle(common.GraphDrawStyleLine)
}
