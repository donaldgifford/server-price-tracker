package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
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
		query      string
		setupMock  func(*storeMocks.MockStore)
		wantStatus int
		wantBody   string
	}{
		{
			name:  "no filters returns listings",
			query: "",
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
			name:  "component type filter",
			query: "?component_type=ram",
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
			name:  "score range filter",
			query: "?min_score=70&max_score=95",
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
			name:  "pagination params",
			query: "?limit=10&offset=20",
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
			name:  "order by param",
			query: "?order_by=score",
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
			name:       "invalid min_score returns 400",
			query:      "?min_score=abc",
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   `"error":"invalid min_score"`,
		},
		{
			name:       "invalid max_score returns 400",
			query:      "?max_score=xyz",
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   `"error":"invalid max_score"`,
		},
		{
			name:       "invalid limit returns 400",
			query:      "?limit=not_a_number",
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   `"error":"invalid limit"`,
		},
		{
			name:       "invalid offset returns 400",
			query:      "?offset=bad",
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   `"error":"invalid offset"`,
		},
		{
			name:  "store error returns 500",
			query: "",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListListings(mock.Anything, mock.Anything).
					Return(nil, 0, assert.AnError).
					Once()
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `"error"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := storeMocks.NewMockStore(t)
			tt.setupMock(mockStore)

			h := handlers.NewListingsHandler(mockStore)

			e := echo.New()
			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/listings"+tt.query,
				http.NoBody,
			)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := h.List(c)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.Contains(t, rec.Body.String(), tt.wantBody)
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
			wantBody:   `"title":"Samsung 32GB DDR4"`,
		},
		{
			name: "not found returns 404",
			id:   "nonexistent",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					GetListingByID(mock.Anything, "nonexistent").
					Return(nil, pgx.ErrNoRows).
					Once()
			},
			wantStatus: http.StatusNotFound,
			wantBody:   `"error":"listing not found"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := storeMocks.NewMockStore(t)
			tt.setupMock(mockStore)

			h := handlers.NewListingsHandler(mockStore)

			e := echo.New()
			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/listings/"+tt.id,
				http.NoBody,
			)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(tt.id)

			err := h.GetByID(c)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.wantBody)
		})
	}
}
