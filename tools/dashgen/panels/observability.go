package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/heatmap"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

// JudgeScoreDistribution returns a heatmap of judge quality scores over
// time, partitioned by component_type. Buckets come from the
// `spt_judge_score` histogram defined in `internal/metrics/metrics.go`.
func JudgeScoreDistribution() *heatmap.PanelBuilder {
	return heatmap.NewPanelBuilder().
		Title("Judge Score Distribution").
		Description("Distribution of LLM-as-judge quality scores by component type (heatmap of spt_judge_score histogram buckets)").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(FullWidth).
		WithTarget(PromQuery(
			`sum by (le, component_type) (increase(spt_judge_score_bucket{job="server-price-tracker"}[5m]))`,
			"{{component_type}}",
			"A",
		))
}

// JudgeVsOperatorAgreement overlays the rate of judge "noise" verdicts
// (score < 0.3) against the rate of operator dismissals. If the two
// curves track, the judge is matching operator intuition; divergence is
// the signal to refresh examples.json or relabel the dataset.
func JudgeVsOperatorAgreement() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Judge vs Operator Agreement").
		Description("Overlay of judge 'noise' verdict rate vs operator dismissal rate — divergence indicates judge drift").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(FullWidth).
		WithTarget(PromQuery(
			`sum(rate(spt_judge_evaluations_total{job="server-price-tracker",verdict="noise"}[5m]))`,
			"judge noise",
			"A",
		)).
		WithTarget(PromQuery(
			`sum(rate(spt_alerts_dismissed_total{job="server-price-tracker"}[5m]))`,
			"operator dismissals",
			"B",
		)).
		Unit("ops").
		FillOpacity(15).
		LineWidth(2).
		Legend(TableLegend("mean", "max", "lastNotNull")).
		Tooltip(MultiTooltip()).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// JudgeCostByModel charts cumulative judge spend per model. This is the
// closest in-tree analog to "LangfuseGenerationCost" — the Langfuse-side
// cost field requires the polling job described in DESIGN-0016 Phase 7
// which is parked as a follow-up. Stack-mode shows total spend at a
// glance.
func JudgeCostByModel() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Judge Cost by Model").
		Description("Cumulative USD cost of LLM-as-judge calls per model (spt_judge_cost_usd_total)").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(12).
		WithTarget(PromQuery(
			`sum by (model) (spt_judge_cost_usd_total{job="server-price-tracker"})`,
			"{{model}}",
			"A",
		)).
		Unit("currencyUSD").
		FillOpacity(20).
		LineWidth(2).
		Legend(TableLegend("lastNotNull", "max")).
		Tooltip(MultiTooltip()).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}

// PipelineStageVolume approximates trace volume per pipeline stage by
// charting the rate of completed operations per stage histogram. Each
// `_count` rate is a proxy for span count per minute until OTel-derived
// span counters are exposed via the Collector → Prometheus pathway.
func PipelineStageVolume() *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Title("Pipeline Stage Volume").
		Description("Operations per second per pipeline stage (proxy for OTel span count by stage)").
		Datasource(DSRef()).
		Height(TSHeight).
		Span(12).
		WithTarget(PromQuery(
			`sum(rate(spt_ingestion_duration_seconds_count{job="server-price-tracker"}[5m]))`,
			"ingestion",
			"A",
		)).
		WithTarget(PromQuery(
			`sum(rate(spt_extraction_duration_seconds_count{job="server-price-tracker"}[5m]))`,
			"extraction",
			"B",
		)).
		WithTarget(PromQuery(
			`sum(rate(spt_alerts_query_duration_seconds_count{job="server-price-tracker"}[5m]))`,
			"alerts query",
			"C",
		)).
		WithTarget(PromQuery(
			`sum(rate(spt_notification_duration_seconds_count{job="server-price-tracker"}[5m]))`,
			"notification",
			"D",
		)).
		Unit("ops").
		FillOpacity(10).
		LineWidth(2).
		Legend(TableLegend("mean", "max", "lastNotNull")).
		Tooltip(MultiTooltip()).
		Thresholds(ThresholdsGreenOnly()).
		ColorScheme(ColorSchemePaletteClassic()).
		DrawStyle(common.GraphDrawStyleLine)
}
