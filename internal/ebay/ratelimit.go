package ebay

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter controls API call rate and daily usage limits.
type RateLimiter struct {
	limiter  *rate.Limiter
	daily    atomic.Int64
	maxDaily int64
	resetAt  time.Time
	mu       sync.Mutex
	nowFunc  func() time.Time
}

// RateLimiterOption configures the RateLimiter.
type RateLimiterOption func(*RateLimiter)

// WithRateLimiterNowFunc overrides the time function for testing.
func WithRateLimiterNowFunc(f func() time.Time) RateLimiterOption {
	return func(r *RateLimiter) {
		r.nowFunc = f
	}
}

// NewRateLimiter creates a rate limiter with the given per-second rate,
// burst size, and daily limit.
func NewRateLimiter(
	perSecond float64,
	burst int,
	maxDaily int64,
	opts ...RateLimiterOption,
) *RateLimiter {
	r := &RateLimiter{
		limiter:  rate.NewLimiter(rate.Limit(perSecond), burst),
		maxDaily: maxDaily,
		nowFunc:  time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}
	r.resetAt = nextMidnight(r.nowFunc())
	return r
}

// Wait blocks until the rate limiter allows the call, or the context is canceled.
// Returns an error if the daily limit has been reached.
func (r *RateLimiter) Wait(ctx context.Context) error {
	r.checkDailyReset()

	if r.daily.Load() >= r.maxDaily {
		return fmt.Errorf(
			"daily API limit reached (%d/%d)",
			r.daily.Load(),
			r.maxDaily,
		)
	}

	if err := r.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter wait: %w", err)
	}

	r.daily.Add(1)
	return nil
}

// DailyCount returns the current daily call count.
func (r *RateLimiter) DailyCount() int64 {
	return r.daily.Load()
}

func (r *RateLimiter) checkDailyReset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.nowFunc()
	if now.After(r.resetAt) {
		r.daily.Store(0)
		r.resetAt = nextMidnight(now)
	}
}

func nextMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, t.Location())
}
