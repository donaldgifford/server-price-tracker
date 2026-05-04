package judge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// Store is the subset of internal/store.Store the judge worker needs.
// Defined here so pkg/judge stays free of internal/* imports — any
// implementation that satisfies these four methods can drive the
// worker, including in-memory test fakes.
type Store interface {
	ListAlertsForJudging(ctx context.Context, q *JudgeStoreQuery) ([]domain.JudgeCandidate, error)
	InsertJudgeScore(ctx context.Context, s *domain.JudgeScore) error
	SumJudgeCostSince(ctx context.Context, since time.Time) (float64, error)
}

// JudgeStoreQuery shadows internal/store.JudgeCandidatesQuery so the
// worker doesn't import internal/* — kept structurally identical so a
// thin adapter in serve.go converts between the two without losing
// information.
type JudgeStoreQuery struct {
	Lookback time.Duration
	Limit    int
}

// MetricsRecorder receives the per-run counter increments. Concrete
// implementation lives in internal/metrics; satisfied by the
// promauto-wrapped vecs there. Tests pass a noop or in-memory recorder
// to verify the worker emits the right increments without booting
// Prometheus.
type MetricsRecorder interface {
	RecordVerdict(verdict string)
	RecordScore(componentType string, score float64)
	RecordCost(model string, costUSD float64)
	RecordBudgetExhausted()
}

// Worker is the cron-driven LLM-as-judge runner. Per tick:
//  1. Compute today's spend so far via Store.SumJudgeCostSince.
//  2. If already over budget, emit budget_exhausted and return.
//  3. Pull a batch of un-judged alerts inside Lookback.
//  4. For each alert: render AlertContext, call Judge, persist
//     verdict, optionally write a Langfuse score on the alert's
//     trace.
//
// Concurrency: today the worker processes alerts sequentially per
// tick. Parallelism would yield faster catch-up but complicates budget
// accounting (each call must check + reserve atomically). Sequential
// is sufficient for current alert volumes; revisit if backlog
// regularly exceeds the per-tick budget.
type Worker struct {
	judge          Judge
	store          Store
	metrics        MetricsRecorder
	lf             langfuse.Client
	logger         *slog.Logger
	lookback       time.Duration
	batchSize      int
	dailyBudgetUSD float64
}

// WorkerConfig is the construction-time bag for Worker. All fields are
// optional except Judge and Store; sane defaults applied otherwise.
type WorkerConfig struct {
	Judge          Judge
	Store          Store
	Metrics        MetricsRecorder
	Langfuse       langfuse.Client
	Logger         *slog.Logger
	Lookback       time.Duration
	BatchSize      int
	DailyBudgetUSD float64
}

// NewWorker validates config and returns a Worker. Judge + Store are
// required; the rest fall back to no-op / sane defaults so a minimal
// test setup can construct one with just those two.
//
// Takes cfg by pointer because the struct is heavy (~96 bytes) — the
// gocritic hugeParam threshold flags the value form. The deref makes a
// copy for the Worker so caller mutations after construction don't
// leak in.
func NewWorker(cfg *WorkerConfig) (*Worker, error) {
	if cfg.Judge == nil {
		return nil, errors.New("judge.NewWorker: Judge is required")
	}
	if cfg.Store == nil {
		return nil, errors.New("judge.NewWorker: Store is required")
	}
	w := &Worker{
		judge:          cfg.Judge,
		store:          cfg.Store,
		metrics:        cfg.Metrics,
		lf:             cfg.Langfuse,
		logger:         cfg.Logger,
		lookback:       cfg.Lookback,
		batchSize:      cfg.BatchSize,
		dailyBudgetUSD: cfg.DailyBudgetUSD,
	}
	if w.metrics == nil {
		w.metrics = noopMetrics{}
	}
	if w.lf == nil {
		w.lf = langfuse.NoopClient{}
	}
	if w.logger == nil {
		w.logger = slog.Default()
	}
	if w.lookback <= 0 {
		w.lookback = 6 * time.Hour
	}
	if w.batchSize <= 0 {
		w.batchSize = 50
	}
	if w.dailyBudgetUSD < 0 {
		w.dailyBudgetUSD = 0
	}
	return w, nil
}

// Run executes one full judge tick: budget check → list candidates →
// per-alert evaluate → persist + optionally score on Langfuse. Returns
// the number of alerts judged this tick (regardless of outcome) plus
// the first error encountered, if any. Subsequent errors are logged
// but don't stop the loop — one bad LLM call shouldn't poison the
// whole batch.
//
// Returns ErrJudgeBudgetExhausted as the error when the budget cap
// halts the run early; callers (cron / HTTP backfill) treat this as
// an info-level outcome, not a real error.
func (w *Worker) Run(ctx context.Context) (int, error) {
	if w.dailyBudgetUSD > 0 {
		spent, err := w.store.SumJudgeCostSince(ctx, todayUTCMidnight())
		if err != nil {
			return 0, fmt.Errorf("checking judge budget: %w", err)
		}
		if spent >= w.dailyBudgetUSD {
			w.metrics.RecordBudgetExhausted()
			w.logger.Warn("judge daily budget exhausted",
				"spent_usd", spent, "budget_usd", w.dailyBudgetUSD)
			return 0, ErrJudgeBudgetExhausted
		}
	}

	candidates, err := w.store.ListAlertsForJudging(ctx, &JudgeStoreQuery{
		Lookback: w.lookback,
		Limit:    w.batchSize,
	})
	if err != nil {
		return 0, fmt.Errorf("listing judge candidates: %w", err)
	}

	judged := 0
	var firstErr error
	for i := range candidates {
		// In-loop budget recheck: a long-running batch could cross the
		// cap mid-flight. Cheap query (indexed scan over today's slice)
		// versus an expensive LLM call.
		if w.dailyBudgetUSD > 0 {
			spent, sumErr := w.store.SumJudgeCostSince(ctx, todayUTCMidnight())
			if sumErr == nil && spent >= w.dailyBudgetUSD {
				w.metrics.RecordBudgetExhausted()
				w.logger.Warn("judge daily budget exhausted mid-batch",
					"spent_usd", spent, "budget_usd", w.dailyBudgetUSD,
					"remaining_in_batch", len(candidates)-i)
				return judged, ErrJudgeBudgetExhausted
			}
		}
		if err := w.judgeOne(ctx, &candidates[i]); err != nil {
			w.logger.Warn("judge evaluate failed (continuing)",
				"alert_id", candidates[i].AlertID, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		judged++
	}
	return judged, firstErr
}

// judgeOne is one alert's path through the worker — extracted so the
// outer loop stays linear and so error wrapping has one consistent
// shape per call site.
func (w *Worker) judgeOne(ctx context.Context, c *domain.JudgeCandidate) error {
	ac := candidateToContext(c)
	v, err := w.judge.EvaluateAlert(ctx, &ac)
	if err != nil {
		return fmt.Errorf("evaluating alert %s: %w", c.AlertID, err)
	}

	score := &domain.JudgeScore{
		AlertID:      c.AlertID,
		Score:        v.Score,
		Reason:       v.Reason,
		Model:        v.Model,
		InputTokens:  v.Tokens.InputTokens,
		OutputTokens: v.Tokens.OutputTokens,
		CostUSD:      v.CostUSD,
	}
	if err := w.store.InsertJudgeScore(ctx, score); err != nil {
		return fmt.Errorf("persisting judge score for %s: %w", c.AlertID, err)
	}

	w.metrics.RecordVerdict(verdictBucket(v.Score))
	w.metrics.RecordScore(string(c.ComponentType), v.Score)
	if v.CostUSD > 0 && v.Model != "" {
		w.metrics.RecordCost(v.Model, v.CostUSD)
	}

	// Langfuse Score is best-effort — buffered enqueue swallows
	// transient failures without surfacing them here.
	if c.TraceID != nil && *c.TraceID != "" {
		if scoreErr := w.lf.Score(ctx, *c.TraceID, "judge_alert_quality", v.Score, v.Reason); scoreErr != nil {
			w.logger.Debug("langfuse Score (judge) failed", "alert_id", c.AlertID, "error", scoreErr)
		}
	}
	return nil
}

// candidateToContext converts a store-side row into the prompt-side
// AlertContext shape. Single-purpose adapter; lives next to the worker
// so changes to either surface stay co-located.
func candidateToContext(c *domain.JudgeCandidate) AlertContext {
	traceID := ""
	if c.TraceID != nil {
		traceID = *c.TraceID
	}
	return AlertContext{
		AlertID:       c.AlertID,
		WatchName:     c.WatchName,
		ComponentType: c.ComponentType,
		ListingTitle:  c.ListingTitle,
		Condition:     c.Condition,
		PriceUSD:      c.PriceUSD,
		BaselineP25:   c.BaselineP25,
		BaselineP50:   c.BaselineP50,
		BaselineP75:   c.BaselineP75,
		SampleSize:    c.SampleSize,
		Score:         c.Score,
		Threshold:     c.Threshold,
		TraceID:       traceID,
		CreatedAt:     c.CreatedAt,
	}
}

// verdictBucket buckets a 0.0-1.0 verdict into the "deal" / "edge" /
// "noise" labels used as the verdict metric label. Three buckets is
// the sweet spot — fine-grained enough to track distribution shifts,
// coarse enough that the operator can pattern-match in a Grafana
// panel without squinting.
func verdictBucket(score float64) string {
	switch {
	case score >= 0.7:
		return "deal"
	case score >= 0.3:
		return "edge"
	default:
		return "noise"
	}
}

// todayUTCMidnight returns the UTC midnight of "today" so daily-budget
// arithmetic is unambiguous regardless of the deployment's local TZ.
// Hot-path in Run; pulled out so tests can wrap a clock if needed.
func todayUTCMidnight() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// noopMetrics is the default MetricsRecorder when callers don't pass
// one. Lets unit tests construct a Worker without booting the real
// metrics package.
type noopMetrics struct{}

func (noopMetrics) RecordVerdict(string)        {}
func (noopMetrics) RecordScore(string, float64) {}
func (noopMetrics) RecordCost(string, float64)  {}
func (noopMetrics) RecordBudgetExhausted()      {}
