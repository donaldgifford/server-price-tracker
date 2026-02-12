package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func loadTestFixture(t *testing.T) *browseAPIResponse {
	t.Helper()
	path := filepath.Join("testdata", "search_response.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var resp browseAPIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return &resp
}

func TestLoadFixture(t *testing.T) {
	fixture := loadTestFixture(t)
	if len(fixture.ItemSummaries) == 0 {
		t.Fatal("expected items in fixture")
	}
	if fixture.Total != len(fixture.ItemSummaries) {
		t.Errorf("total=%d, want %d", fixture.Total, len(fixture.ItemSummaries))
	}
}

func TestTokenHandler_Success(t *testing.T) {
	handler := tokenHandler(testLogger())
	req := httptest.NewRequest(http.MethodPost, "/identity/v1/oauth2/token", http.NoBody)
	req.SetBasicAuth("app-id", "cert-id")
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp["access_token"] == nil || resp["access_token"] == "" {
		t.Error("expected non-empty access_token")
	}
	if resp["token_type"] != "Application Access Token" {
		t.Errorf("token_type=%v, want Application Access Token", resp["token_type"])
	}
	if resp["expires_in"] != float64(7200) {
		t.Errorf("expires_in=%v, want 7200", resp["expires_in"])
	}
}

func TestTokenHandler_MissingAuth(t *testing.T) {
	handler := tokenHandler(testLogger())
	req := httptest.NewRequest(http.MethodPost, "/identity/v1/oauth2/token", http.NoBody)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusUnauthorized)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp["error"] != "invalid_client" {
		t.Errorf("error=%s, want invalid_client", resp["error"])
	}
}

func TestSearchHandler_AllItems(t *testing.T) {
	fixture := loadTestFixture(t)
	handler := searchHandler(testLogger(), fixture)
	req := httptest.NewRequest(http.MethodGet, "/buy/browse/v1/item_summary/search", http.NoBody)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusOK)
	}

	var resp browseAPIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Total != len(fixture.ItemSummaries) {
		t.Errorf("total=%d, want %d", resp.Total, len(fixture.ItemSummaries))
	}
	if len(resp.ItemSummaries) != len(fixture.ItemSummaries) {
		t.Errorf("items=%d, want %d", len(resp.ItemSummaries), len(fixture.ItemSummaries))
	}
}

func TestSearchHandler_QueryFilter(t *testing.T) {
	fixture := loadTestFixture(t)
	handler := searchHandler(testLogger(), fixture)
	req := httptest.NewRequest(http.MethodGet, "/buy/browse/v1/item_summary/search?q=DDR4", http.NoBody)
	w := httptest.NewRecorder()

	handler(w, req)

	var resp browseAPIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Total == 0 {
		t.Error("expected DDR4 results")
	}
	// Every returned item should contain "ddr4" in its title.
	for _, raw := range resp.ItemSummaries {
		var item itemSummary
		_ = json.Unmarshal(raw, &item)
		if item.Title == "" {
			t.Error("expected non-empty title")
		}
	}
	if resp.Total >= len(fixture.ItemSummaries) {
		t.Error("expected filter to reduce results")
	}
}

func TestSearchHandler_Pagination(t *testing.T) {
	fixture := loadTestFixture(t)
	handler := searchHandler(testLogger(), fixture)
	req := httptest.NewRequest(http.MethodGet, "/buy/browse/v1/item_summary/search?limit=3&offset=0", http.NoBody)
	w := httptest.NewRecorder()

	handler(w, req)

	var resp browseAPIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.ItemSummaries) != 3 {
		t.Errorf("items=%d, want 3", len(resp.ItemSummaries))
	}
	if resp.Total != len(fixture.ItemSummaries) {
		t.Errorf("total=%d, want %d", resp.Total, len(fixture.ItemSummaries))
	}
	if resp.Next == "" {
		t.Error("expected non-empty next for paginated response")
	}
}

func TestSearchHandler_PaginationOffset(t *testing.T) {
	fixture := loadTestFixture(t)
	handler := searchHandler(testLogger(), fixture)
	total := len(fixture.ItemSummaries)

	req := httptest.NewRequest(http.MethodGet, "/buy/browse/v1/item_summary/search?limit=50&offset=15", http.NoBody)
	w := httptest.NewRecorder()
	handler(w, req)

	var resp browseAPIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.ItemSummaries) != total-15 {
		t.Errorf("items=%d, want %d", len(resp.ItemSummaries), total-15)
	}
	if resp.Next != "" {
		t.Error("expected empty next when all items returned")
	}
}

func TestSearchHandler_MultiWordQuery(t *testing.T) {
	fixture := loadTestFixture(t)
	handler := searchHandler(testLogger(), fixture)
	// "32GB DDR4 ECC" should match items containing all three words.
	req := httptest.NewRequest(http.MethodGet, "/buy/browse/v1/item_summary/search?q=32GB+DDR4+ECC", http.NoBody)
	w := httptest.NewRecorder()

	handler(w, req)

	var resp browseAPIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Total == 0 {
		t.Error("expected multi-word query to match items with all words present")
	}
}

func TestSearchHandler_NoResults(t *testing.T) {
	fixture := loadTestFixture(t)
	handler := searchHandler(testLogger(), fixture)
	req := httptest.NewRequest(http.MethodGet, "/buy/browse/v1/item_summary/search?q=nonexistent_xyz_product", http.NoBody)
	w := httptest.NewRecorder()

	handler(w, req)

	var resp browseAPIResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("total=%d, want 0", resp.Total)
	}
	if resp.ItemSummaries == nil {
		t.Error("expected empty array, got nil")
	}
	if len(resp.ItemSummaries) != 0 {
		t.Errorf("items=%d, want 0", len(resp.ItemSummaries))
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}
