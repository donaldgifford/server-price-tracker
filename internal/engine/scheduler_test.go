package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ebayMocks "github.com/donaldgifford/server-price-tracker/internal/ebay/mocks"
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
