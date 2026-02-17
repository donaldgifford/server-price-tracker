package handlers_test

import (
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	storeMocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestListBaselines_Success(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	ms.On("ListBaselines", mock.Anything).Return([]domain.PriceBaseline{
		{ID: "b1", ProductKey: "ram:ddr4:ecc_reg:32gb:2666", SampleCount: 47},
	}, nil)

	h := handlers.NewBaselinesHandler(ms)
	_, api := humatest.New(t)
	handlers.RegisterBaselineRoutes(api, h)

	resp := api.Get("/api/v1/baselines")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "ram:ddr4:ecc_reg:32gb:2666")
}

func TestListBaselines_Empty(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	ms.On("ListBaselines", mock.Anything).Return(nil, nil)

	h := handlers.NewBaselinesHandler(ms)
	_, api := humatest.New(t)
	handlers.RegisterBaselineRoutes(api, h)

	resp := api.Get("/api/v1/baselines")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "[]")
}

func TestGetBaseline_Success(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	ms.On("GetBaseline", mock.Anything, "ram:ddr4:ecc_reg:32gb:2666").Return(
		&domain.PriceBaseline{
			ID:          "b1",
			ProductKey:  "ram:ddr4:ecc_reg:32gb:2666",
			SampleCount: 47,
			P50:         28.99,
		}, nil,
	)

	h := handlers.NewBaselinesHandler(ms)
	_, api := humatest.New(t)
	handlers.RegisterBaselineRoutes(api, h)

	resp := api.Get("/api/v1/baselines/ram:ddr4:ecc_reg:32gb:2666")
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), "ram:ddr4:ecc_reg:32gb:2666")
	assert.Contains(t, resp.Body.String(), "28.99")
}

func TestGetBaseline_NotFound(t *testing.T) {
	t.Parallel()

	ms := storeMocks.NewMockStore(t)
	ms.On("GetBaseline", mock.Anything, "nonexistent").Return(
		nil, assert.AnError,
	)

	h := handlers.NewBaselinesHandler(ms)
	_, api := humatest.New(t)
	handlers.RegisterBaselineRoutes(api, h)

	resp := api.Get("/api/v1/baselines/nonexistent")
	require.Equal(t, http.StatusNotFound, resp.Code)
}
