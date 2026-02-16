package panels

import (
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/gauge"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
)

// HealthzStat returns a stat panel showing the health check status.
func HealthzStat() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Healthz").
		Description("Health check status (1 = ok, 0 = failing)").
		Datasource(DSRef()).
		Height(StatHeight).
		Span(StatWidth).
		WithTarget(PromQuery(`spt_healthz_up`, "", "A")).
		Thresholds(ThresholdsRedGreen(1)).
		ColorScheme(ColorSchemeThresholds()).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone).
		TextMode(common.BigValueTextModeValue)
}

// ReadyzStat returns a stat panel showing the readiness check status.
func ReadyzStat() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Readyz").
		Description("Readiness check status (1 = ready, 0 = not ready)").
		Datasource(DSRef()).
		Height(StatHeight).
		Span(StatWidth).
		WithTarget(PromQuery(`spt_readyz_up`, "", "A")).
		Thresholds(ThresholdsRedGreen(1)).
		ColorScheme(ColorSchemeThresholds()).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone).
		TextMode(common.BigValueTextModeValue)
}

// QuotaGauge returns a gauge panel showing eBay API daily usage as a
// percentage of the limit.
func QuotaGauge() *gauge.PanelBuilder {
	expr := fmt.Sprintf("spt_ebay_daily_usage / %d * 100", EbayDailyLimit)
	return gauge.NewPanelBuilder().
		Title("eBay Quota %").
		Description("Daily eBay API usage as percentage of limit").
		Datasource(DSRef()).
		Height(StatHeight).
		Span(StatWidth).
		WithTarget(PromQuery(expr, "", "A")).
		Unit("percent").
		Min(0).
		Max(100).
		Thresholds(ThresholdsGreenYellowRed(80, 95)).
		ColorScheme(ColorSchemeThresholds())
}

// UptimeStat returns a stat panel showing process uptime.
func UptimeStat() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Uptime").
		Description("Time since process start").
		Datasource(DSRef()).
		Height(StatHeight).
		Span(StatWidth).
		WithTarget(PromQuery(
			`time() - process_start_time_seconds{job="server-price-tracker"}`,
			"", "A",
		)).
		Unit("s").
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemeThresholds()).
		GraphMode(common.BigValueGraphModeNone)
}
