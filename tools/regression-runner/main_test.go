package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/regression"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestParseBackends(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flagVal  string
		fallback string
		want     []string
	}{
		{
			name:     "empty flag falls back to single config backend",
			flagVal:  "",
			fallback: "ollama",
			want:     []string{"ollama"},
		},
		{
			name:     "single backend in flag overrides fallback",
			flagVal:  "anthropic",
			fallback: "ollama",
			want:     []string{"anthropic"},
		},
		{
			name:     "multiple backends parsed in order",
			flagVal:  "ollama,anthropic,openai_compat",
			fallback: "ollama",
			want:     []string{"ollama", "anthropic", "openai_compat"},
		},
		{
			name:     "duplicates are deduplicated",
			flagVal:  "ollama,anthropic,ollama",
			fallback: "ollama",
			want:     []string{"ollama", "anthropic"},
		},
		{
			name:     "whitespace is trimmed",
			flagVal:  " ollama , anthropic ",
			fallback: "ollama",
			want:     []string{"ollama", "anthropic"},
		},
		{
			name:     "empty entries from stray commas are skipped",
			flagVal:  "ollama,,anthropic,",
			fallback: "ollama",
			want:     []string{"ollama", "anthropic"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseBackends(tt.flagVal, tt.fallback)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPercentiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []time.Duration
		wantP50 time.Duration
		wantP95 time.Duration
	}{
		{
			name:    "empty slice returns zero",
			input:   nil,
			wantP50: 0,
			wantP95: 0,
		},
		{
			name:    "single sample is its own p50 and p95",
			input:   []time.Duration{500 * time.Millisecond},
			wantP50: 500 * time.Millisecond,
			wantP95: 500 * time.Millisecond,
		},
		{
			name: "twenty evenly-spaced samples — nearest-rank picks the lower index",
			input: func() []time.Duration {
				out := make([]time.Duration, 20)
				for i := range out {
					out[i] = time.Duration(i+1) * 100 * time.Millisecond
				}
				return out
			}(),
			// nearest-rank: idx50 = (20-1)*50/100 = 9 → sorted[9] = 1000ms
			// nearest-rank: idx95 = (20-1)*95/100 = 18 → sorted[18] = 1900ms
			wantP50: 1000 * time.Millisecond,
			wantP95: 1900 * time.Millisecond,
		},
		{
			name: "unsorted input is sorted before percentile selection",
			input: []time.Duration{
				900 * time.Millisecond,
				100 * time.Millisecond,
				500 * time.Millisecond,
			},
			// sorted: 100, 500, 900
			// idx50 = (3-1)*50/100 = 1 → 500ms
			// idx95 = (3-1)*95/100 = 1 → 500ms
			wantP50: 500 * time.Millisecond,
			wantP95: 500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotP50, gotP95 := percentiles(tt.input)
			assert.Equal(t, tt.wantP50, gotP50, "p50")
			assert.Equal(t, tt.wantP95, gotP95, "p95")
		})
	}
}

func TestComponentResultAccuracy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  componentResult
		wantPC float64
	}{
		{
			name:   "zero total returns zero rather than NaN",
			input:  componentResult{Component: domain.ComponentRAM, Total: 0, Correct: 0},
			wantPC: 0,
		},
		{
			name:   "perfect run reports 100%",
			input:  componentResult{Component: domain.ComponentRAM, Total: 10, Correct: 10},
			wantPC: 100,
		},
		{
			name:   "half correct reports 50%",
			input:  componentResult{Component: domain.ComponentDrive, Total: 8, Correct: 4},
			wantPC: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.InDelta(t, tt.wantPC, tt.input.Accuracy(), 0.01)
		})
	}
}

func TestBuildDatasetRunItems_PairsMismatchesByTitle(t *testing.T) {
	t.Parallel()

	dataset := []regression.Item{
		{Title: "Dell R740xd", ExpectedComponent: domain.ComponentServer},
		{Title: "32GB DDR4 ECC", ExpectedComponent: domain.ComponentRAM},
		{Title: "RTX 3090 24GB", ExpectedComponent: domain.ComponentGPU},
	}

	r := &runResult{
		Backend: "ollama",
		Mismatches: []mismatch{
			{Title: "Dell R740xd", Expected: domain.ComponentServer, Actual: domain.ComponentOther},
		},
	}

	items := buildDatasetRunItems(dataset, r)
	require.Len(t, items, 3)

	// First row mismatched: actual differs from expected, error
	// field is empty (no LLM error, just wrong label).
	assert.Equal(t, "server", items[0].Output["expected"])
	assert.Equal(t, "other", items[0].Output["actual"])
	assert.Empty(t, items[0].Output["error"])

	// Second & third rows correct: actual == expected.
	assert.Equal(t, "ram", items[1].Output["expected"])
	assert.Equal(t, "ram", items[1].Output["actual"])
	assert.Equal(t, "gpu", items[2].Output["expected"])
	assert.Equal(t, "gpu", items[2].Output["actual"])

	// DatasetItemIDs are deterministic title hashes.
	assert.Equal(t, regression.TitleHash("Dell R740xd"), items[0].DatasetItemID)
	assert.Equal(t, regression.TitleHash("32GB DDR4 ECC"), items[1].DatasetItemID)
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{
			name: "shorter than max passes through unchanged",
			s:    "hello",
			n:    10,
			want: "hello",
		},
		{
			name: "exactly at max passes through unchanged",
			s:    "hello",
			n:    5,
			want: "hello",
		},
		{
			name: "longer than max gets ellipsis-suffix at n",
			s:    "hello world",
			n:    8,
			want: "hello...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncate(tt.s, tt.n)
			assert.Equal(t, tt.want, got)
			assert.LessOrEqual(t, len(got), tt.n)
		})
	}
}
