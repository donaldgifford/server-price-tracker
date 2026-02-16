// Package panels provides Grafana dashboard panel builders for
// server-price-tracker metrics.
package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
)

// EbayDailyLimit is the hard limit for eBay Browse API daily calls.
const EbayDailyLimit = 5000

// Standard panel dimensions for a 24-column grid.
const (
	StatWidth  = 6
	StatHeight = 4

	TSWidth  = 12
	TSHeight = 8

	FullWidth = 24
)

// DSRef returns a datasource reference pointing at the ${datasource}
// template variable.
func DSRef() dashboard.DataSourceRef {
	return dashboard.DataSourceRef{
		Type: cog.ToPtr("prometheus"),
		Uid:  cog.ToPtr("${datasource}"),
	}
}

// PromQuery builds a Prometheus query target with the given expression,
// legend format, and ref ID.
func PromQuery(expr, legendFormat, refID string) *prometheus.DataqueryBuilder {
	return prometheus.NewDataqueryBuilder().
		Expr(expr).
		LegendFormat(legendFormat).
		RefId(refID)
}

// ThresholdsRedGreen returns thresholds that are red below the value and
// green at or above it.
func ThresholdsRedGreen(greenAbove float64) cog.Builder[dashboard.ThresholdsConfig] {
	return dashboard.NewThresholdsConfigBuilder().
		Mode(dashboard.ThresholdsModeAbsolute).
		Steps([]dashboard.Threshold{
			{Color: "red"},
			{Value: cog.ToPtr[float64](greenAbove), Color: "green"},
		})
}

// ThresholdsGreenYellowRed returns three-tier thresholds.
func ThresholdsGreenYellowRed(yellow, red float64) cog.Builder[dashboard.ThresholdsConfig] {
	return dashboard.NewThresholdsConfigBuilder().
		Mode(dashboard.ThresholdsModeAbsolute).
		Steps([]dashboard.Threshold{
			{Color: "green"},
			{Value: cog.ToPtr[float64](yellow), Color: "yellow"},
			{Value: cog.ToPtr[float64](red), Color: "red"},
		})
}

// ThresholdsGreenOnly returns a single green threshold step.
func ThresholdsGreenOnly() cog.Builder[dashboard.ThresholdsConfig] {
	return dashboard.NewThresholdsConfigBuilder().
		Mode(dashboard.ThresholdsModeAbsolute).
		Steps([]dashboard.Threshold{
			{Color: "green"},
		})
}

// ColorSchemeThresholds returns a color scheme that maps to threshold colors.
func ColorSchemeThresholds() cog.Builder[dashboard.FieldColor] {
	return dashboard.NewFieldColorBuilder().
		Mode(dashboard.FieldColorModeIdThresholds)
}

// ColorSchemePaletteClassic returns a color scheme using the classic palette.
func ColorSchemePaletteClassic() cog.Builder[dashboard.FieldColor] {
	return dashboard.NewFieldColorBuilder().
		Mode(dashboard.FieldColorModeIdPaletteClassic)
}

// TableLegend returns a legend configuration displaying as a table at the
// bottom with the specified calculation columns.
func TableLegend(calcs ...string) *common.VizLegendOptionsBuilder {
	return common.NewVizLegendOptionsBuilder().
		DisplayMode(common.LegendDisplayModeTable).
		Placement(common.LegendPlacementBottom).
		Calcs(calcs)
}

// MultiTooltip returns a tooltip configuration showing all series sorted
// descending.
func MultiTooltip() *common.VizTooltipOptionsBuilder {
	return common.NewVizTooltipOptionsBuilder().
		Mode(common.TooltipDisplayModeMulti).
		Sort(common.SortOrderDescending)
}
