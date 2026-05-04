package langfuse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestModelCost_ComputeCost is a table-driven check that the per-million
// conversion is right and that the input/output legs are summed
// independently. The 1e6 division is the only subtle bit here — easy
// to confuse with 1e3 or per-thousand pricing models.
func TestModelCost_ComputeCost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cost    ModelCost
		usage   TokenUsage
		wantUSD float64
		delta   float64
	}{
		{
			name:    "haiku-4-5 anchor: 1M input + 200k output @ $1/$5",
			cost:    ModelCost{InputUSDPerMillion: 1.0, OutputUSDPerMillion: 5.0},
			usage:   TokenUsage{InputTokens: 1_000_000, OutputTokens: 200_000},
			wantUSD: 1.0 + 1.0, // $1 input + $1 output ($5/M * 0.2M)
			delta:   0.0001,
		},
		{
			name:    "small request: 250 in / 5 out @ $1/$5 ≈ negligible",
			cost:    ModelCost{InputUSDPerMillion: 1.0, OutputUSDPerMillion: 5.0},
			usage:   TokenUsage{InputTokens: 250, OutputTokens: 5},
			wantUSD: 0.000275, // 250/1e6*$1 + 5/1e6*$5 = 0.00025 + 0.000025
			delta:   0.0000001,
		},
		{
			name:    "free model (Ollama): zero rates → zero cost",
			cost:    ModelCost{InputUSDPerMillion: 0, OutputUSDPerMillion: 0},
			usage:   TokenUsage{InputTokens: 5_000_000, OutputTokens: 5_000_000},
			wantUSD: 0,
			delta:   0,
		},
		{
			name:    "zero usage: ignores rates",
			cost:    ModelCost{InputUSDPerMillion: 100.0, OutputUSDPerMillion: 100.0},
			usage:   TokenUsage{},
			wantUSD: 0,
			delta:   0,
		},
		{
			name:    "asymmetric output-only call",
			cost:    ModelCost{InputUSDPerMillion: 1.0, OutputUSDPerMillion: 5.0},
			usage:   TokenUsage{InputTokens: 0, OutputTokens: 1_000_000},
			wantUSD: 5.0,
			delta:   0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.cost.ComputeCost(tt.usage)
			assert.InDelta(t, tt.wantUSD, got, tt.delta)
		})
	}
}
