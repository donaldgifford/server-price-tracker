package handlers_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestWatchHandler_List(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		setupMock  func(*storeMocks.MockStore)
		wantStatus int
		wantBody   string
	}{
		{
			name: "returns watches",
			path: "/api/v1/watches",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListWatches(mock.Anything, false).
					Return([]domain.Watch{
						{ID: "w1", Name: "DDR4 Watch"},
					}, nil).
					Once()
			},
			wantStatus: http.StatusOK,
			wantBody:   `"DDR4 Watch"`,
		},
		{
			name: "enabled only filter",
			path: "/api/v1/watches?enabled=true",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListWatches(mock.Anything, true).
					Return(nil, nil).
					Once()
			},
			wantStatus: http.StatusOK,
			wantBody:   `[]`,
		},
		{
			name: "store error",
			path: "/api/v1/watches",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListWatches(mock.Anything, false).
					Return(nil, errors.New("db error")).
					Once()
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `listing watches`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ms := storeMocks.NewMockStore(t)
			tt.setupMock(ms)
			h := handlers.NewWatchHandler(ms)

			_, api := humatest.New(t)
			handlers.RegisterWatchRoutes(api, h)

			resp := api.Get(tt.path)
			require.Equal(t, tt.wantStatus, resp.Code)
			assert.Contains(t, resp.Body.String(), tt.wantBody)
		})
	}
}

func TestWatchHandler_Get(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		id         string
		setupMock  func(*storeMocks.MockStore)
		wantStatus int
		wantBody   string
	}{
		{
			name: "found",
			id:   "w1",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					GetWatch(mock.Anything, "w1").
					Return(&domain.Watch{ID: "w1", Name: "Test"}, nil).
					Once()
			},
			wantStatus: http.StatusOK,
			wantBody:   `"w1"`,
		},
		{
			name: "not found",
			id:   "w-missing",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					GetWatch(mock.Anything, "w-missing").
					Return(nil, errors.New("not found")).
					Once()
			},
			wantStatus: http.StatusNotFound,
			wantBody:   `watch not found`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ms := storeMocks.NewMockStore(t)
			tt.setupMock(ms)
			h := handlers.NewWatchHandler(ms)

			_, api := humatest.New(t)
			handlers.RegisterWatchRoutes(api, h)

			resp := api.Get("/api/v1/watches/" + tt.id)
			require.Equal(t, tt.wantStatus, resp.Code)
			assert.Contains(t, resp.Body.String(), tt.wantBody)
		})
	}
}

func TestWatchHandler_Create(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       any
		setupMock  func(*storeMocks.MockStore)
		wantStatus int
		wantBody   string
	}{
		{
			name: "valid watch",
			body: map[string]any{
				"name":         "DDR4 Watch",
				"search_query": "DDR4 ECC",
			},
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					CreateWatch(mock.Anything, mock.MatchedBy(func(w *domain.Watch) bool {
						return w.Name == "DDR4 Watch" && w.SearchQuery == "DDR4 ECC"
					})).
					Return(nil).
					Once()
			},
			wantStatus: http.StatusCreated,
			wantBody:   `"DDR4 Watch"`,
		},
		{
			name: "missing name returns 422",
			body: map[string]any{
				"search_query": "DDR4 ECC",
			},
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   `expected required property name to be present`,
		},
		{
			name: "missing query returns 422",
			body: map[string]any{
				"name": "Test",
			},
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   `expected required property search_query to be present`,
		},
		{
			name: "store error",
			body: map[string]any{
				"name":         "Test",
				"search_query": "test",
			},
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					CreateWatch(mock.Anything, mock.Anything).
					Return(errors.New("db error")).
					Once()
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   "creating watch",
		},
		{
			name:       "invalid JSON",
			body:       strings.NewReader(`{invalid}`),
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ms := storeMocks.NewMockStore(t)
			tt.setupMock(ms)
			h := handlers.NewWatchHandler(ms)

			_, api := humatest.New(t)
			handlers.RegisterWatchRoutes(api, h)

			resp := api.Post("/api/v1/watches", tt.body)
			require.Equal(t, tt.wantStatus, resp.Code)
			if tt.wantBody != "" {
				assert.Contains(t, resp.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestWatchHandler_Update(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	ms.EXPECT().
		UpdateWatch(mock.Anything, mock.MatchedBy(func(w *domain.Watch) bool {
			return w.ID == "w1" && w.Name == "Updated"
		})).
		Return(nil).
		Once()

	h := handlers.NewWatchHandler(ms)

	_, api := humatest.New(t)
	handlers.RegisterWatchRoutes(api, h)

	resp := api.Put("/api/v1/watches/w1", map[string]any{
		"name":         "Updated",
		"search_query": "test",
	})
	require.Equal(t, http.StatusOK, resp.Code)
}

func TestWatchHandler_SetEnabled(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	ms.EXPECT().
		SetWatchEnabled(mock.Anything, "w1", false).
		Return(nil).
		Once()

	h := handlers.NewWatchHandler(ms)

	_, api := humatest.New(t)
	handlers.RegisterWatchRoutes(api, h)

	resp := api.Put("/api/v1/watches/w1/enabled", map[string]any{
		"enabled": false,
	})
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "updated")
}

func TestWatchHandler_Delete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setupMock  func(*storeMocks.MockStore)
		wantStatus int
	}{
		{
			name: "success",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					DeleteWatch(mock.Anything, "w1").
					Return(nil).
					Once()
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "store error",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					DeleteWatch(mock.Anything, "w1").
					Return(errors.New("db error")).
					Once()
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ms := storeMocks.NewMockStore(t)
			tt.setupMock(ms)
			h := handlers.NewWatchHandler(ms)

			_, api := humatest.New(t)
			handlers.RegisterWatchRoutes(api, h)

			resp := api.Delete("/api/v1/watches/w1")
			require.Equal(t, tt.wantStatus, resp.Code)
		})
	}
}
