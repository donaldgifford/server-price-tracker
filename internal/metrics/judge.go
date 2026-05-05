package metrics

// JudgeRecorder adapts the package-level Prometheus vecs to the
// judge.MetricsRecorder interface so the judge worker can stay free of
// Prometheus imports. The package-level vecs are the source of truth;
// this struct is just a method-shaped wrapper that pkg/judge can hold
// by interface.
type JudgeRecorder struct{}

// RecordVerdict bumps the judge_evaluations_total counter for the
// supplied verdict bucket label.
func (JudgeRecorder) RecordVerdict(verdict string) {
	JudgeEvaluationsTotal.WithLabelValues(verdict).Inc()
}

// RecordScore observes the judge's per-component quality score
// distribution.
func (JudgeRecorder) RecordScore(componentType string, score float64) {
	JudgeScore.WithLabelValues(componentType).Observe(score)
}

// RecordCost adds USD spent on the supplied judge model. Operators
// alert on a daily integral against the configured budget cap.
func (JudgeRecorder) RecordCost(model string, costUSD float64) {
	JudgeCostUSDTotal.WithLabelValues(model).Add(costUSD)
}

// RecordBudgetExhausted increments the cap-hit counter; the worker
// calls this once per tick that short-circuits.
func (JudgeRecorder) RecordBudgetExhausted() {
	JudgeBudgetExhaustedTotal.Inc()
}
