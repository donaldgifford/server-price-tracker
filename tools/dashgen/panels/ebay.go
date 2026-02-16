package panels

import (
	"fmt"

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

// DailyUsage returns a timeseries panel showing the rolling 24h eBay API
// usage with a threshold line at the daily limit.
func DailyUsage() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Daily Usage vs Limit").
		Description(fmt.Sprintf("Rolling 24h eBay API call count (limit: %d)", EbayDailyLimit)).
		Datasource(DSRef()).
		Height(TSHeight).
		Span(8).
		WithTarget(PromQuery(`spt_ebay_daily_usage{job="server-price-tracker"}`, "usage", "A")).
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenYellowRed(float64(EbayDailyLimit)*0.8, float64(EbayDailyLimit))).
		ColorScheme(ColorSchemeThresholds()).
		DrawStyle(common.GraphDrawStyleLine)
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
