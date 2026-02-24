package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	ptestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	ebayMocks "github.com/donaldgifford/server-price-tracker/internal/ebay/mocks"
	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	notifyMocks "github.com/donaldgifford/server-price-tracker/internal/notify/mocks"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	extractMocks "github.com/donaldgifford/server-price-tracker/pkg/extract/mocks"
)

// newSchedulerTestEngine returns a test engine and a mock store for use in scheduler tests.
func newSchedulerTestEngine(t *testing.T) (*Engine, *storeMocks.MockStore) {
	t.Helper()
	ms := storeMocks.NewMockStore(t)
	me := ebayMocks.NewMockEbayClient(t)
	mx := extractMocks.NewMockExtractor(t)
	mn := notifyMocks.NewMockNotifier(t)
	return newTestEngine(ms, me, mx, mn), ms
}

func TestNewScheduler_RegistersCronEntries(t *testing.T) {
	t.Parallel()

	eng, ms := newSchedulerTestEngine(t)

	sched, err := NewScheduler(
		eng,
		ms,
		15*time.Minute,
		6*time.Hour,
		0,
		quietLogger(),
	)
	require.NoError(t, err)

	entries := sched.Entries()
	assert.Len(t, entries, 2)
}

func TestScheduler_StartStop(t *testing.T) {
	t.Parallel()

	eng, ms := newSchedulerTestEngine(t)

	sched, err := NewScheduler(
		eng,
		ms,
		1*time.Hour,
		24*time.Hour,
		0,
		quietLogger(),
	)
	require.NoError(t, err)

	sched.Start()
	ctx := sched.Stop()
	<-ctx.Done()
}

func TestScheduler_SyncNextRunTimestamps(t *testing.T) {
	t.Parallel()

	eng, ms := newSchedulerTestEngine(t)

	sched, err := NewScheduler(
		eng,
		ms,
		15*time.Minute,
		6*time.Hour,
		0,
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

	eng, ms := newSchedulerTestEngine(t)

	sched, err := NewScheduler(
		eng,
		ms,
		15*time.Minute,
		6*time.Hour,
		0,
		quietLogger(),
	)
	require.NoError(t, err)

	// Verify entry IDs are stored (non-zero).
	assert.NotZero(t, sched.ingestionEntryID)
	assert.NotZero(t, sched.baselineEntryID)
	assert.NotEqual(t, sched.ingestionEntryID, sched.baselineEntryID)
}

func TestNewScheduler_WithReExtraction(t *testing.T) {
	t.Parallel()

	eng, ms := newSchedulerTestEngine(t)

	sched, err := NewScheduler(
		eng,
		ms,
		15*time.Minute,
		6*time.Hour,
		1*time.Hour,
		quietLogger(),
	)
	require.NoError(t, err)

	entries := sched.Entries()
	assert.Len(t, entries, 3)
	assert.NotZero(t, sched.reExtractionEntryID)
}

func TestNewScheduler_WithoutReExtraction(t *testing.T) {
	t.Parallel()

	eng, ms := newSchedulerTestEngine(t)

	sched, err := NewScheduler(
		eng,
		ms,
		15*time.Minute,
		6*time.Hour,
		0,
		quietLogger(),
	)
	require.NoError(t, err)

	entries := sched.Entries()
	assert.Len(t, entries, 2)
	assert.Zero(t, sched.reExtractionEntryID)
}

func TestScheduler_RunJob_Success(t *testing.T) {
	t.Parallel()

	eng, _ := newSchedulerTestEngine(t)
	ms := storeMocks.NewMockStore(t)

	sched, err := NewScheduler(eng, ms, 1*time.Hour, 24*time.Hour, 0, quietLogger())
	require.NoError(t, err)

	ms.EXPECT().
		AcquireSchedulerLock(mock.Anything, "test-job", mock.Anything, mock.Anything).
		Return(true, nil).Once()
	ms.EXPECT().InsertJobRun(mock.Anything, "test-job").Return("run-id-1", nil).Once()
	ms.EXPECT().
		CompleteJobRun(mock.Anything, "run-id-1", "succeeded", "", 0).
		Return(nil).Once()
	ms.EXPECT().
		ReleaseSchedulerLock(mock.Anything, "test-job", mock.Anything).
		Return(nil).Once()

	called := false
	err = sched.runJob(context.Background(), "test-job", 5*time.Minute, func(_ context.Context) error {
		called = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, called)
}

func TestScheduler_RunJob_Failure(t *testing.T) {
	t.Parallel()

	eng, _ := newSchedulerTestEngine(t)
	ms := storeMocks.NewMockStore(t)

	sched, err := NewScheduler(eng, ms, 1*time.Hour, 24*time.Hour, 0, quietLogger())
	require.NoError(t, err)

	jobErr := errors.New("something went wrong")

	ms.EXPECT().
		AcquireSchedulerLock(mock.Anything, "fail-job", mock.Anything, mock.Anything).
		Return(true, nil).Once()
	ms.EXPECT().InsertJobRun(mock.Anything, "fail-job").Return("run-id-2", nil).Once()
	ms.EXPECT().
		CompleteJobRun(mock.Anything, "run-id-2", "failed", jobErr.Error(), 0).
		Return(nil).Once()
	ms.EXPECT().
		ReleaseSchedulerLock(mock.Anything, "fail-job", mock.Anything).
		Return(nil).Once()

	err = sched.runJob(context.Background(), "fail-job", 5*time.Minute, func(_ context.Context) error {
		return jobErr
	})

	require.ErrorIs(t, err, jobErr)
}

func TestScheduler_RecoverStaleJobs(t *testing.T) {
	t.Parallel()

	eng, _ := newSchedulerTestEngine(t)
	ms := storeMocks.NewMockStore(t)

	sched, err := NewScheduler(eng, ms, 1*time.Hour, 24*time.Hour, 0, quietLogger())
	require.NoError(t, err)

	ms.EXPECT().
		RecoverStaleJobRuns(mock.Anything, 2*time.Hour).
		Return(3, nil).Once()

	sched.RecoverStaleJobRuns(context.Background())
}
