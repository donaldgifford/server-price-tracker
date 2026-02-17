package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// LastIngestion returns a stat panel showing time since the last successful ingestion.
func LastIngestion() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Last Ingestion").
		Description("Time since last successful ingestion cycle").
		Datasource(DSRef()).
		Height(StatHeight).
		Span(StatWidth).
		WithTarget(PromQuery(
			`time() - spt_ingestion_last_success_timestamp{job="server-price-tracker"}`,
			"", "A",
		)).
		Unit("s").
		Thresholds(ThresholdsGreenYellowRed(1800, 3600)).
		ColorScheme(ColorSchemeThresholds()).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone)
}

// NextIngestion returns a stat panel showing time until the next scheduled ingestion.
func NextIngestion() *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title("Next Ingestion").
		Description("Time until next scheduled ingestion cycle").
		Datasource(DSRef()).
		Height(StatHeight).
		Span(StatWidth).
		WithTarget(PromQuery(
			`spt_scheduler_next_ingestion_timestamp{job="server-price-tracker"} - time()`,
			"", "A",
		)).
		Unit("s").
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemeThresholds()).
		ColorMode(common.BigValueColorModeBackground).
		GraphMode(common.BigValueGraphModeNone)
}

// ListingsRate returns a timeseries panel showing ingested listings per minute.
func ListingsRate() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Listings / min").
		Description("Rate of listings ingested per minute").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(8).
		WithTarget(PromQuery(`spt:ingestion_listings:rate5m * 60`, "listings/min", "A")).
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// IngestionErrors returns a timeseries panel showing ingestion errors per minute.
func IngestionErrors() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Errors / min").
		Description("Rate of ingestion errors per minute").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(8).
		WithTarget(PromQuery(`spt:ingestion_errors:rate5m * 60`, "errors/min", "A")).
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenYellowRed(0.1, 1)).
		ColorScheme(ColorSchemeThresholds()).
		DrawStyle(common.GraphDrawStyleLine)
}

// CycleDuration returns a timeseries panel showing the p95 ingestion cycle
// duration.
func CycleDuration() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Cycle Duration (p95)").
		Description("95th percentile ingestion cycle duration").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(8).
		WithTarget(PromQuery(
			`histogram_quantile(0.95, sum(rate(spt_ingestion_duration_seconds_bucket{job="server-price-tracker"}[5m])) by (le))`,
			"p95",
			"A",
		)).
		Unit("s").
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}
