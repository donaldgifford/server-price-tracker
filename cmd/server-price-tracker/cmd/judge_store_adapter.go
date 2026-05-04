package cmd

import (
	"context"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/store"
	"github.com/donaldgifford/server-price-tracker/pkg/judge"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// judgeStoreAdapter satisfies pkg/judge.Store against an
// internal/store.Store. The two interfaces have the same shape but
// different package boundaries — pkg/judge can't import internal/* —
// so this adapter does the structural translation. Single thin layer;
// holds nothing other than the wrapped store.
type judgeStoreAdapter struct {
	inner store.Store
}

func newJudgeStoreAdapter(s store.Store) *judgeStoreAdapter {
	return &judgeStoreAdapter{inner: s}
}

// ListAlertsForJudging copies the cap fields onto the inner query type
// and forwards. The inner Store applies its own defaults when zero
// values come through.
func (a *judgeStoreAdapter) ListAlertsForJudging(ctx context.Context, q *judge.JudgeStoreQuery) ([]domain.JudgeCandidate, error) {
	return a.inner.ListAlertsForJudging(ctx, &store.JudgeCandidatesQuery{
		Lookback: q.Lookback,
		Limit:    q.Limit,
	})
}

func (a *judgeStoreAdapter) InsertJudgeScore(ctx context.Context, sc *domain.JudgeScore) error {
	return a.inner.InsertJudgeScore(ctx, sc)
}

func (a *judgeStoreAdapter) SumJudgeCostSince(ctx context.Context, since time.Time) (float64, error) {
	return a.inner.SumJudgeCostSince(ctx, since)
}
