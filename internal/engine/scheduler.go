package engine

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler manages periodic ingestion and baseline refresh tasks.
type Scheduler struct {
	cron   *cron.Cron
	engine *Engine
	log    *slog.Logger
}

// NewScheduler creates a new Scheduler that runs engine tasks on a schedule.
func NewScheduler(
	eng *Engine,
	ingestionInterval time.Duration,
	baselineInterval time.Duration,
	log *slog.Logger,
) (*Scheduler, error) {
	c := cron.New()

	s := &Scheduler{
		cron:   c,
		engine: eng,
		log:    log,
	}

	if _, err := c.AddFunc(
		"@every "+ingestionInterval.String(),
		s.runIngestion,
	); err != nil {
		return nil, err
	}

	if _, err := c.AddFunc(
		"@every "+baselineInterval.String(),
		s.runBaselineRefresh,
	); err != nil {
		return nil, err
	}

	return s, nil
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

func (s *Scheduler) runIngestion() {
	ctx := context.Background()
	s.log.Info("scheduled ingestion starting")
	if err := s.engine.RunIngestion(ctx); err != nil {
		s.log.Error("scheduled ingestion failed", "error", err)
	}
}

func (s *Scheduler) runBaselineRefresh() {
	ctx := context.Background()
	s.log.Info("scheduled baseline refresh starting")
	if err := s.engine.RunBaselineRefresh(ctx); err != nil {
		s.log.Error("scheduled baseline refresh failed", "error", err)
	}
}
