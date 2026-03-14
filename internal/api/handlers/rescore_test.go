package handlers_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestRescoreHandler_Rescore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setupMock  func(*storeMocks.MockStore)
		wantStatus int
		wantBody   string
	}{
		{
			name: "successful rescore",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListListingsCursor(mock.Anything, "", 200).
					Return([]domain.Listing{
						{
							ID:         "l1",
							ProductKey: "ram:ddr4:32gb",
							Price:      45.99,
							Quantity:   1,
						},
					}, nil).
					Once()
				m.EXPECT().
					ListListingsCursor(mock.Anything, "l1", 200).
					Return(nil, nil).
					Once()
				m.EXPECT().
					GetBaseline(mock.Anything, "ram:ddr4:32gb").
					Return(nil, pgx.ErrNoRows).
					Once()
				m.EXPECT().
					UpdateScore(mock.Anything, "l1", mock.AnythingOfType("int"), mock.Anything).
					Return(nil).
					Once()
			},
			wantStatus: http.StatusOK,
			wantBody:   `"scored":1`,
		},
		{
			name: "no listings to rescore",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListListingsCursor(mock.Anything, "", 200).
					Return(nil, nil).
					Once()
			},
			wantStatus: http.StatusOK,
			wantBody:   `"scored":0`,
		},
		{
			name: "store error returns 500",
			setupMock: func(m *storeMocks.MockStore) {
				m.EXPECT().
					ListListingsCursor(mock.Anything, "", 200).
					Return(nil, errors.New("db down")).
					Once()
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `rescore failed`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := storeMocks.NewMockStore(t)
			tt.setupMock(mockStore)

			h := handlers.NewRescoreHandler(mockStore)

			_, api := humatest.New(t)
			handlers.RegisterRescoreRoutes(api, h)

			resp := api.Post("/api/v1/rescore")
			require.Equal(t, tt.wantStatus, resp.Code)
			assert.Contains(t, resp.Body.String(), tt.wantBody)
		})
	}
}

func TestRescoreHandler_Rescore_PartialFailure(t *testing.T) {
	t.Parallel()

	mockStore := storeMocks.NewMockStore(t)

	listings := []domain.Listing{
		{ID: "l1", ProductKey: "key-a", Price: 10, Quantity: 1},
		{ID: "l2", ProductKey: "key-b", Price: 20, Quantity: 1},
	}

	mockStore.EXPECT().
		ListListingsCursor(mock.Anything, "", 200).
		Return(listings, nil).
		Once()
	mockStore.EXPECT().
		ListListingsCursor(mock.Anything, "l2", 200).
		Return(nil, nil).
		Once()

	mockStore.EXPECT().
		GetBaseline(mock.Anything, "key-a").
		Return(nil, pgx.ErrNoRows).
		Once()
	mockStore.EXPECT().
		UpdateScore(mock.Anything, "l1", mock.AnythingOfType("int"), mock.Anything).
		Return(nil).
		Once()

	mockStore.EXPECT().
		GetBaseline(mock.Anything, "key-b").
		Return(nil, errors.New("intermittent failure")).
		Once()

	h := handlers.NewRescoreHandler(mockStore)

	_, api := humatest.New(t)
	handlers.RegisterRescoreRoutes(api, h)

	resp := api.Post("/api/v1/rescore")
	// Partial failure still returns 500 since engine returns error.
	assert.Equal(t, http.StatusInternalServerError, resp.Code)
}
