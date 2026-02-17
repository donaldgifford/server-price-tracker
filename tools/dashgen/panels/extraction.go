package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// ExtractionDuration returns a timeseries panel showing p50 and p95 LLM
// extraction latencies.
func ExtractionDuration() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Extraction Duration").
		Description("LLM extraction call duration percentiles").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(TSWidth).
		WithTarget(PromQuery(
			`histogram_quantile(0.50, sum(rate(spt_extraction_duration_seconds_bucket{job="server-price-tracker"}[5m])) by (le))`,
			"p50",
			"A",
		)).
		WithTarget(PromQuery(
			`histogram_quantile(0.95, sum(rate(spt_extraction_duration_seconds_bucket{job="server-price-tracker"}[5m])) by (le))`,
			"p95",
			"B",
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

// ExtractionFailures returns a timeseries panel showing the extraction
// failure rate.
func ExtractionFailures() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Extraction Failures").
		Description("LLM extraction failure rate per second").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(TSWidth).
		WithTarget(PromQuery(`spt:extraction_failures:rate5m`, "failures/s", "A")).
		FillOpacity(10).
		LineWidth(2).
		Thresholds(ThresholdsGreenYellowRed(0.01, 0.1)).
		ColorScheme(ColorSchemeThresholds()).
		DrawStyle(common.GraphDrawStyleLine)
}
