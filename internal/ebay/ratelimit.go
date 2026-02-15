package ebay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// ErrDailyLimitReached is returned when the daily API call limit has been exhausted.
var ErrDailyLimitReached = errors.New("daily API limit reached")

// RateLimiter controls API call rate and daily usage limits.
// It uses a token bucket for per-second rate limiting and a rolling
// 24-hour window for daily quota tracking.
type RateLimiter struct {
	limiter     *rate.Limiter
	daily       atomic.Int64
	maxDaily    int64
	windowStart time.Time
	resetAt     time.Time
	mu          sync.Mutex
	nowFunc     func() time.Time
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
// burst size, and daily limit. The daily quota uses a rolling 24-hour
// window that resets 24 hours after the first API call in each window.
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
	now := r.nowFunc()
	r.windowStart = now
	r.resetAt = now.Add(24 * time.Hour)
	return r
}

// Wait blocks until the rate limiter allows the call, or the context is canceled.
// Returns ErrDailyLimitReached if the daily limit has been exhausted.
func (r *RateLimiter) Wait(ctx context.Context) error {
	r.checkDailyReset()

	if r.daily.Load() >= r.maxDaily {
		return fmt.Errorf("%w (%d/%d)", ErrDailyLimitReached, r.daily.Load(), r.maxDaily)
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

// MaxDaily returns the configured daily call limit.
func (r *RateLimiter) MaxDaily() int64 {
	return r.maxDaily
}

// Remaining returns the number of API calls remaining in the current
// 24-hour window.
func (r *RateLimiter) Remaining() int64 {
	remaining := r.maxDaily - r.daily.Load()
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ResetAt returns the time when the current 24-hour window expires
// and the daily counter resets.
func (r *RateLimiter) ResetAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.resetAt
}

func (r *RateLimiter) checkDailyReset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.nowFunc()
	if now.After(r.resetAt) {
		r.daily.Store(0)
		r.windowStart = now
		r.resetAt = now.Add(24 * time.Hour)
	}
}
