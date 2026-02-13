package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
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
		query      string
		setupMock  func(*storeMocks.MockStore)
		wantStatus int
		wantBody   string
	}{
		{
			name:  "returns watches",
			query: "",
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
			name:  "enabled only filter",
			query: "?enabled=true",
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
			name:  "store error",
			query: "",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListWatches(mock.Anything, false).
					Return(nil, errors.New("db error")).
					Once()
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `"error"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ms := storeMocks.NewMockStore(t)
			tt.setupMock(ms)
			h := handlers.NewWatchHandler(ms)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/watches"+tt.query, http.NoBody)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := h.List(c)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.wantBody)
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
			wantBody:   `"watch not found"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ms := storeMocks.NewMockStore(t)
			tt.setupMock(ms)
			h := handlers.NewWatchHandler(ms)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(tt.id)

			err := h.Get(c)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.wantBody)
		})
	}
}

func TestWatchHandler_Create(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		setupMock  func(*storeMocks.MockStore)
		wantStatus int
		wantBody   string
	}{
		{
			name: "valid watch",
			body: `{"name":"DDR4 Watch","search_query":"DDR4 ECC"}`,
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
			name:       "missing name",
			body:       `{"search_query":"DDR4 ECC"}`,
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   `"name and search_query are required"`,
		},
		{
			name:       "missing query",
			body:       `{"name":"Test"}`,
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   `"name and search_query are required"`,
		},
		{
			name: "store error",
			body: `{"name":"Test","search_query":"test"}`,
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
			body:       `{invalid}`,
			setupMock:  func(_ *storeMocks.MockStore) {},
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid request body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ms := storeMocks.NewMockStore(t)
			tt.setupMock(ms)
			h := handlers.NewWatchHandler(ms)

			e := echo.New()
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/watches",
				strings.NewReader(tt.body),
			)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := h.Create(c)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.wantBody)
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

	e := echo.New()
	body := `{"name":"Updated","search_query":"test"}`
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("w1")

	err := h.Update(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWatchHandler_SetEnabled(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	ms.EXPECT().
		SetWatchEnabled(mock.Anything, "w1", false).
		Return(nil).
		Once()

	h := handlers.NewWatchHandler(ms)

	e := echo.New()
	body := `{"enabled":false}`
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("w1")

	err := h.SetEnabled(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "updated")
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

			e := echo.New()
			req := httptest.NewRequest(http.MethodDelete, "/", http.NoBody)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("w1")

			err := h.Delete(c)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}
