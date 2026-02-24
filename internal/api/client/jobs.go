package client

import (
	"context"
	"fmt"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ListJobs returns the most recent run for each distinct scheduled job.
func (c *Client) ListJobs(ctx context.Context) ([]domain.JobRun, error) {
	var runs []domain.JobRun
	if err := c.get(ctx, "/api/v1/jobs", &runs); err != nil {
		return nil, err
	}
	return runs, nil
}

// GetJobHistory returns the run history for a specific scheduled job.
func (c *Client) GetJobHistory(ctx context.Context, jobName string) ([]domain.JobRun, error) {
	var runs []domain.JobRun
	if err := c.get(ctx, fmt.Sprintf("/api/v1/jobs/%s", jobName), &runs); err != nil {
		return nil, err
	}
	return runs, nil
}
