package handlers_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestListingsHandler_List(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		setupMock  func(*storeMocks.MockStore)
		wantStatus int
		wantBody   string
	}{
		{
			name: "no filters returns listings",
			path: "/api/v1/listings",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListListings(mock.Anything, mock.Anything).
					Return([]domain.Listing{
						{ID: "l1", Title: "Test RAM"},
					}, 1, nil).
					Once()
			},
			wantStatus: http.StatusOK,
			wantBody:   `"total":1`,
		},
		{
			name: "component type filter",
			path: "/api/v1/listings?component_type=ram",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListListings(mock.Anything, mock.MatchedBy(func(q *store.ListingQuery) bool {
						return q.ComponentType != nil && *q.ComponentType == "ram"
					})).
					Return(nil, 0, nil).
					Once()
			},
			wantStatus: http.StatusOK,
			wantBody:   `"total":0`,
		},
		{
			name: "score range filter",
			path: "/api/v1/listings?min_score=70&max_score=95",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListListings(mock.Anything, mock.MatchedBy(func(q *store.ListingQuery) bool {
						return q.MinScore != nil && *q.MinScore == 70 &&
							q.MaxScore != nil && *q.MaxScore == 95
					})).
					Return(nil, 0, nil).
					Once()
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "pagination params",
			path: "/api/v1/listings?limit=10&offset=20",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListListings(mock.Anything, mock.MatchedBy(func(q *store.ListingQuery) bool {
						return q.Limit == 10 && q.Offset == 20
					})).
					Return(nil, 0, nil).
					Once()
			},
			wantStatus: http.StatusOK,
			wantBody:   `"limit":10`,
		},
		{
			name: "order by param",
			path: "/api/v1/listings?order_by=score",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListListings(mock.Anything, mock.MatchedBy(func(q *store.ListingQuery) bool {
						return q.OrderBy == "score"
					})).
					Return(nil, 0, nil).
					Once()
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid min_score returns 422",
			path:       "/api/v1/listings?min_score=abc",
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid max_score returns 422",
			path:       "/api/v1/listings?max_score=xyz",
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid limit returns 422",
			path:       "/api/v1/listings?limit=not_a_number",
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid offset returns 422",
			path:       "/api/v1/listings?offset=bad",
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "invalid enum returns 422",
			path:       "/api/v1/listings?component_type=invalid_type",
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "store error returns 500",
			path: "/api/v1/listings",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListListings(mock.Anything, mock.Anything).
					Return(nil, 0, errors.New("db error")).
					Once()
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `listing query failed`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ms := storeMocks.NewMockStore(t)
			tt.setupMock(ms)
			h := handlers.NewListingsHandler(ms)

			_, api := humatest.New(t)
			handlers.RegisterListingRoutes(api, h)

			resp := api.Get(tt.path)
			require.Equal(t, tt.wantStatus, resp.Code)
			if tt.wantBody != "" {
				assert.Contains(t, resp.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestListingsHandler_GetByID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		id         string
		setupMock  func(*storeMocks.MockStore)
		wantStatus int
		wantBody   string
	}{
		{
			name: "found returns 200",
			id:   "abc-123",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					GetListingByID(mock.Anything, "abc-123").
					Return(&domain.Listing{
						ID:    "abc-123",
						Title: "Samsung 32GB DDR4",
					}, nil).
					Once()
			},
			wantStatus: http.StatusOK,
			wantBody:   `Samsung 32GB DDR4`,
		},
		{
			name: "not found returns 404",
			id:   "nonexistent",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					GetListingByID(mock.Anything, "nonexistent").
					Return(nil, errors.New("not found")).
					Once()
			},
			wantStatus: http.StatusNotFound,
			wantBody:   `listing not found`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ms := storeMocks.NewMockStore(t)
			tt.setupMock(ms)
			h := handlers.NewListingsHandler(ms)

			_, api := humatest.New(t)
			handlers.RegisterListingRoutes(api, h)

			resp := api.Get("/api/v1/listings/" + tt.id)
			require.Equal(t, tt.wantStatus, resp.Code)
			assert.Contains(t, resp.Body.String(), tt.wantBody)
		})
	}
}
