package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/bargauge"
	"github.com/grafana/grafana-foundation-sdk/go/common"
)

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
