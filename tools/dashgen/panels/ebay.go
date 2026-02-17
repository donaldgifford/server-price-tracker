package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// APICallsRate returns a timeseries panel showing the eBay API call rate.
func APICallsRate() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("API Calls Rate").
		Description("eBay Browse API calls per second").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(8).
		WithTarget(PromQuery(`spt:ebay_api_calls:rate5m`, "calls/s", "A")).
		Unit("reqps").
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// DailyUsage returns a timeseries panel showing eBay API usage vs limit,
// derived from the Analytics API metrics.
func DailyUsage() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Daily Usage vs Limit").
		Description("eBay API call count and limit (from Analytics API)").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(8).
		WithTarget(PromQuery(
			`spt_ebay_rate_limit{job="server-price-tracker"} - spt_ebay_rate_remaining{job="server-price-tracker"}`,
			"usage", "A",
		)).
		WithTarget(PromQuery(
			`spt_ebay_rate_limit{job="server-price-tracker"}`, "limit", "B",
		)).
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// ResetCountdown returns a stat panel showing the time until the eBay API
// quota window resets.
func ResetCountdown() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Quota Reset In").
		Description("Time until eBay API quota window resets").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(8).
		WithTarget(PromQuery(`spt_ebay_rate_reset_timestamp{job="server-price-tracker"} - time()`, "", "A")).
		Unit("s").
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemeThresholds()).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone)
}

// LimitHits returns a stat panel showing the number of daily limit hits
// in the past 24 hours.
func LimitHits() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Limit Hits (24h)").
		Description("Times the eBay daily limit was reached in the last 24 hours").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(8).
		WithTarget(PromQuery(`increase(spt_ebay_daily_limit_hits_total{job="server-price-tracker"}[24h])`, "", "A")).
		Thresholds(ThresholdsGreenYellowRed(1, 3)).
		ColorScheme(ColorSchemeThresholds()).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeArea)
}
