package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/donaldgifford/server-price-tracker/tools/dashgen/dashboards"
	"github.com/donaldgifford/server-price-tracker/tools/dashgen/rules"
	"github.com/donaldgifford/server-price-tracker/tools/dashgen/validate"
)

func TestDefaultConfigValid(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	require.NoError(t, cfg.Validate())
}

func TestConfigValidate_EmptyOutputDir(t *testing.T) {
	t.Parallel()
	cfg := Config{OutputDir: "", DashboardEnabled: true}
	assert.Error(t, cfg.Validate())
}

func TestConfigValidate_NothingEnabled(t *testing.T) {
	t.Parallel()
	cfg := Config{OutputDir: "/tmp", DashboardEnabled: false, RulesEnabled: false}
	assert.Error(t, cfg.Validate())
}

func TestBuildOverviewDashboard(t *testing.T) {
	t.Parallel()

	builder := dashboards.BuildOverview()
	dash, err := builder.Build()
	require.NoError(t, err)

	// Verify dashboard metadata.
	require.NotNil(t, dash.Uid)
	assert.Equal(t, "spt-overview", *dash.Uid)

	require.NotNil(t, dash.Title)
	assert.Equal(t, "SPT Overview", *dash.Title)

	// Verify template variable.
	require.NotNil(t, dash.Templating)
	assert.Len(t, dash.Templating.List, 1)
	assert.Equal(t, "datasource", dash.Templating.List[0].Name)

	// Verify we have 7 rows.
	assert.Len(t, dash.Panels, 7)

	// Count total inner panels.
	totalPanels := 0
	for _, p := range dash.Panels {
		if p.RowPanel != nil {
			totalPanels += len(p.RowPanel.Panels)
		}
	}
	assert.Equal(t, 18, totalPanels)

	// Validate PromQL and metrics.
	result := validate.Dashboard(dash, KnownMetrics)
	assert.True(t, result.Ok(), "validation errors: %v", result.Errors)
	assert.Empty(t, result.Warnings, "unexpected warnings: %v", result.Warnings)
}

func TestRecordingRules(t *testing.T) {
	t.Parallel()

	cr := rules.RecordingRules()
	assert.Equal(t, "monitoring.coreos.com/v1", cr.APIVersion)
	assert.Equal(t, "PrometheusRule", cr.Kind)
	assert.Equal(t, "spt-recording-rules", cr.Metadata.Name)

	require.Len(t, cr.Spec.Groups, 1)
	group := cr.Spec.Groups[0]
	assert.Equal(t, "spt-recording", group.Name)
	require.Len(t, group.Rules, 6)

	expectedRecords := []string{
		"spt:http_requests:rate5m",
		"spt:http_errors:rate5m",
		"spt:ingestion_listings:rate5m",
		"spt:ingestion_errors:rate5m",
		"spt:extraction_failures:rate5m",
		"spt:ebay_api_calls:rate5m",
	}
	for i, rule := range group.Rules {
		assert.Equal(t, expectedRecords[i], rule.Record)
		assert.NotEmpty(t, rule.Expr)
	}

	// Verify YAML marshaling works.
	data, err := yaml.Marshal(cr)
	require.NoError(t, err)
	assert.Contains(t, string(data), "apiVersion: monitoring.coreos.com/v1")
}

func TestAlertRules(t *testing.T) {
	t.Parallel()

	cr := rules.AlertRules()
	assert.Equal(t, "monitoring.coreos.com/v1", cr.APIVersion)
	assert.Equal(t, "PrometheusRule", cr.Kind)
	assert.Equal(t, "spt-alerts", cr.Metadata.Name)

	require.Len(t, cr.Spec.Groups, 1)
	group := cr.Spec.Groups[0]
	assert.Equal(t, "spt-alerts", group.Name)
	require.Len(t, group.Rules, 8)

	expectedAlerts := []string{
		"SptDown",
		"SptReadinessDown",
		"SptHighErrorRate",
		"SptIngestionErrors",
		"SptExtractionFailures",
		"SptEbayQuotaHigh",
		"SptEbayLimitReached",
		"SptNotificationFailures",
	}
	for i, rule := range group.Rules {
		assert.Equal(t, expectedAlerts[i], rule.Alert)
		assert.NotEmpty(t, rule.Expr)
		assert.NotEmpty(t, rule.Labels["severity"], "alert %s missing severity", rule.Alert)
		assert.NotEmpty(t, rule.Annotations["summary"], "alert %s missing summary", rule.Alert)
		assert.NotEmpty(t, rule.Annotations["description"], "alert %s missing description", rule.Alert)
	}
}

func TestStaleness(t *testing.T) {
	t.Parallel()

	// Generate dashboard JSON.
	builder := dashboards.BuildOverview()
	dash, err := builder.Build()
	require.NoError(t, err)

	dashJSON, err := json.MarshalIndent(dash, "", "  ")
	require.NoError(t, err)
	dashJSON = append(dashJSON, '\n')

	// Generate recording rules YAML.
	recording := rules.RecordingRules()
	recordingYAML, err := yaml.Marshal(recording)
	require.NoError(t, err)
	recordingYAML = append([]byte(generatedHeader), recordingYAML...)

	// Generate alert rules YAML.
	alerts := rules.AlertRules()
	alertsYAML, err := yaml.Marshal(alerts)
	require.NoError(t, err)
	alertsYAML = append([]byte(generatedHeader), alertsYAML...)

	// Compare with committed files.
	deployDir := filepath.Join("..", "..", "deploy")

	tests := []struct {
		name     string
		path     string
		expected []byte
	}{
		{
			name:     "spt-overview.json",
			path:     filepath.Join(deployDir, "grafana", "data", "spt-overview.json"),
			expected: dashJSON,
		},
		{
			name:     "spt-recording-rules.yaml",
			path:     filepath.Join(deployDir, "prometheus", "spt-recording-rules.yaml"),
			expected: recordingYAML,
		},
		{
			name:     "spt-alerts.yaml",
			path:     filepath.Join(deployDir, "prometheus", "spt-alerts.yaml"),
			expected: alertsYAML,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			committed, err := os.ReadFile(tt.path)
			require.NoError(t, err, "could not read committed file %s", tt.path)
			assert.Equal(t, string(tt.expected), string(committed),
				"%s is stale — run 'make dashboards' to regenerate", tt.name)
		})
	}
}
