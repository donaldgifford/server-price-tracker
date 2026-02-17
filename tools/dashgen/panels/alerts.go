package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// AlertsRate returns a timeseries panel showing the rate of alerts fired.
func AlertsRate() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Alerts Fired Rate").
		Description("Rate of alerts fired per second").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(TSWidth).
		WithTarget(PromQuery(`sum(rate(spt_alerts_fired_total{job="server-price-tracker"}[5m]))`, "alerts/s", "A")).
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// LastNotification returns a stat panel showing time since the last successful
// notification delivery.
func LastNotification() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Last Notification").
		Description("Time since last successful notification delivery").
		Datasource(DSRef()).
		Height(StatHeight).
		Span(StatWidth).
		WithTarget(PromQuery(
			`time() - spt_notification_last_success_timestamp{job="server-price-tracker"}`,
			"", "A",
		)).
		Unit("s").
		Thresholds(ThresholdsGreenYellowRed(3600, 86400)).
		ColorScheme(ColorSchemeThresholds()).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone)
}

// NotificationLatency returns a timeseries panel showing the p95 notification
// webhook latency.
func NotificationLatency() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Notification Latency (p95)").
		Description("95th percentile Discord webhook latency").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(TSWidth).
		WithTarget(PromQuery(
			`spt:notification_duration:p95_5m`,
			"p95", "A",
		)).
		Unit("s").
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenYellowRed(1, 5)).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// NotificationFailures returns a stat panel showing notification failures
// in the past 24 hours.
func NotificationFailures() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Notification Failures (24h)").
		Description("Failed alert notification deliveries in the last 24 hours").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(TSWidth).
		WithTarget(PromQuery(`increase(spt_notification_failures_total{job="server-price-tracker"}[24h])`, "", "A")).
		Thresholds(ThresholdsGreenYellowRed(1, 5)).
		ColorScheme(ColorSchemeThresholds()).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeArea)
}
