package client

import (
	"context"
)

// JudgeRunResult mirrors handlers.JudgeRunOutput.Body — duplicated
// here so the client doesn't depend on internal/api/handlers.
type JudgeRunResult struct {
	Judged          int  `json:"judged"`
	BudgetExhausted bool `json:"budget_exhausted"`
}

// RunJudge POSTs /api/v1/judge/run and returns the judge tally. The
// server enforces the daily-budget cap; BudgetExhausted=true means
// the run stopped early because the cap was met.
func (c *Client) RunJudge(ctx context.Context) (*JudgeRunResult, error) {
	var resp JudgeRunResult
	if err := c.post(ctx, "/api/v1/judge/run", map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
