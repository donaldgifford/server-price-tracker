package engine

import (
	"testing"
	"time"

	ptestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ebayMocks "github.com/donaldgifford/server-price-tracker/internal/ebay/mocks"
	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	notifyMocks "github.com/donaldgifford/server-price-tracker/internal/notify/mocks"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	extractMocks "github.com/donaldgifford/server-price-tracker/pkg/extract/mocks"
)

func TestNewScheduler_RegistersCronEntries(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	sched, err := NewScheduler(
		eng,
		15*time.Minute,
		6*time.Hour,
		quietLogger(),
	)
	require.NoError(t, err)

	entries := sched.Entries()
	assert.Len(t, entries, 2)
}

func TestScheduler_StartStop(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	sched, err := NewScheduler(
		eng,
		1*time.Hour,
		24*time.Hour,
		quietLogger(),
	)
	require.NoError(t, err)

	sched.Start()
	ctx := sched.Stop()
	<-ctx.Done()
}

func TestScheduler_SyncNextRunTimestamps(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	sched, err := NewScheduler(
		eng,
		15*time.Minute,
		6*time.Hour,
		quietLogger(),
	)
	require.NoError(t, err)

	// Start so that cron populates Next times.
	sched.Start()
	defer sched.Stop()

	sched.SyncNextRunTimestamps()

	ingestionNext := ptestutil.ToFloat64(metrics.SchedulerNextIngestionTimestamp)
	baselineNext := ptestutil.ToFloat64(metrics.SchedulerNextBaselineTimestamp)
	assert.Greater(t, ingestionNext, float64(0), "ingestion next timestamp should be set")
	assert.Greater(t, baselineNext, float64(0), "baseline next timestamp should be set")
}

func TestScheduler_StoresEntryIDs(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	eng := newTestEngine(ms, me, mx, mn)

	sched, err := NewScheduler(
		eng,
		15*time.Minute,
		6*time.Hour,
		quietLogger(),
	)
	require.NoError(t, err)

	// Verify entry IDs are stored (non-zero).
	assert.NotZero(t, sched.ingestionEntryID)
	assert.NotZero(t, sched.baselineEntryID)
	assert.NotEqual(t, sched.ingestionEntryID, sched.baselineEntryID)
}
