package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
	assert.NotNil(t, ScoringDistribution)
	assert.NotNil(t, AlertsFiredTotal)
	assert.NotNil(t, NotificationFailuresTotal)
}
