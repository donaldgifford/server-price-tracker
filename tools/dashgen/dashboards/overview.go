// Package dashboards assembles Grafana dashboard definitions from panel builders.
package dashboards

import (
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/donaldgifford/server-price-tracker/tools/dashgen/panels"
)

// BuildOverview constructs the SPT Overview dashboard with all metric rows.
func BuildOverview() *dashboard.DashboardBuilder {
	b := dashboard.NewDashboardBuilder("SPT Overview").
		Uid("spt-overview").
		Tags([]string{"spt", "server-price-tracker"}).
		Refresh("30s").
		Time("now-6h", "now").
		Timezone("browser").
		Editable().
		Tooltip(dashboard.DashboardCursorSyncCrosshair).
		WithVariable(datasourceVar())

	// Row 1: Overview.
	b.WithRow(dashboard.NewRowBuilder("Overview").
		WithPanel(panels.HealthzStat()).
		WithPanel(panels.ReadyzStat()).
		WithPanel(panels.QuotaGauge()).
		WithPanel(panels.UptimeStat()))

	// Row 2: HTTP.
	b.WithRow(dashboard.NewRowBuilder("HTTP").
		WithPanel(panels.RequestRate()).
		WithPanel(panels.LatencyPercentiles()).
		WithPanel(panels.ErrorRate()))

	// Row 3: eBay API.
	b.WithRow(dashboard.NewRowBuilder("eBay API").
		WithPanel(panels.APICallsRate()).
		WithPanel(panels.DailyUsage()).
		WithPanel(panels.ResetCountdown()).
		WithPanel(panels.LimitHits()))

	// Row 4: Ingestion.
	b.WithRow(dashboard.NewRowBuilder("Ingestion").
		WithPanel(panels.ListingsRate()).
		WithPanel(panels.IngestionErrors()).
		WithPanel(panels.CycleDuration()))

	// Row 5: Extraction.
	b.WithRow(dashboard.NewRowBuilder("Extraction").
		WithPanel(panels.ExtractionDuration()).
		WithPanel(panels.ExtractionFailures()))

	// Row 6: Scoring.
	b.WithRow(dashboard.NewRowBuilder("Scoring").
		WithPanel(panels.ScoreDistribution()))

	// Row 7: Alerts.
	b.WithRow(dashboard.NewRowBuilder("Alerts").
		WithPanel(panels.AlertsRate()).
		WithPanel(panels.NotificationFailures()))

	return b
}

func datasourceVar() *dashboard.DatasourceVariableBuilder {
	return dashboard.NewDatasourceVariableBuilder("datasource").
		Label("Datasource").
		Type("prometheus")
}
