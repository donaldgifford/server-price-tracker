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
		WithTarget(PromQuery(`rate(spt_alerts_fired_total{job="server-price-tracker"}[5m])`, "alerts/s", "A")).
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenOnly()).
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
