package judge_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	extractmocks "github.com/donaldgifford/server-price-tracker/pkg/extract/mocks"
	"github.com/donaldgifford/server-price-tracker/pkg/judge"
	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// sampleAlertContext is the shared canned input for happy-path tests.
// Numbers are calibrated to look like a "good deal": price near P25,
// large baseline sample, used_working condition.
func sampleAlertContext() *judge.AlertContext {
	return &judge.AlertContext{
		AlertID:       "alert-1",
		WatchName:     "Dell R740xd",
		ComponentType: domain.ComponentServer,
		ListingTitle:  "Dell PowerEdge R740xd 12-Bay LFF Xeon Gold 6126 256GB",
		Condition:     domain.ConditionUsedWorking,
		PriceUSD:      650.00,
		BaselineP25:   700.00,
		BaselineP50:   850.00,
		BaselineP75:   1100.00,
		SampleSize:    35,
		Score:         85,
		Threshold:     80,
		Reasons:       []string{"price below P25", "seller feedback >99%"},
	}
}

// TestLLMJudge_EvaluateAlert_HappyPath: backend returns a well-formed
// JSON verdict; judge parses it and packages tokens + model into the
// returned Verdict.
func TestLLMJudge_EvaluateAlert_HappyPath(t *testing.T) {
	t.Parallel()

	backend := extractmocks.NewMockLLMBackend(t)
	backend.EXPECT().
		Generate(mock.Anything, mock.MatchedBy(func(req extract.GenerateRequest) bool {
			return req.Format == extract.FormatJSON
		})).
		Return(extract.GenerateResponse{
			Content: `{"score": 0.87, "reason": "below P25 with full RAM/storage"}`,
			Model:   "claude-haiku-4-5",
			Usage: extract.TokenUsage{
				PromptTokens:     1200,
				CompletionTokens: 25,
				TotalTokens:      1225,
			},
		}, nil).
		Once()

	j, err := judge.NewLLMJudge(backend)
	require.NoError(t, err)

	v, err := j.EvaluateAlert(context.Background(), sampleAlertContext())
	require.NoError(t, err)
	assert.InDelta(t, 0.87, v.Score, 0.0001)
	assert.Equal(t, "below P25 with full RAM/storage", v.Reason)
	assert.Equal(t, "claude-haiku-4-5", v.Model)
	assert.Equal(t, 1200, v.Tokens.InputTokens)
	assert.Equal(t, 25, v.Tokens.OutputTokens)
}

// TestLLMJudge_EvaluateAlert_StripsMarkdownFences: same as happy path
// but the response is wrapped in ```json``` (Anthropic's habit). Must
// parse cleanly.
func TestLLMJudge_EvaluateAlert_StripsMarkdownFences(t *testing.T) {
	t.Parallel()

	backend := extractmocks.NewMockLLMBackend(t)
	backend.EXPECT().
		Generate(mock.Anything, mock.Anything).
		Return(extract.GenerateResponse{
			Content: "```json\n{\"score\": 0.42, \"reason\": \"borderline\"}\n```",
			Model:   "claude-haiku-4-5",
		}, nil).
		Once()

	j, err := judge.NewLLMJudge(backend)
	require.NoError(t, err)

	v, err := j.EvaluateAlert(context.Background(), sampleAlertContext())
	require.NoError(t, err)
	assert.InDelta(t, 0.42, v.Score, 0.0001)
}

// TestLLMJudge_EvaluateAlert_ScoreOutOfRange: the LLM hallucinates a
// score outside [0, 1]; judge must reject rather than letting the
// poisoned value through to the budget tracker.
func TestLLMJudge_EvaluateAlert_ScoreOutOfRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "above 1.0", content: `{"score": 1.5, "reason": "x"}`},
		{name: "negative", content: `{"score": -0.2, "reason": "x"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			backend := extractmocks.NewMockLLMBackend(t)
			backend.EXPECT().
				Generate(mock.Anything, mock.Anything).
				Return(extract.GenerateResponse{Content: tt.content}, nil).
				Once()

			j, err := judge.NewLLMJudge(backend)
			require.NoError(t, err)

			_, err = j.EvaluateAlert(context.Background(), sampleAlertContext())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "out of range")
		})
	}
}

// TestLLMJudge_EvaluateAlert_BackendError: backend Generate fails;
// judge surfaces the error wrapped with context.
func TestLLMJudge_EvaluateAlert_BackendError(t *testing.T) {
	t.Parallel()

	backend := extractmocks.NewMockLLMBackend(t)
	backend.EXPECT().
		Generate(mock.Anything, mock.Anything).
		Return(extract.GenerateResponse{}, errors.New("upstream timeout")).
		Once()

	j, err := judge.NewLLMJudge(backend)
	require.NoError(t, err)

	_, err = j.EvaluateAlert(context.Background(), sampleAlertContext())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upstream timeout")
}

// TestLLMJudge_EvaluateAlert_ComputesCostFromModelTable: when a cost
// table is wired and the response model matches, Verdict.CostUSD is
// populated. Validates the per-million-token math via langfuse.ModelCost.
func TestLLMJudge_EvaluateAlert_ComputesCostFromModelTable(t *testing.T) {
	t.Parallel()

	backend := extractmocks.NewMockLLMBackend(t)
	backend.EXPECT().
		Generate(mock.Anything, mock.Anything).
		Return(extract.GenerateResponse{
			Content: `{"score": 0.5, "reason": "ok"}`,
			Model:   "claude-haiku-4-5",
			Usage:   extract.TokenUsage{PromptTokens: 1_000_000, CompletionTokens: 200_000},
		}, nil).
		Once()

	costs := map[string]langfuse.ModelCost{
		"claude-haiku-4-5": {InputUSDPerMillion: 1.0, OutputUSDPerMillion: 5.0},
	}
	j, err := judge.NewLLMJudge(backend, judge.WithModelCosts(costs))
	require.NoError(t, err)

	v, err := j.EvaluateAlert(context.Background(), sampleAlertContext())
	require.NoError(t, err)
	// 1M input @ $1 = $1; 200k output @ $5/M = $1; total $2.
	assert.InDelta(t, 2.0, v.CostUSD, 0.0001)
}

// TestLLMJudge_EvaluateAlert_RendersPrompt: a quick smoke test that
// the prompt builder produces non-empty output that includes the
// alert's title and price. Catches template breakage without locking
// in the entire prompt body as a golden file.
func TestLLMJudge_EvaluateAlert_RendersPrompt(t *testing.T) {
	t.Parallel()

	backend := extractmocks.NewMockLLMBackend(t)
	var seenPrompt string
	backend.EXPECT().
		Generate(mock.Anything, mock.Anything).
		Run(func(_ context.Context, req extract.GenerateRequest) {
			seenPrompt = req.Prompt
		}).
		Return(extract.GenerateResponse{Content: `{"score": 0.5, "reason": "ok"}`}, nil).
		Once()

	j, err := judge.NewLLMJudge(backend)
	require.NoError(t, err)

	ac := sampleAlertContext()
	_, err = j.EvaluateAlert(context.Background(), ac)
	require.NoError(t, err)
	assert.Contains(t, seenPrompt, ac.ListingTitle)
	assert.Contains(t, seenPrompt, "650.00")
	assert.Contains(t, seenPrompt, "Verdict")
}
