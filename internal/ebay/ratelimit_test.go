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
				assert.Contains(t, lastErr.Error(), "daily API limit reached")
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

	now := time.Date(2025, 1, 15, 23, 59, 0, 0, time.UTC)
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

	// Advance past midnight.
	mu.Lock()
	currentTime = time.Date(2025, 1, 16, 0, 1, 0, 0, time.UTC)
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
