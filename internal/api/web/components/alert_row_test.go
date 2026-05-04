package components_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/web/components"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// renderRow runs AlertRow with the given options and returns the
// rendered HTML so individual feature flags can be asserted by
// substring. Tiny helper kept here so each test stays compact.
func renderRow(t *testing.T, a domain.AlertWithListing, opts components.TableOptions) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, components.AlertRow(a, opts).Render(context.Background(), &buf))
	return buf.String()
}

// TestAlertRow_LangfuseEnabledRendersTraceLink covers the Phase 4
// IMPL-0019 deep-link wiring: when the alert has a trace_id AND the
// Langfuse endpoint is configured, the row renders a Trace ↗ link.
func TestAlertRow_LangfuseEnabledRendersTraceLink(t *testing.T) {
	t.Parallel()

	traceID := "abc123"
	a := domain.AlertWithListing{
		Alert: domain.Alert{
			ID:        "alert-1",
			Score:     85,
			TraceID:   &traceID,
			CreatedAt: time.Now().Add(-2 * time.Hour),
		},
		Listing: domain.Listing{
			ItemURL:       "https://www.ebay.com/itm/123",
			Title:         "test listing",
			ComponentType: domain.ComponentRAM,
		},
		WatchName: "RAM watch",
	}

	html := renderRow(t, a, components.TableOptions{
		LangfuseEndpoint: "https://langfuse.example.com",
	})
	assert.Contains(t, html, "https://langfuse.example.com/trace/abc123")
	assert.Contains(t, html, "Trace ↗")
}

// TestAlertRow_LangfuseDisabledHidesTraceLink: empty endpoint → no
// Trace link, even when the alert has a trace_id.
func TestAlertRow_LangfuseDisabledHidesTraceLink(t *testing.T) {
	t.Parallel()

	traceID := "abc123"
	a := domain.AlertWithListing{
		Alert: domain.Alert{
			ID:      "alert-1",
			Score:   85,
			TraceID: &traceID,
		},
		Listing: domain.Listing{ItemURL: "https://www.ebay.com/itm/123", Title: "x"},
	}

	html := renderRow(t, a, components.TableOptions{LangfuseEndpoint: ""})
	assert.NotContains(t, html, "Trace ↗")
}

// TestAlertRow_NoTraceIDHidesTraceLink: alert without trace_id (older
// alert, pre-IMPL-0019) → no Trace link even when langfuse is on.
func TestAlertRow_NoTraceIDHidesTraceLink(t *testing.T) {
	t.Parallel()

	a := domain.AlertWithListing{
		Alert:   domain.Alert{ID: "alert-1", Score: 85, TraceID: nil},
		Listing: domain.Listing{ItemURL: "https://www.ebay.com/itm/123", Title: "x"},
	}
	html := renderRow(t, a, components.TableOptions{LangfuseEndpoint: "https://langfuse.example.com"})
	assert.NotContains(t, html, "Trace ↗")
}

// TestAlertRow_JudgeEnabledRendersColumn covers the Phase 4
// judge_score column placeholder: rendered when Judge is enabled,
// hidden otherwise. Cell starts empty until Phase 5 wires it.
func TestAlertRow_JudgeEnabledRendersColumn(t *testing.T) {
	t.Parallel()

	a := domain.AlertWithListing{
		Alert:   domain.Alert{ID: "alert-1", Score: 85},
		Listing: domain.Listing{Title: "x"},
	}

	enabled := renderRow(t, a, components.TableOptions{JudgeEnabled: true})
	assert.Contains(t, enabled, `class="judge-score"`,
		"judge-score column must render when JudgeEnabled is true")

	disabled := renderRow(t, a, components.TableOptions{JudgeEnabled: false})
	assert.NotContains(t, disabled, `class="judge-score"`,
		"judge-score column must be absent when JudgeEnabled is false")
}
