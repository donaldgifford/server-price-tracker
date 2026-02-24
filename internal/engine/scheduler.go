package engine

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	"github.com/donaldgifford/server-price-tracker/internal/store"
)

// Scheduler manages periodic ingestion and baseline refresh tasks.
type Scheduler struct {
	cron                 *cron.Cron
	engine               *Engine
	store                store.Store
	hostname             string
	log                  *slog.Logger
	reExtractionInterval time.Duration
}

// NewScheduler creates a new Scheduler that runs engine tasks on a schedule.
func NewScheduler(
	eng *Engine,
	s store.Store,
	ingestionInterval time.Duration,
	baselineInterval time.Duration,
	reExtractionInterval time.Duration,
	log *slog.Logger,
) (*Scheduler, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	c := cron.New()

	sched := &Scheduler{
		cron:                 c,
		engine:               eng,
		store:                s,
		hostname:             hostname,
		log:                  log,
		reExtractionInterval: reExtractionInterval,
	}

	if _, err = c.AddFunc("@every "+ingestionInterval.String(), sched.runIngestion); err != nil {
		return nil, err
	}

	if _, err = c.AddFunc("@every "+baselineInterval.String(), sched.runBaselineRefresh); err != nil {
		return nil, err
	}

	if reExtractionInterval > 0 {
		if _, err = c.AddFunc("@every "+reExtractionInterval.String(), sched.runReExtraction); err != nil {
			return nil, err
		}
	}

	return sched, nil
}

// Start begins running scheduled tasks.
func (s *Scheduler) Start() {
	s.log.Info("scheduler started")
	s.cron.Start()
}

// Stop gracefully stops the scheduler, waiting for running jobs to finish.
func (s *Scheduler) Stop() context.Context {
	s.log.Info("scheduler stopping")
	return s.cron.Stop()
}

// Entries returns the registered cron entries for inspection.
func (s *Scheduler) Entries() []cron.Entry {
	return s.cron.Entries()
}

// RecoverStaleJobRuns marks any running job rows that started before 2 hours ago
// as crashed, and deletes rows older than 30 days. Called at startup.
func (s *Scheduler) RecoverStaleJobRuns(ctx context.Context) {
	affected, err := s.store.RecoverStaleJobRuns(ctx, 2*time.Hour)
	if err != nil {
		s.log.Warn("failed to recover stale job runs", "error", err)
		return
	}
	if affected > 0 {
		s.log.Info("recovered stale job runs as crashed", "count", affected)
	}
}

// runJob wraps a job function with distributed locking and DB-backed run tracking.
// If the lock is already held by another instance, the job is skipped silently.
func (s *Scheduler) runJob(
	ctx context.Context,
	jobName string,
	ttl time.Duration,
	fn func(context.Context) error,
) error {
	acquired, err := s.store.AcquireSchedulerLock(ctx, jobName, s.hostname, ttl)
	if err != nil {
		s.log.Error("failed to acquire scheduler lock", "job", jobName, "error", err)
		return nil
	}
	if !acquired {
		s.log.Info("scheduler lock held by another instance, skipping", "job", jobName)
		return nil
	}

	runID, err := s.store.InsertJobRun(ctx, jobName)
	if err != nil {
		s.log.Error("failed to record job start", "job", jobName, "error", err)
		if releaseErr := s.store.ReleaseSchedulerLock(ctx, jobName, s.hostname); releaseErr != nil {
			s.log.Warn("failed to release scheduler lock", "job", jobName, "error", releaseErr)
		}
		return fn(ctx)
	}

	status := "succeeded"
	errText := ""

	defer func() {
		if releaseErr := s.store.ReleaseSchedulerLock(ctx, jobName, s.hostname); releaseErr != nil {
			s.log.Warn("failed to release scheduler lock", "job", jobName, "error", releaseErr)
		}
		if completeErr := s.store.CompleteJobRun(ctx, runID, status, errText, 0); completeErr != nil {
			s.log.Warn("failed to record job completion", "job", jobName, "error", completeErr)
		}
	}()

	if runErr := fn(ctx); runErr != nil {
		status = "failed"
		errText = runErr.Error()
		return runErr
	}

	return nil
}

func (s *Scheduler) runIngestion() {
	ctx := context.Background()
	s.log.Info("scheduled ingestion starting")
	if err := s.runJob(ctx, "ingestion", 30*time.Minute, s.engine.RunIngestion); err != nil {
		s.log.Error("scheduled ingestion failed", "error", err)
	} else {
		metrics.IngestionLastSuccessTimestamp.Set(float64(time.Now().Unix()))
	}
}

func (s *Scheduler) runBaselineRefresh() {
	ctx := context.Background()
	s.log.Info("scheduled baseline refresh starting")
	if err := s.runJob(ctx, "baseline_refresh", 60*time.Minute, s.engine.RunBaselineRefresh); err != nil {
		s.log.Error("scheduled baseline refresh failed", "error", err)
	} else {
		metrics.BaselineLastRefreshTimestamp.Set(float64(time.Now().Unix()))
	}
}

func (s *Scheduler) runReExtraction() {
	ctx := context.Background()
	s.log.Info("scheduled re-extraction starting")
	fn := func(ctx context.Context) error {
		count, err := s.engine.RunReExtraction(ctx, "", 100)
		if err != nil {
			return err
		}
		s.log.Info("scheduled re-extraction completed", "re_extracted", count)
		return nil
	}
	if err := s.runJob(ctx, "re_extraction", 30*time.Minute, fn); err != nil {
		s.log.Error("scheduled re-extraction failed", "error", err)
	}
}
