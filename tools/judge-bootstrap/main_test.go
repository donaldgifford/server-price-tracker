package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/pkg/judge"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestStratify_InterleavesAcrossComponentTypes(t *testing.T) {
	t.Parallel()

	now := time.Now()
	candidates := []domain.JudgeCandidate{
		{AlertID: "ram-1", ComponentType: domain.ComponentRAM, ListingTitle: "32GB DDR4", CreatedAt: now},
		{AlertID: "ram-2", ComponentType: domain.ComponentRAM, ListingTitle: "64GB DDR4", CreatedAt: now},
		{AlertID: "ram-3", ComponentType: domain.ComponentRAM, ListingTitle: "128GB DDR4", CreatedAt: now},
		{AlertID: "drive-1", ComponentType: domain.ComponentDrive, ListingTitle: "1TB NVMe", CreatedAt: now},
		{AlertID: "drive-2", ComponentType: domain.ComponentDrive, ListingTitle: "2TB SSD", CreatedAt: now},
	}

	got := stratify(candidates, 4)
	require.Len(t, got, 4)

	// Should interleave: drive-1, ram-1, drive-2, ram-2 (alphabetic key order).
	assert.Equal(t, "drive-1", got[0].Alert.AlertID)
	assert.Equal(t, "ram-1", got[1].Alert.AlertID)
	assert.Equal(t, "drive-2", got[2].Alert.AlertID)
	assert.Equal(t, "ram-2", got[3].Alert.AlertID)
}

func TestStratify_RespectsLimit(t *testing.T) {
	t.Parallel()

	now := time.Now()
	candidates := []domain.JudgeCandidate{
		{AlertID: "a", ComponentType: domain.ComponentRAM, CreatedAt: now},
		{AlertID: "b", ComponentType: domain.ComponentRAM, CreatedAt: now},
		{AlertID: "c", ComponentType: domain.ComponentRAM, CreatedAt: now},
	}
	got := stratify(candidates, 2)
	assert.Len(t, got, 2)
}

func TestStratify_EmptyInput(t *testing.T) {
	t.Parallel()
	got := stratify(nil, 5)
	assert.Empty(t, got)
}

func TestValidateLabels(t *testing.T) {
	t.Parallel()

	good := labelledCandidate{
		Label: "deal",
		Alert: judge.AlertContext{AlertID: "a"},
		Verdict: judge.Verdict{
			Score:  0.85,
			Reason: "well below P25 + clean specs",
		},
	}

	tests := []struct {
		name    string
		input   []labelledCandidate
		wantErr bool
	}{
		{
			name:    "valid row passes",
			input:   []labelledCandidate{good},
			wantErr: false,
		},
		{
			name: "invalid label rejected",
			input: []labelledCandidate{{
				Label:   "spam",
				Alert:   good.Alert,
				Verdict: good.Verdict,
			}},
			wantErr: true,
		},
		{
			name: "score below 0 rejected",
			input: []labelledCandidate{{
				Label:   "noise",
				Alert:   good.Alert,
				Verdict: judge.Verdict{Score: -0.1, Reason: "x"},
			}},
			wantErr: true,
		},
		{
			name: "score above 1 rejected",
			input: []labelledCandidate{{
				Label:   "deal",
				Alert:   good.Alert,
				Verdict: judge.Verdict{Score: 1.1, Reason: "x"},
			}},
			wantErr: true,
		},
		{
			name: "empty reason rejected",
			input: []labelledCandidate{{
				Label:   "deal",
				Alert:   good.Alert,
				Verdict: judge.Verdict{Score: 0.5, Reason: ""},
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateLabels(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestTraceIDValue(t *testing.T) {
	t.Parallel()
	assert.Empty(t, traceIDValue(nil))

	id := "abc-123"
	assert.Equal(t, id, traceIDValue(&id))
}
