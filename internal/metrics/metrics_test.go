package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsRegistered(t *testing.T) {
	t.Parallel()

	// Verify all metrics are non-nil (registered via promauto on package init).
	assert.NotNil(t, HTTPRequestDuration)
	assert.NotNil(t, HTTPRequestsTotal)
	assert.NotNil(t, IngestionListingsTotal)
	assert.NotNil(t, IngestionErrorsTotal)
	assert.NotNil(t, IngestionDuration)
	assert.NotNil(t, ExtractionDuration)
	assert.NotNil(t, ExtractionFailuresTotal)
	assert.NotNil(t, ExtractionTokensTotal)
	assert.NotNil(t, ExtractionTokensPerRequest)
	assert.NotNil(t, ScoringDistribution)
	assert.NotNil(t, AlertsFiredTotal)
	assert.NotNil(t, NotificationFailuresTotal)
}

// TestMetricsHandlerExposesLLMTokenMetrics scrapes the standard Prometheus
// HTTP handler (the same one wired into /metrics in serve.go) and verifies
// that the new LLM token metrics appear with HELP/TYPE lines once at least
// one series exists. This closes the manual "curl /metrics | grep
// spt_extraction_tokens" check from IMPL-0014 phase 2/4. (Empty vec metrics
// with no observed series do not emit HELP/TYPE — that's a property of the
// prometheus client library, not a missing registration. We force one
// observation so the metadata appears.)
func TestMetricsHandlerExposesLLMTokenMetrics(t *testing.T) {
	t.Parallel()

	// Materialize one series on each new metric so HELP/TYPE lines render.
	// Use a unique label value so this test does not collide with concurrent
	// tests in other packages that read the same metric vec.
	const seedLabel = "metrics-handler-test"
	ExtractionTokensTotal.WithLabelValues(seedLabel, seedLabel, "input").Add(0)
	ExtractionTokensPerRequest.WithLabelValues(seedLabel, seedLabel).Observe(0)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	promhttp.Handler().ServeHTTP(rec, req)

	require.Equal(t, 200, rec.Code, "expected /metrics to return 200")
	body := rec.Body.String()

	for _, expected := range []string{
		"# HELP spt_extraction_tokens_total",
		"# TYPE spt_extraction_tokens_total counter",
		"# HELP spt_extraction_tokens_per_request",
		"# TYPE spt_extraction_tokens_per_request histogram",
	} {
		assert.Contains(t, body, expected,
			"/metrics output must include %q", expected)
	}
}
