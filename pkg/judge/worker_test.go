package judge_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/pkg/judge"
	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// fakeJudge returns a canned verdict + records every alert id passed
// through. Lets the worker test assert which alerts were actually
// evaluated without bringing up a real LLM.
type fakeJudge struct {
	verdict judge.Verdict
	err     error
	seen    []string
}

func (f *fakeJudge) EvaluateAlert(_ context.Context, ac *judge.AlertContext) (judge.Verdict, error) {
	f.seen = append(f.seen, ac.AlertID)
	if f.err != nil {
		return judge.Verdict{}, f.err
	}
	return f.verdict, nil
}

// fakeStore is a minimal in-memory JudgeStore for the worker tests.
// Keeps spend totals + persisted scores so the budget-enforcement and
// idempotency paths can be observed without Postgres.
type fakeStore struct {
	candidates []domain.JudgeCandidate
	persisted  []*domain.JudgeScore
	preSpent   float64
	// sumErrAfter, when > 0, makes SumJudgeCostSince return an error
	// after the Nth call (1-indexed). Lets a test simulate the DB
	// dying mid-batch on the in-loop budget recheck.
	sumErrAfter int
	sumCalls    int
}

func (s *fakeStore) ListAlertsForJudging(_ context.Context, q *judge.JudgeStoreQuery) ([]domain.JudgeCandidate, error) {
	limit := q.Limit
	if limit <= 0 || limit > len(s.candidates) {
		limit = len(s.candidates)
	}
	return s.candidates[:limit], nil
}

func (s *fakeStore) InsertJudgeScore(_ context.Context, sc *domain.JudgeScore) error {
	s.persisted = append(s.persisted, sc)
	s.preSpent += sc.CostUSD
	return nil
}

func (s *fakeStore) SumJudgeCostSince(_ context.Context, _ time.Time) (float64, error) {
	s.sumCalls++
	if s.sumErrAfter > 0 && s.sumCalls > s.sumErrAfter {
		return 0, errors.New("simulated DB error on judge_scores SUM")
	}
	return s.preSpent, nil
}

// fakeMetrics records every counter increment so tests assert on
// emission without booting Prometheus.
type fakeMetrics struct {
	verdicts        []string
	costs           map[string]float64
	scores          []float64
	budgetExhausted int
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{costs: map[string]float64{}}
}

func (m *fakeMetrics) RecordVerdict(v string)              { m.verdicts = append(m.verdicts, v) }
func (m *fakeMetrics) RecordScore(_ string, score float64) { m.scores = append(m.scores, score) }
func (m *fakeMetrics) RecordCost(model string, c float64)  { m.costs[model] += c }
func (m *fakeMetrics) RecordBudgetExhausted()              { m.budgetExhausted++ }

// fakeLangfuseClient is the minimal langfuse.Client surface the worker
// uses: just Score. Captures every call so tests can assert.
type fakeLangfuseClient struct {
	scoreCalls []scoreCall
}

type scoreCall struct {
	traceID string
	name    string
	value   float64
}

func (c *fakeLangfuseClient) Score(_ context.Context, traceID, name string, value float64, _ string) error {
	c.scoreCalls = append(c.scoreCalls, scoreCall{traceID, name, value})
	return nil
}

func (*fakeLangfuseClient) LogGeneration(_ context.Context, _ *langfuse.GenerationRecord) error {
	return nil
}

func (*fakeLangfuseClient) CreateTrace(_ context.Context, _ string, _ map[string]string) (langfuse.TraceHandle, error) {
	return langfuse.TraceHandle{}, nil
}

func (*fakeLangfuseClient) CreateDatasetItem(_ context.Context, _ string, _ *langfuse.DatasetItem) error {
	return nil
}

func (*fakeLangfuseClient) CreateDatasetRun(_ context.Context, _ *langfuse.DatasetRun) error {
	return nil
}

func ptrString(s string) *string { return &s }

// candidate makes a JudgeCandidate with sensible defaults so each test
// only spells out the fields it actually depends on.
func candidate(id string) domain.JudgeCandidate {
	return domain.JudgeCandidate{
		AlertID:       id,
		WatchID:       "watch-1",
		WatchName:     "Test watch",
		ListingID:     "listing-" + id,
		ListingTitle:  "Dell PowerEdge R740xd",
		ComponentType: domain.ComponentServer,
		Condition:     domain.ConditionUsedWorking,
		PriceUSD:      650,
		BaselineP25:   700,
		BaselineP50:   850,
		BaselineP75:   1100,
		SampleSize:    35,
		Score:         85,
		Threshold:     80,
		TraceID:       ptrString("trace-" + id),
		CreatedAt:     time.Now().Add(-1 * time.Hour),
	}
}

// TestWorker_Run_HappyPath: 3 candidates, no budget cap → all judged,
// metrics + Langfuse scores emitted for each.
func TestWorker_Run_HappyPath(t *testing.T) {
	t.Parallel()

	s := &fakeStore{candidates: []domain.JudgeCandidate{
		candidate("a"), candidate("b"), candidate("c"),
	}}
	j := &fakeJudge{verdict: judge.Verdict{Score: 0.8, Reason: "deal", Model: "claude-haiku-4-5", CostUSD: 0.001}}
	m := newFakeMetrics()
	lf := &fakeLangfuseClient{}

	w, err := judge.NewWorker(&judge.WorkerConfig{Judge: j, Store: s, Metrics: m, Langfuse: lf})
	require.NoError(t, err)

	n, err := w.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Len(t, s.persisted, 3, "every candidate must be persisted")
	assert.Equal(t, []string{"deal", "deal", "deal"}, m.verdicts)
	assert.Len(t, lf.scoreCalls, 3, "every candidate with a trace_id must get a Langfuse score")
	assert.InDelta(t, 0.003, m.costs["claude-haiku-4-5"], 0.0001)
}

// TestWorker_Run_SkipsAlertsWithoutTrace: candidate without trace_id
// is still judged + persisted, but no Langfuse score is posted (no
// anchor).
func TestWorker_Run_SkipsAlertsWithoutTrace(t *testing.T) {
	t.Parallel()

	c := candidate("no-trace")
	c.TraceID = nil
	s := &fakeStore{candidates: []domain.JudgeCandidate{c}}
	j := &fakeJudge{verdict: judge.Verdict{Score: 0.5, Model: "m"}}
	lf := &fakeLangfuseClient{}

	w, err := judge.NewWorker(&judge.WorkerConfig{Judge: j, Store: s, Langfuse: lf})
	require.NoError(t, err)

	n, err := w.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "candidate must still be judged + persisted")
	assert.Len(t, s.persisted, 1)
	assert.Empty(t, lf.scoreCalls, "trace_id absent → no Langfuse score")
}

// TestWorker_Run_BudgetAlreadyExhausted: pre-spent ≥ budget → return
// ErrJudgeBudgetExhausted before listing candidates. Counter
// increments once.
func TestWorker_Run_BudgetAlreadyExhausted(t *testing.T) {
	t.Parallel()

	s := &fakeStore{
		candidates: []domain.JudgeCandidate{candidate("a")},
		preSpent:   12.0, // > $10 cap
	}
	j := &fakeJudge{}
	m := newFakeMetrics()

	w, err := judge.NewWorker(&judge.WorkerConfig{
		Judge:          j,
		Store:          s,
		Metrics:        m,
		DailyBudgetUSD: 10.0,
	})
	require.NoError(t, err)

	n, err := w.Run(context.Background())
	require.ErrorIs(t, err, judge.ErrJudgeBudgetExhausted)
	assert.Zero(t, n, "no candidates should be judged when budget is exhausted")
	assert.Empty(t, j.seen, "judge must not be invoked when budget is over")
	assert.Equal(t, 1, m.budgetExhausted)
}

// TestWorker_Run_BudgetCrossedMidBatch: batch of 3 each costing $5 —
// after 2 verdicts spend = $10 = $10 cap; the 3rd recheck sees the cap
// is met and short-circuits the rest. We expect 2 judged + budget
// metric. The "≥" comparison is deliberate: hitting exactly the cap
// halts the next call so we never actually exceed it.
func TestWorker_Run_BudgetCrossedMidBatch(t *testing.T) {
	t.Parallel()

	s := &fakeStore{candidates: []domain.JudgeCandidate{
		candidate("a"), candidate("b"), candidate("c"),
	}}
	j := &fakeJudge{verdict: judge.Verdict{Score: 0.5, Model: "m", CostUSD: 5.0}}
	m := newFakeMetrics()

	w, err := judge.NewWorker(&judge.WorkerConfig{
		Judge:          j,
		Store:          s,
		Metrics:        m,
		DailyBudgetUSD: 10.0,
	})
	require.NoError(t, err)

	n, err := w.Run(context.Background())
	require.ErrorIs(t, err, judge.ErrJudgeBudgetExhausted)
	assert.Equal(t, 2, n, "should judge 2 alerts before mid-batch recheck cuts off the 3rd")
	assert.Equal(t, 1, m.budgetExhausted)
}

// TestWorker_Run_HaltsOnBudgetRecheckDBError: the in-loop budget
// recheck must halt the tick on a DB error. Silently continuing
// would defeat the budget guarantee — see INV-0001 MEDIUM-8.
func TestWorker_Run_HaltsOnBudgetRecheckDBError(t *testing.T) {
	t.Parallel()

	// 3 candidates; first SumJudgeCostSince call (Run preflight) is
	// fine; the in-loop recheck is the *second* call → it errors.
	s := &fakeStore{
		candidates:  []domain.JudgeCandidate{candidate("a"), candidate("b"), candidate("c")},
		sumErrAfter: 1,
	}
	j := &fakeJudge{verdict: judge.Verdict{Score: 0.5, Model: "m", CostUSD: 5.0}}
	m := newFakeMetrics()

	w, err := judge.NewWorker(&judge.WorkerConfig{
		Judge:          j,
		Store:          s,
		Metrics:        m,
		DailyBudgetUSD: 100.0, // well above what 3 candidates would spend
	})
	require.NoError(t, err)

	n, err := w.Run(context.Background())
	require.Error(t, err)
	require.NotErrorIs(t, err, judge.ErrJudgeBudgetExhausted,
		"halt on DB error must NOT masquerade as budget exhaustion")
	assert.Contains(t, err.Error(), "judge budget recheck")
	assert.Equal(t, 0, n,
		"in-loop recheck fires before the first judgement → 0 alerts judged")
	assert.Equal(t, 0, m.budgetExhausted,
		"budget metric must not increment for DB-error halts")
}

// TestWorker_Run_JudgeErrorContinues: one alert blows up; the worker
// logs + continues. Persisted count = 2 (one failed), error returned.
func TestWorker_Run_JudgeErrorContinues(t *testing.T) {
	t.Parallel()

	s := &fakeStore{candidates: []domain.JudgeCandidate{
		candidate("a"), candidate("b"), candidate("c"),
	}}
	// Custom judge that fails the second alert only.
	calls := 0
	j := &flakyJudge{fn: func(_ context.Context, ac *judge.AlertContext) (judge.Verdict, error) {
		calls++
		if ac.AlertID == "b" {
			return judge.Verdict{}, errors.New("LLM blew up")
		}
		return judge.Verdict{Score: 0.6, Model: "m"}, nil
	}}

	w, err := judge.NewWorker(&judge.WorkerConfig{Judge: j, Store: s})
	require.NoError(t, err)

	n, err := w.Run(context.Background())
	require.Error(t, err) // first error surfaced
	assert.Contains(t, err.Error(), "LLM blew up")
	assert.Equal(t, 2, n)
	assert.Equal(t, 3, calls, "judge must be invoked for every candidate even when one fails")
}

// flakyJudge lets a test customise per-alert behaviour without copying
// the fakeJudge struct. Pulled out so the per-test fixture stays
// readable.
type flakyJudge struct {
	fn func(context.Context, *judge.AlertContext) (judge.Verdict, error)
}

func (f *flakyJudge) EvaluateAlert(ctx context.Context, ac *judge.AlertContext) (judge.Verdict, error) {
	return f.fn(ctx, ac)
}

// TestWorker_NewWorker_ValidatesRequired: missing Judge or Store →
// construction error so config bugs surface at startup, not per-tick.
func TestWorker_NewWorker_ValidatesRequired(t *testing.T) {
	t.Parallel()

	_, err := judge.NewWorker(&judge.WorkerConfig{Store: &fakeStore{}})
	require.Error(t, err)

	_, err = judge.NewWorker(&judge.WorkerConfig{Judge: &fakeJudge{}})
	require.Error(t, err)

	w, err := judge.NewWorker(&judge.WorkerConfig{Judge: &fakeJudge{}, Store: &fakeStore{}})
	require.NoError(t, err)
	require.NotNil(t, w)
}
