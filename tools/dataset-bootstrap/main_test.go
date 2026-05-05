package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/store"
	storemocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestCandidateFromListing_PrefillsExpectedFields(t *testing.T) {
	t.Parallel()

	l := &domain.Listing{
		Title:         "Dell PowerEdge R740xd 2.5\" SFF",
		ComponentType: domain.ComponentServer,
		ProductKey:    "server:dell:r740xd:sff:configured",
	}

	got := candidateFromListing(l)
	assert.Equal(t, l.Title, got.Title)
	assert.Equal(t, domain.ComponentServer, got.ExpectedComponent)
	assert.Equal(t, l.ProductKey, got.ExpectedProductKey)
	// item_specifics is intentionally an empty map (operator fills it
	// in by hand if needed) rather than nil — tests the contract that
	// downstream JSON emits {} not null for empty specifics.
	assert.NotNil(t, got.ItemSpecifics)
	assert.Empty(t, got.ItemSpecifics)
}

func TestStratifiedSample_SkipsErroredBucketsAndSortsResults(t *testing.T) {
	t.Parallel()

	st := storemocks.NewMockStore(t)

	st.EXPECT().
		ListListings(mock.Anything, queryFor("ram")).
		Return([]domain.Listing{
			{Title: "32GB DDR4 ECC", ComponentType: domain.ComponentRAM, ProductKey: "ram:ddr4:32gb"},
			{Title: "64GB DDR4 ECC", ComponentType: domain.ComponentRAM, ProductKey: "ram:ddr4:64gb"},
		}, 2, nil).Once()

	// Drive bucket errors — should be skipped, not abort the run.
	st.EXPECT().
		ListListings(mock.Anything, queryFor("drive")).
		Return(nil, 0, errors.New("transient db error")).Once()

	st.EXPECT().
		ListListings(mock.Anything, queryFor("server")).
		Return([]domain.Listing{
			{Title: "Dell R740xd", ComponentType: domain.ComponentServer, ProductKey: "server:dell:r740xd"},
		}, 1, nil).Once()

	// Every remaining bucket returns empty so the test exercises the
	// stratifiedSample loop body for every ComponentType the function
	// iterates over — drift in that list shows up here as an
	// unexpected-call failure.
	for _, ct := range []domain.ComponentType{
		domain.ComponentCPU, domain.ComponentNIC, domain.ComponentGPU,
		domain.ComponentWorkstation, domain.ComponentDesktop, domain.ComponentOther,
	} {
		st.EXPECT().
			ListListings(mock.Anything, queryFor(string(ct))).
			Return(nil, 0, nil).Once()
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	got := stratifiedSample(context.Background(), st, 5, logger)
	require.Len(t, got, 3, "RAM 2 + Server 1; drive errored; others empty")

	// Output is sorted by ExpectedComponent so the operator can audit
	// category-by-category. Just assert non-decreasing component string.
	for i := 1; i < len(got); i++ {
		assert.LessOrEqual(t,
			string(got[i-1].ExpectedComponent),
			string(got[i].ExpectedComponent))
	}
}

func TestStratifiedSample_PassesPerComponentAsLimit(t *testing.T) {
	t.Parallel()

	st := storemocks.NewMockStore(t)

	// Every ComponentType bucket should see Limit=7 in the query —
	// drift would show up as an unexpected-call match failure.
	st.EXPECT().
		ListListings(mock.Anything, mock.MatchedBy(func(q any) bool {
			query, ok := q.(*store.ListingQuery)
			return ok && query.Limit == 7
		})).
		Return(nil, 0, nil).
		Times(9) // nine ComponentTypes in stratifiedSample.

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	got := stratifiedSample(context.Background(), st, 7, logger)
	assert.Empty(t, got)
}

// queryFor builds a mock matcher that asserts the *store.ListingQuery
// argument has the given ComponentType. Keeps test bodies readable.
func queryFor(want string) any {
	return mock.MatchedBy(func(q any) bool {
		query, ok := q.(*store.ListingQuery)
		if !ok || query.ComponentType == nil {
			return false
		}
		return *query.ComponentType == want
	})
}
