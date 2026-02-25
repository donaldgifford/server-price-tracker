package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	ebayMocks "github.com/donaldgifford/server-price-tracker/internal/ebay/mocks"
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

func TestScheduler_RecoverStaleJobs_Error(t *testing.T) {
	t.Parallel()

	eng, _ := newSchedulerTestEngine(t)
	ms := storeMocks.NewMockStore(t)

	sched, err := NewScheduler(eng, ms, 1*time.Hour, 24*time.Hour, 0, quietLogger())
	require.NoError(t, err)

	ms.EXPECT().
		RecoverStaleJobRuns(mock.Anything, 2*time.Hour).
		Return(0, errors.New("db error")).Once()

	// Should log and return without panic.
	sched.RecoverStaleJobRuns(context.Background())
}

func TestScheduler_RunIngestion_LockNotAcquired(t *testing.T) {
	t.Parallel()

	eng, _ := newSchedulerTestEngine(t)
	ms := storeMocks.NewMockStore(t)

	sched, err := NewScheduler(eng, ms, 1*time.Hour, 24*time.Hour, 0, quietLogger())
	require.NoError(t, err)

	ms.EXPECT().
		AcquireSchedulerLock(mock.Anything, "ingestion", mock.Anything, mock.Anything).
		Return(false, nil).Once()

	// Should return silently; no job row, no engine calls.
	sched.runIngestion()
}

func TestScheduler_RunIngestion_Success(t *testing.T) {
	eng, engMs := newSchedulerTestEngine(t)
	schedMs := storeMocks.NewMockStore(t)

	sched, err := NewScheduler(eng, schedMs, 1*time.Hour, 24*time.Hour, 0, quietLogger())
	require.NoError(t, err)

	schedMs.EXPECT().
		AcquireSchedulerLock(mock.Anything, "ingestion", mock.Anything, 30*time.Minute).
		Return(true, nil).Once()
	schedMs.EXPECT().
		InsertJobRun(mock.Anything, "ingestion").
		Return("run-1", nil).Once()
	schedMs.EXPECT().
		ReleaseSchedulerLock(mock.Anything, "ingestion", mock.Anything).
		Return(nil).Once()
	schedMs.EXPECT().
		CompleteJobRun(mock.Anything, "run-1", "succeeded", "", 0).
		Return(nil).Once()

	// Engine store: RunIngestion with no watches.
	engMs.EXPECT().ListWatches(mock.Anything, true).Return(nil, nil).Once()
	engMs.EXPECT().ListPendingAlerts(mock.Anything).Return(nil, nil).Once()

	sched.runIngestion()
}

func TestScheduler_RunBaselineRefresh_LockNotAcquired(t *testing.T) {
	t.Parallel()

	eng, _ := newSchedulerTestEngine(t)
	ms := storeMocks.NewMockStore(t)

	sched, err := NewScheduler(eng, ms, 1*time.Hour, 24*time.Hour, 0, quietLogger())
	require.NoError(t, err)

	ms.EXPECT().
		AcquireSchedulerLock(mock.Anything, "baseline_refresh", mock.Anything, mock.Anything).
		Return(false, nil).Once()

	sched.runBaselineRefresh()
}

func TestScheduler_RunBaselineRefresh_Success(t *testing.T) {
	eng, engMs := newSchedulerTestEngine(t)
	schedMs := storeMocks.NewMockStore(t)

	sched, err := NewScheduler(eng, schedMs, 1*time.Hour, 24*time.Hour, 0, quietLogger())
	require.NoError(t, err)

	schedMs.EXPECT().
		AcquireSchedulerLock(mock.Anything, "baseline_refresh", mock.Anything, 60*time.Minute).
		Return(true, nil).Once()
	schedMs.EXPECT().
		InsertJobRun(mock.Anything, "baseline_refresh").
		Return("run-2", nil).Once()
	schedMs.EXPECT().
		ReleaseSchedulerLock(mock.Anything, "baseline_refresh", mock.Anything).
		Return(nil).Once()
	schedMs.EXPECT().
		CompleteJobRun(mock.Anything, "run-2", "succeeded", "", 0).
		Return(nil).Once()

	// Engine store: RunBaselineRefresh with no listings.
	engMs.EXPECT().RecomputeAllBaselines(mock.Anything, 90).Return(nil).Once()
	engMs.EXPECT().
		ListListingsCursor(mock.Anything, "", 200).
		Return(nil, nil).Once()

	sched.runBaselineRefresh()
}

func TestScheduler_RunReExtraction_LockNotAcquired(t *testing.T) {
	t.Parallel()

	eng, _ := newSchedulerTestEngine(t)
	ms := storeMocks.NewMockStore(t)

	sched, err := NewScheduler(eng, ms, 1*time.Hour, 24*time.Hour, 1*time.Hour, quietLogger())
	require.NoError(t, err)

	ms.EXPECT().
		AcquireSchedulerLock(mock.Anything, "re_extraction", mock.Anything, mock.Anything).
		Return(false, nil).Once()

	sched.runReExtraction()
}

func TestScheduler_RunReExtraction_Success(t *testing.T) {
	eng, engMs := newSchedulerTestEngine(t)
	schedMs := storeMocks.NewMockStore(t)

	sched, err := NewScheduler(eng, schedMs, 1*time.Hour, 24*time.Hour, 1*time.Hour, quietLogger())
	require.NoError(t, err)

	schedMs.EXPECT().
		AcquireSchedulerLock(mock.Anything, "re_extraction", mock.Anything, 30*time.Minute).
		Return(true, nil).Once()
	schedMs.EXPECT().
		InsertJobRun(mock.Anything, "re_extraction").
		Return("run-3", nil).Once()
	schedMs.EXPECT().
		ReleaseSchedulerLock(mock.Anything, "re_extraction", mock.Anything).
		Return(nil).Once()
	schedMs.EXPECT().
		CompleteJobRun(mock.Anything, "run-3", "succeeded", "", 0).
		Return(nil).Once()

	// Engine store: RunReExtraction with no incomplete listings.
	engMs.EXPECT().
		ListIncompleteExtractions(mock.Anything, "", 100).
		Return(nil, nil).Once()

	sched.runReExtraction()
}
