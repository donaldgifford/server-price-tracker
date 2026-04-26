package notify

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/metrics"
)

// rateLimitState tracks Discord's per-bucket budget derived from response
// headers (X-RateLimit-*). One instance per DiscordNotifier — webhook URL
// owns the bucket per DESIGN-0009. All state is guarded by mu so multiple
// goroutines could share a notifier safely; in practice the engine sends
// alerts serially per watch, so contention is minimal.
type rateLimitState struct {
	mu        sync.Mutex
	bucket    string
	remaining int
	resetAt   time.Time
}

// newRateLimitState returns a state with capacity assumed; the first
// response will populate real numbers.
func newRateLimitState() *rateLimitState {
	return &rateLimitState{remaining: 1}
}

// update absorbs the X-RateLimit-* headers from a Discord response.
// Safe to call on any response, including non-2xx ones — Discord
// returns the same headers on 429s and they're how we know how long to
// back off.
func (r *rateLimitState) update(resp *http.Response) {
	if resp == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if b := resp.Header.Get("X-RateLimit-Bucket"); b != "" {
		r.bucket = b
	}
	if rem := resp.Header.Get("X-RateLimit-Remaining"); rem != "" {
		if n, err := strconv.Atoi(rem); err == nil {
			r.remaining = n
			metrics.DiscordRateLimitRemaining.Set(float64(n))
		}
	}
	// Prefer X-RateLimit-Reset-After (relative seconds, fractional) over
	// X-RateLimit-Reset (absolute Unix epoch). Both are present on
	// well-formed Discord responses; the relative form sidesteps clock
	// skew between us and Discord's edge.
	if ra := resp.Header.Get("X-RateLimit-Reset-After"); ra != "" {
		if secs, err := strconv.ParseFloat(ra, 64); err == nil {
			r.resetAt = time.Now().Add(time.Duration(secs * float64(time.Second)))
			return
		}
	}
	if abs := resp.Header.Get("X-RateLimit-Reset"); abs != "" {
		if epoch, err := strconv.ParseFloat(abs, 64); err == nil {
			secs, frac := splitEpoch(epoch)
			r.resetAt = time.Unix(secs, frac)
		}
	}
}

// waitForBucket blocks until either the bucket has capacity (remaining > 0)
// or its reset window has elapsed. Returns the duration actually waited
// so callers can log defensive throttling. Honors ctx.Done().
func (r *rateLimitState) waitForBucket(ctx context.Context) (time.Duration, error) {
	r.mu.Lock()
	if r.remaining > 0 || r.resetAt.IsZero() {
		r.mu.Unlock()
		return 0, nil
	}
	wait := time.Until(r.resetAt)
	r.mu.Unlock()

	if wait <= 0 {
		return 0, nil
	}
	metrics.DiscordRateLimitWaitsTotal.Inc()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
		return wait, nil
	}
}

// splitEpoch breaks a fractional Unix timestamp (e.g., 1700000000.123)
// into seconds + nanosecond remainder for time.Unix().
func splitEpoch(epoch float64) (int64, int64) {
	secs := int64(epoch)
	frac := int64((epoch - float64(secs)) * float64(time.Second))
	return secs, frac
}

// chunkAlerts splits a flat alert slice into batches of at most n
// elements. Returns nil for an empty input (so callers can range over
// the result without a special case). n must be >= 1; callers pass
// maxEmbedsPerMessage and the constant is non-zero.
func chunkAlerts(alerts []AlertPayload, n int) [][]AlertPayload {
	if len(alerts) == 0 {
		return nil
	}
	chunks := make([][]AlertPayload, 0, (len(alerts)+n-1)/n)
	for i := 0; i < len(alerts); i += n {
		end := min(i+n, len(alerts))
		chunks = append(chunks, alerts[i:end])
	}
	return chunks
}
