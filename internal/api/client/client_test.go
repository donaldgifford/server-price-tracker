package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestClient_ConnectionRefused(t *testing.T) {
	t.Parallel()

	c := New("http://127.0.0.1:1") // nothing listening
	_, err := c.ListWatches(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API server not running")
}

func TestClient_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.ListWatches(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error (HTTP 500)")
}

func TestClient_ListWatches(t *testing.T) {
	t.Parallel()

	watches := []domain.Watch{
		{ID: "w1", Name: "Test Watch"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/watches", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(watches)
	}))
	defer srv.Close()

	c := New(srv.URL)
	result, err := c.ListWatches(context.Background())
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "w1", result[0].ID)
}

func TestClient_CreateWatch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var watch domain.Watch
		err := json.NewDecoder(r.Body).Decode(&watch)
		assert.NoError(t, err)
		watch.ID = "w-created"

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(watch)
	}))
	defer srv.Close()

	c := New(srv.URL)
	result, err := c.CreateWatch(context.Background(), &domain.Watch{
		Name:        "New Watch",
		SearchQuery: "DDR4 ECC",
	})
	require.NoError(t, err)
	assert.Equal(t, "w-created", result.ID)
}

func TestClient_DeleteWatch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v1/watches/w1", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.DeleteWatch(context.Background(), "w1")
	require.NoError(t, err)
}

func TestClient_ListListings(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/listings", r.URL.Path)
		assert.Equal(t, "ram", r.URL.Query().Get("component_type"))
		assert.Equal(t, "10", r.URL.Query().Get("limit"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListingsResponse{
			Listings: []domain.Listing{{ID: "l1"}},
			Total:    1,
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	resp, err := c.ListListings(context.Background(), &ListListingsParams{
		ComponentType: "ram",
		Limit:         10,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, resp.Total)
	assert.Len(t, resp.Listings, 1)
}

func TestClient_TriggerIngestion(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/ingest", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.TriggerIngestion(context.Background())
	require.NoError(t, err)
}

func TestClient_GetWatch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/watches/w1", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.Watch{ID: "w1", Name: "Test"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	result, err := c.GetWatch(context.Background(), "w1")
	require.NoError(t, err)
	assert.Equal(t, "w1", result.ID)
	assert.Equal(t, "Test", result.Name)
}

func TestClient_UpdateWatch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/v1/watches/w1", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var watch domain.Watch
		err := json.NewDecoder(r.Body).Decode(&watch)
		assert.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(watch)
	}))
	defer srv.Close()

	c := New(srv.URL)
	result, err := c.UpdateWatch(context.Background(), &domain.Watch{
		ID:   "w1",
		Name: "Updated Watch",
	})
	require.NoError(t, err)
	assert.Equal(t, "Updated Watch", result.Name)
}

func TestClient_SetWatchEnabled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/v1/watches/w1/enabled", r.URL.Path)

		var body map[string]bool
		err := json.NewDecoder(r.Body).Decode(&body)
		assert.NoError(t, err)
		assert.False(t, body["enabled"])

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.SetWatchEnabled(context.Background(), "w1", false)
	require.NoError(t, err)
}

func TestClient_GetListing(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/listings/l1", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.Listing{ID: "l1", Title: "Test Listing"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	result, err := c.GetListing(context.Background(), "l1")
	require.NoError(t, err)
	assert.Equal(t, "l1", result.ID)
	assert.Equal(t, "Test Listing", result.Title)
}

func TestClient_Rescore(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/rescore", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"scored": 42})
	}))
	defer srv.Close()

	c := New(srv.URL)
	scored, err := c.Rescore(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 42, scored)
}

func TestClient_ListBaselines(t *testing.T) {
	t.Parallel()

	baselines := []domain.PriceBaseline{
		{ID: "b1", ProductKey: "ram:ddr4"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/baselines", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(baselines)
	}))
	defer srv.Close()

	c := New(srv.URL)
	result, err := c.ListBaselines(context.Background())
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "ram:ddr4", result[0].ProductKey)
}

func TestClient_GetBaseline(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/baselines/ram:ddr4", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(domain.PriceBaseline{
			ID:         "b1",
			ProductKey: "ram:ddr4",
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	result, err := c.GetBaseline(context.Background(), "ram:ddr4")
	require.NoError(t, err)
	assert.Equal(t, "ram:ddr4", result.ProductKey)
}

func TestClient_RefreshBaselines(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/baselines/refresh", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.RefreshBaselines(context.Background())
	require.NoError(t, err)
}

func TestClient_ListListingsAllParams(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "ram", q.Get("component_type"))
		assert.Equal(t, "ram:ddr4", q.Get("product_key"))
		assert.Equal(t, "50", q.Get("min_score"))
		assert.Equal(t, "90", q.Get("max_score"))
		assert.Equal(t, "25", q.Get("limit"))
		assert.Equal(t, "10", q.Get("offset"))
		assert.Equal(t, "score", q.Get("order_by"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListingsResponse{
			Listings: nil,
			Total:    0,
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	resp, err := c.ListListings(context.Background(), &ListListingsParams{
		ComponentType: "ram",
		ProductKey:    "ram:ddr4",
		MinScore:      50,
		MaxScore:      90,
		Limit:         25,
		Offset:        10,
		OrderBy:       "score",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Total)
}

func TestWithHTTPClient(t *testing.T) {
	t.Parallel()

	custom := &http.Client{}
	c := New("http://example.com", WithHTTPClient(custom))
	assert.Same(t, custom, c.httpClient)
}
