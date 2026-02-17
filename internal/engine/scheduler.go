package engine

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
)

// Scheduler manages periodic ingestion and baseline refresh tasks.
type Scheduler struct {
	cron                 *cron.Cron
	engine               *Engine
	log                  *slog.Logger
	ingestionEntryID     cron.EntryID
	baselineEntryID      cron.EntryID
	reExtractionEntryID  cron.EntryID
	reExtractionInterval time.Duration
}

// NewScheduler creates a new Scheduler that runs engine tasks on a schedule.
func NewScheduler(
	eng *Engine,
	ingestionInterval time.Duration,
	baselineInterval time.Duration,
	reExtractionInterval time.Duration,
	log *slog.Logger,
) (*Scheduler, error) {
	c := cron.New()

	s := &Scheduler{
		cron:                 c,
		engine:               eng,
		log:                  log,
		reExtractionInterval: reExtractionInterval,
	}

	ingestionID, err := c.AddFunc(
		"@every "+ingestionInterval.String(),
		s.runIngestion,
	)
	if err != nil {
		return nil, err
	}
	s.ingestionEntryID = ingestionID

	baselineID, err := c.AddFunc(
		"@every "+baselineInterval.String(),
		s.runBaselineRefresh,
	)
	if err != nil {
		return nil, err
	}
	s.baselineEntryID = baselineID

	if reExtractionInterval > 0 {
		reExtractID, reErr := c.AddFunc(
			"@every "+reExtractionInterval.String(),
			s.runReExtraction,
		)
		if reErr != nil {
			return nil, reErr
		}
		s.reExtractionEntryID = reExtractID
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
	} else {
		metrics.IngestionLastSuccessTimestamp.Set(float64(time.Now().Unix()))
	}
	s.SyncNextRunTimestamps()
}

func (s *Scheduler) runBaselineRefresh() {
	ctx := context.Background()
	s.log.Info("scheduled baseline refresh starting")
	if err := s.engine.RunBaselineRefresh(ctx); err != nil {
		s.log.Error("scheduled baseline refresh failed", "error", err)
	} else {
		metrics.BaselineLastRefreshTimestamp.Set(float64(time.Now().Unix()))
	}
	s.SyncNextRunTimestamps()
}

func (s *Scheduler) runReExtraction() {
	ctx := context.Background()
	s.log.Info("scheduled re-extraction starting")
	count, err := s.engine.RunReExtraction(ctx, "", 100)
	if err != nil {
		s.log.Error("scheduled re-extraction failed", "error", err)
	} else {
		s.log.Info("scheduled re-extraction completed", "re_extracted", count)
	}
	s.SyncNextRunTimestamps()
}

// SyncNextRunTimestamps updates Prometheus gauges with the next scheduled run times.
func (s *Scheduler) SyncNextRunTimestamps() {
	ingestion := s.cron.Entry(s.ingestionEntryID)
	if !ingestion.Next.IsZero() {
		metrics.SchedulerNextIngestionTimestamp.Set(float64(ingestion.Next.Unix()))
	}
	baseline := s.cron.Entry(s.baselineEntryID)
	if !baseline.Next.IsZero() {
		metrics.SchedulerNextBaselineTimestamp.Set(float64(baseline.Next.Unix()))
	}
	if s.reExtractionEntryID != 0 {
		reExtract := s.cron.Entry(s.reExtractionEntryID)
		if !reExtract.Next.IsZero() {
			metrics.SchedulerNextReExtractionTimestamp.Set(float64(reExtract.Next.Unix()))
		}
	}
}
