// Package main implements a mock eBay API server for local development.
// It serves canned responses from JSON fixtures to simulate the eBay Browse API
// and OAuth token endpoint without requiring real eBay credentials.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type browseAPIResponse struct {
	ItemSummaries []json.RawMessage `json:"itemSummaries"`
	Total         int               `json:"total"`
	Offset        int               `json:"offset"`
	Limit         int               `json:"limit"`
	Next          string            `json:"next"`
}

type itemSummary struct {
	Title string `json:"title"`
}

func main() {
	port := flag.Int("port", 8089, "port to listen on")
	fixtureFile := flag.String("fixture", "tools/mock-server/testdata/search_response.json", "path to search response fixture")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	fixture, err := loadFixture(*fixtureFile)
	if err != nil {
		logger.Error("failed to load fixture", "path", *fixtureFile, "error", err)
		os.Exit(1)
	}
	logger.Info("loaded fixture", "items", len(fixture.ItemSummaries))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /identity/v1/oauth2/token", tokenHandler(logger))
	mux.HandleFunc("GET /buy/browse/v1/item_summary/search", searchHandler(logger, fixture))

	addr := fmt.Sprintf(":%d", *port)
	logger.Info("starting mock eBay server", "addr", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      requestLogger(logger, mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func loadFixture(path string) (*browseAPIResponse, error) {
	data, err := os.ReadFile(path) //nolint:gosec // fixture path from trusted CLI flag
	if err != nil {
		return nil, fmt.Errorf("reading fixture: %w", err)
	}
	var resp browseAPIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing fixture: %w", err)
	}
	return &resp, nil
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Debug("request", "method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery)
		next.ServeHTTP(w, r)
	})
}

func tokenHandler(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Validate Basic Auth header is present (don't verify creds).
		if _, _, ok := r.BasicAuth(); !ok {
			logger.Warn("token request missing Basic Auth header")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			//nolint:errcheck,gosec // best-effort write to HTTP response in mock server
			json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_client",
				"error_description": "client authentication failed",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		//nolint:errcheck,gosec // best-effort write to HTTP response in mock server
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-token-v1-" + strconv.FormatInt(int64(os.Getpid()), 16),
			"expires_in":   7200,
			"token_type":   "Application Access Token",
		})
		logger.Info("issued mock token")
	}
}

func searchHandler(logger *slog.Logger, fixture *browseAPIResponse) http.HandlerFunc {
	// Pre-parse titles for filtering.
	type indexedItem struct {
		raw   json.RawMessage
		title string
	}
	items := make([]indexedItem, 0, len(fixture.ItemSummaries))
	for _, raw := range fixture.ItemSummaries {
		var s itemSummary
		//nolint:errcheck,gosec // fixture data is trusted; title extraction is best-effort
		json.Unmarshal(raw, &s)
		items = append(items, indexedItem{raw: raw, title: strings.ToLower(s.Title)})
	}

	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("q"))
		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")

		limit := 50
		if limitStr != "" {
			if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
				limit = v
			}
		}
		offset := 0
		if offsetStr != "" {
			if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
				offset = v
			}
		}

		// Filter items by query substring match on title.
		var matched []json.RawMessage
		for _, item := range items {
			if q == "" || strings.Contains(item.title, q) {
				matched = append(matched, item.raw)
			}
		}

		total := len(matched)

		// Apply pagination.
		if offset >= len(matched) {
			matched = nil
		} else {
			end := min(offset+limit, len(matched))
			matched = matched[offset:end]
		}

		next := ""
		if offset+limit < total {
			next = fmt.Sprintf("/buy/browse/v1/item_summary/search?q=%s&offset=%d&limit=%d",
				r.URL.Query().Get("q"), offset+limit, limit)
		}

		resp := browseAPIResponse{
			ItemSummaries: matched,
			Total:         total,
			Offset:        offset,
			Limit:         limit,
			Next:          next,
		}

		// Return empty array instead of null when no results.
		if resp.ItemSummaries == nil {
			resp.ItemSummaries = []json.RawMessage{}
		}

		w.Header().Set("Content-Type", "application/json")
		//nolint:errcheck,gosec // best-effort write to HTTP response in mock server
		json.NewEncoder(w).Encode(resp)
		logger.Info("search", "query", q, "matched", total, "returned", len(matched), "offset", offset, "limit", limit)
	}
}
