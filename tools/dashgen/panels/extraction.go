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

// ExtractionTokenRate returns a timeseries panel showing input vs output
// token rates per backend/model.
func ExtractionTokenRate() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Extraction Token Rate").
		Description("LLM token rate (input vs output) by backend and model").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(TSWidth).
		WithTarget(PromQuery(
			`spt:extraction_tokens_input:rate5m`,
			"{{backend}}/{{model}} input",
			"A",
		)).
		WithTarget(PromQuery(
			`spt:extraction_tokens_output:rate5m`,
			"{{backend}}/{{model}} output",
			"B",
		)).
		Unit("tokens/s").
		FillOpacity(20).
		LineWidth(2).
		Legend(TableLegend("mean", "max", "lastNotNull")).
		Tooltip(MultiTooltip()).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// ExtractionTokensTotal returns a timeseries panel showing the cumulative
// token consumption per backend and model.
func ExtractionTokensTotal() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Extraction Tokens (cumulative)").
		Description("Total LLM tokens consumed since process start, by backend and model").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(TSWidth).
		WithTarget(PromQuery(
			`sum by (backend, model, direction) (spt_extraction_tokens_total)`,
			"{{backend}}/{{model}} {{direction}}",
			"A",
		)).
		Unit("short").
		FillOpacity(10).
		LineWidth(2).
		Legend(TableLegend("lastNotNull", "max")).
		Tooltip(MultiTooltip()).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// ExtractionTokensPerRequest returns a timeseries panel showing the p50
// and p95 of total tokens per LLM request.
func ExtractionTokensPerRequest() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Tokens per Request").
		Description("Distribution of total tokens per extraction request (p50, p95)").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(TSWidth).
		WithTarget(PromQuery(
			`histogram_quantile(0.50, sum by (backend, model, le) (rate(spt_extraction_tokens_per_request_bucket[5m])))`,
			"{{backend}}/{{model}} p50",
			"A",
		)).
		WithTarget(PromQuery(
			`spt:extraction_tokens_per_request:p95`,
			"{{backend}}/{{model}} p95",
			"B",
		)).
		Unit("short").
		FillOpacity(10).
		LineWidth(2).
		Legend(TableLegend("mean", "max", "lastNotNull")).
		Tooltip(MultiTooltip()).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}
