package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// mockJobsProvider is a test double for JobsProvider.
type mockJobsProvider struct {
	latestRuns []domain.JobRun
	history    []domain.JobRun
	err        error
}

func (m *mockJobsProvider) ListLatestJobRuns(_ context.Context) ([]domain.JobRun, error) {
	return m.latestRuns, m.err
}

func (m *mockJobsProvider) ListJobRuns(_ context.Context, _ string, _ int) ([]domain.JobRun, error) {
	return m.history, m.err
}

func sampleJobRun(jobName, status string) domain.JobRun {
	now := time.Now().Truncate(time.Second)
	return domain.JobRun{
		ID:        "job-run-id-1",
		JobName:   jobName,
		StartedAt: now,
		Status:    status,
	}
}

func TestListJobs_Success(t *testing.T) {
	t.Parallel()

	runs := []domain.JobRun{
		sampleJobRun("ingestion", "succeeded"),
		sampleJobRun("baseline_refresh", "succeeded"),
	}
	h := handlers.NewJobsHandler(&mockJobsProvider{latestRuns: runs})

	_, api := humatest.New(t)
	handlers.RegisterJobRoutes(api, h)

	resp := api.Get("/api/v1/jobs")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "ingestion")
	assert.Contains(t, resp.Body.String(), "baseline_refresh")
}

func TestListJobs_Empty(t *testing.T) {
	t.Parallel()

	h := handlers.NewJobsHandler(&mockJobsProvider{latestRuns: nil})

	_, api := humatest.New(t)
	handlers.RegisterJobRoutes(api, h)

	resp := api.Get("/api/v1/jobs")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "[]")
}

func TestListJobs_Error(t *testing.T) {
	t.Parallel()

	h := handlers.NewJobsHandler(&mockJobsProvider{err: errors.New("db error")})

	_, api := humatest.New(t)
	handlers.RegisterJobRoutes(api, h)

	resp := api.Get("/api/v1/jobs")
	require.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.Contains(t, resp.Body.String(), "listing jobs failed")
}

func TestGetJobHistory_Success(t *testing.T) {
	t.Parallel()

	runs := []domain.JobRun{
		sampleJobRun("ingestion", "succeeded"),
		sampleJobRun("ingestion", "failed"),
	}
	h := handlers.NewJobsHandler(&mockJobsProvider{history: runs})

	_, api := humatest.New(t)
	handlers.RegisterJobRoutes(api, h)

	resp := api.Get("/api/v1/jobs/ingestion")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "ingestion")
}

func TestGetJobHistory_Error(t *testing.T) {
	t.Parallel()

	h := handlers.NewJobsHandler(&mockJobsProvider{err: errors.New("db error")})

	_, api := humatest.New(t)
	handlers.RegisterJobRoutes(api, h)

	resp := api.Get("/api/v1/jobs/ingestion")
	require.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.Contains(t, resp.Body.String(), "fetching job history failed")
}
