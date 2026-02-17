package ebay_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/ebay"
)

func TestRateLimiter_Wait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rate    float64
		burst   int
		daily   int64
		calls   int
		wantErr bool
	}{
		{
			name:  "allows calls within rate",
			rate:  100,
			burst: 10,
			daily: 5000,
			calls: 3,
		},
		{
			name:  "allows burst",
			rate:  100,
			burst: 5,
			daily: 5000,
			calls: 5,
		},
		{
			name:    "rejects when daily limit reached",
			rate:    100,
			burst:   10,
			daily:   2,
			calls:   3,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rl := ebay.NewRateLimiter(tt.rate, tt.burst, tt.daily)

			var lastErr error
			for range tt.calls {
				lastErr = rl.Wait(context.Background())
				if lastErr != nil {
					break
				}
			}

			if tt.wantErr {
				require.Error(t, lastErr)
				assert.ErrorIs(t, lastErr, ebay.ErrDailyLimitReached)
			} else {
				require.NoError(t, lastErr)
			}
		})
	}
}

func TestRateLimiter_DailyCount(t *testing.T) {
	t.Parallel()

	rl := ebay.NewRateLimiter(100, 10, 5000)

	assert.Equal(t, int64(0), rl.DailyCount())

	require.NoError(t, rl.Wait(context.Background()))
	assert.Equal(t, int64(1), rl.DailyCount())

	require.NoError(t, rl.Wait(context.Background()))
	assert.Equal(t, int64(2), rl.DailyCount())
}

func TestRateLimiter_DailyReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	currentTime := now

	rl := ebay.NewRateLimiter(
		100, 10, 5000,
		ebay.WithRateLimiterNowFunc(func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return currentTime
		}),
	)

	// Make some calls.
	require.NoError(t, rl.Wait(context.Background()))
	require.NoError(t, rl.Wait(context.Background()))
	assert.Equal(t, int64(2), rl.DailyCount())

	// Advance past the 24-hour rolling window.
	mu.Lock()
	currentTime = now.Add(25 * time.Hour)
	mu.Unlock()

	// Counter should reset on next call.
	require.NoError(t, rl.Wait(context.Background()))
	assert.Equal(t, int64(1), rl.DailyCount())
}

func TestRateLimiter_ContextCanceled(t *testing.T) {
	t.Parallel()

	// Very slow rate limiter — 1 per 10 seconds, burst 1.
	rl := ebay.NewRateLimiter(0.1, 1, 5000)

	// First call should succeed (uses burst).
	require.NoError(t, rl.Wait(context.Background()))

	// Second call with canceled context should fail.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rl.Wait(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limiter wait")
}

func TestRateLimiter_MaxDaily(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		maxDaily int64
	}{
		{name: "default limit", maxDaily: 5000},
		{name: "custom limit", maxDaily: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rl := ebay.NewRateLimiter(5, 10, tt.maxDaily)
			assert.Equal(t, tt.maxDaily, rl.MaxDaily())
		})
	}
}

func TestRateLimiter_Remaining(t *testing.T) {
	t.Parallel()

	rl := ebay.NewRateLimiter(100, 10, 5)

	assert.Equal(t, int64(5), rl.Remaining())

	require.NoError(t, rl.Wait(context.Background()))
	assert.Equal(t, int64(4), rl.Remaining())

	// Exhaust the limit.
	for range 4 {
		require.NoError(t, rl.Wait(context.Background()))
	}
	assert.Equal(t, int64(0), rl.Remaining())

	// After limit is reached, remaining stays at 0.
	_ = rl.Wait(context.Background())
	assert.Equal(t, int64(0), rl.Remaining())
}

func TestRateLimiter_ResetAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)
	rl := ebay.NewRateLimiter(
		5, 10, 5000,
		ebay.WithRateLimiterNowFunc(func() time.Time { return now }),
	)

	resetAt := rl.ResetAt()
	expected := now.Add(24 * time.Hour)
	assert.Equal(t, expected, resetAt)
}

func TestRateLimiter_ErrDailyLimitReached(t *testing.T) {
	t.Parallel()

	rl := ebay.NewRateLimiter(100, 10, 1)

	// First call succeeds.
	require.NoError(t, rl.Wait(context.Background()))

	// Second call should return ErrDailyLimitReached.
	err := rl.Wait(context.Background())
	require.ErrorIs(t, err, ebay.ErrDailyLimitReached)
	assert.Contains(t, err.Error(), "1/1")
}

func TestRateLimiter_Sync(t *testing.T) {
	t.Parallel()

	rl := ebay.NewRateLimiter(100, 10, 5000)

	// Sync with known eBay Analytics values.
	resetAt := time.Date(2026, 2, 17, 8, 0, 0, 0, time.UTC)
	rl.Sync(110, 5000, resetAt)

	assert.Equal(t, int64(110), rl.DailyCount())
	assert.Equal(t, int64(5000), rl.MaxDaily())
	assert.Equal(t, int64(4890), rl.Remaining())
	assert.Equal(t, resetAt, rl.ResetAt())
}

func TestRateLimiter_Sync_UpdatesLimit(t *testing.T) {
	t.Parallel()

	rl := ebay.NewRateLimiter(100, 10, 5000)
	assert.Equal(t, int64(5000), rl.MaxDaily())

	// eBay reports a different limit.
	resetAt := time.Date(2026, 2, 17, 8, 0, 0, 0, time.UTC)
	rl.Sync(50, 10000, resetAt)

	assert.Equal(t, int64(10000), rl.MaxDaily())
	assert.Equal(t, int64(50), rl.DailyCount())
	assert.Equal(t, int64(9950), rl.Remaining())
}

func TestRateLimiter_Sync_ThenWait(t *testing.T) {
	t.Parallel()

	rl := ebay.NewRateLimiter(100, 10, 5000)

	// Sync with count=100 (eBay says 100 calls used).
	// Use a far-future resetAt so checkDailyReset never triggers during the test.
	resetAt := time.Now().Add(24 * time.Hour)
	rl.Sync(100, 5000, resetAt)
	assert.Equal(t, int64(100), rl.DailyCount())

	// Wait() should increment from the synced baseline.
	require.NoError(t, rl.Wait(context.Background()))
	assert.Equal(t, int64(101), rl.DailyCount())
	assert.Equal(t, int64(4899), rl.Remaining())
}

func TestRateLimiter_RollingWindowReset(t *testing.T) {
	t.Parallel()

	start := time.Date(2025, 3, 10, 8, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	currentTime := start

	rl := ebay.NewRateLimiter(
		100, 10, 5000,
		ebay.WithRateLimiterNowFunc(func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return currentTime
		}),
	)

	// Initial reset should be 24h from start.
	assert.Equal(t, start.Add(24*time.Hour), rl.ResetAt())

	require.NoError(t, rl.Wait(context.Background()))
	assert.Equal(t, int64(1), rl.DailyCount())

	// Advance 23 hours — should NOT reset (still within window).
	mu.Lock()
	currentTime = start.Add(23 * time.Hour)
	mu.Unlock()

	require.NoError(t, rl.Wait(context.Background()))
	assert.Equal(t, int64(2), rl.DailyCount())

	// Advance past 24 hours — should reset.
	mu.Lock()
	currentTime = start.Add(25 * time.Hour)
	mu.Unlock()

	require.NoError(t, rl.Wait(context.Background()))
	assert.Equal(t, int64(1), rl.DailyCount())

	// New window should start from the reset time.
	newResetAt := rl.ResetAt()
	assert.Equal(t, currentTime.Add(24*time.Hour), newResetAt)
}
