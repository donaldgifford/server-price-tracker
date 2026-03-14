package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// JobsProvider defines the store methods required by the jobs handler.
type JobsProvider interface {
	ListLatestJobRuns(ctx context.Context) ([]domain.JobRun, error)
	ListJobRuns(ctx context.Context, jobName string, limit int) ([]domain.JobRun, error)
}

// JobsHandler handles scheduler job history requests.
type JobsHandler struct {
	store JobsProvider
}

// NewJobsHandler creates a new JobsHandler.
func NewJobsHandler(s JobsProvider) *JobsHandler {
	return &JobsHandler{store: s}
}

// ListJobsOutput is the response body for listing the latest job runs.
type ListJobsOutput struct {
	Body []domain.JobRun
}

// GetJobHistoryInput is the request path for job history.
type GetJobHistoryInput struct {
	JobName string `path:"job_name" doc:"Scheduled job name (e.g. ingestion, baseline_refresh)"`
}

// GetJobHistoryOutput is the response body for a single job's history.
type GetJobHistoryOutput struct {
	Body []domain.JobRun
}

const defaultJobHistoryLimit = 20

// ListJobs returns the most recent run for each distinct scheduler job.
func (h *JobsHandler) ListJobs(
	ctx context.Context,
	_ *struct{},
) (*ListJobsOutput, error) {
	runs, err := h.store.ListLatestJobRuns(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("listing jobs failed: " + err.Error())
	}

	if runs == nil {
		runs = []domain.JobRun{}
	}

	return &ListJobsOutput{Body: runs}, nil
}

// GetJobHistory returns the run history for a specific scheduler job.
func (h *JobsHandler) GetJobHistory(
	ctx context.Context,
	input *GetJobHistoryInput,
) (*GetJobHistoryOutput, error) {
	runs, err := h.store.ListJobRuns(ctx, input.JobName, defaultJobHistoryLimit)
	if err != nil {
		return nil, huma.Error500InternalServerError("fetching job history failed: " + err.Error())
	}

	if runs == nil {
		runs = []domain.JobRun{}
	}

	return &GetJobHistoryOutput{Body: runs}, nil
}

// RegisterJobRoutes registers scheduler job endpoints with the Huma API.
func RegisterJobRoutes(api huma.API, h *JobsHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "list-jobs",
		Method:      http.MethodGet,
		Path:        "/api/v1/jobs",
		Summary:     "List latest scheduler job runs",
		Description: "Returns the most recent run record for each distinct scheduled job.",
		Tags:        []string{"scheduler"},
		Errors:      []int{http.StatusInternalServerError},
	}, h.ListJobs)

	huma.Register(api, huma.Operation{
		OperationID: "get-job-history",
		Method:      http.MethodGet,
		Path:        "/api/v1/jobs/{job_name}",
		Summary:     "Get scheduler job history",
		Description: "Returns the run history for a specific scheduled job (newest first).",
		Tags:        []string{"scheduler"},
		Errors:      []int{http.StatusInternalServerError},
	}, h.GetJobHistory)
}
