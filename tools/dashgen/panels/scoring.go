package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/bargauge"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// BaselineCoverage returns a stat panel showing the percentage of product keys
// with warm baselines.
func BaselineCoverage() *stat.PanelBuilder {
	expr := `max(spt_baselines_warm{job="server-price-tracker"}) / (max(spt_baselines_warm{job="server-price-tracker"}) + max(spt_baselines_cold{job="server-price-tracker"}) + max(spt_product_keys_no_baseline{job="server-price-tracker"})) * 100`
	return stat.NewPanelBuilder().
		Title("Baseline Coverage").
		Description("Percentage of product keys with warm baselines (>= 10 samples)").
		Datasource(DSRef()).
		Height(StatHeight).
		Span(8).
		WithTarget(PromQuery(expr, "", "A")).
		Unit("percent").
		Thresholds(ThresholdsRedGreen(50)).
		ColorScheme(ColorSchemeThresholds()).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone)
}

// BaselineMaturity returns a stat panel showing warm, cold, and no-baseline
// product key counts.
func BaselineMaturity() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Baseline Maturity").
		Description("Breakdown of baseline maturity across product keys").
		Datasource(DSRef()).
		Height(StatHeight).
		Span(8).
		WithTarget(PromQuery(`max(spt_baselines_warm{job="server-price-tracker"})`, "warm", "A")).
		WithTarget(PromQuery(`max(spt_baselines_cold{job="server-price-tracker"})`, "cold", "B")).
		WithTarget(PromQuery(`max(spt_product_keys_no_baseline{job="server-price-tracker"})`, "no baseline", "C")).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		GraphMode(common.BigValueGraphModeNone)
}

// ColdStartRate returns a timeseries panel showing the percentage of scorings
// that used cold start (no baseline).
func ColdStartRate() *timeseries.PanelBuilder {
	expr := `sum(rate(spt_scoring_cold_start_total{job="server-price-tracker"}[5m])) / (sum(rate(spt_scoring_cold_start_total{job="server-price-tracker"}[5m])) + sum(rate(spt_scoring_with_baseline_total{job="server-price-tracker"}[5m]))) * 100`
	return timeseries.NewPanelBuilder().
		Title("Cold Start Rate").
		Description("Percentage of listings scored without a warm baseline").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(8).
		WithTarget(PromQuery(expr, "cold start %", "A")).
		Unit("percent").
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// ScoreDistribution returns a bar gauge panel showing the distribution of
// computed listing scores across histogram buckets.
func ScoreDistribution() *bargauge.PanelBuilder {
	return bargauge.NewPanelBuilder().
		Title("Score Distribution").
		Description("Distribution of listing scores (0-100)").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(FullWidth).
		WithTarget(PromQuery(
			`sum(increase(spt_scoring_distribution_bucket{job="server-price-tracker"}[1h])) by (le)`,
			"{{le}}", "A",
		)).
		Orientation(common.VizOrientationHorizontal).
		Min(0).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic())
}
