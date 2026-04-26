package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	notifymocks "github.com/donaldgifford/server-price-tracker/internal/notify/mocks"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	storemocks "github.com/donaldgifford/server-price-tracker/internal/store/mocks"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// newAlertsUITestServer builds a handler wired against a mock store/notifier
// and registers the routes onto a fresh Echo instance. Returns both so
// individual tests can program expectations on the mocks.
func newAlertsUITestServer(t *testing.T) (*echo.Echo, *storemocks.MockStore, *notifymocks.MockNotifier) {
	t.Helper()
	s := storemocks.NewMockStore(t)
	n := notifymocks.NewMockNotifier(t)
	h := handlers.NewAlertsUIHandler(handlers.AlertsUIDeps{Store: s, Notifier: n})
	e := echo.New()
	handlers.RegisterAlertsUIRoutes(e, h)
	return e, s, n
}

// TestParseAlertsListQuery exercises every branch of the URL → AlertReviewQuery
// mapping. Done indirectly via /alerts.json so we cover the same code path
// the HTML page uses (parseAlertsListQuery is unexported).
func TestParseAlertsListQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rawQuery string
		want     store.AlertReviewQuery
	}{
		{
			name:     "empty query falls back to defaults",
			rawQuery: "",
			want: store.AlertReviewQuery{
				Status:   store.AlertStatusActive,
				Sort:     "score",
				MinScore: 75,
				Page:     1,
				PerPage:  25,
			},
		},
		{
			name:     "explicit valid filters round-trip",
			rawQuery: "q=poweredge&type=server&watch=w1&status=dismissed&sort=created&min_score=80&page=3&per_page=50",
			want: store.AlertReviewQuery{
				Search:        "poweredge",
				ComponentType: "server",
				WatchID:       "w1",
				Status:        store.AlertStatusDismissed,
				Sort:          "created",
				MinScore:      80,
				Page:          3,
				PerPage:       50,
			},
		},
		{
			name:     "invalid status defaults to active",
			rawQuery: "status=bogus",
			want: store.AlertReviewQuery{
				Status:   store.AlertStatusActive,
				Sort:     "score",
				MinScore: 75,
				Page:     1,
				PerPage:  25,
			},
		},
		{
			name:     "invalid sort defaults to score",
			rawQuery: "sort=garbage",
			want: store.AlertReviewQuery{
				Status:   store.AlertStatusActive,
				Sort:     "score",
				MinScore: 75,
				Page:     1,
				PerPage:  25,
			},
		},
		{
			name:     "min_score clamps to 0..100",
			rawQuery: "min_score=999",
			want: store.AlertReviewQuery{
				Status:   store.AlertStatusActive,
				Sort:     "score",
				MinScore: 100,
				Page:     1,
				PerPage:  25,
			},
		},
		{
			name:     "negative min_score clamps to 0",
			rawQuery: "min_score=-5",
			want: store.AlertReviewQuery{
				Status:   store.AlertStatusActive,
				Sort:     "score",
				MinScore: 0,
				Page:     1,
				PerPage:  25,
			},
		},
		{
			name:     "per_page clamps to maxPerPage",
			rawQuery: "per_page=500",
			want: store.AlertReviewQuery{
				Status:   store.AlertStatusActive,
				Sort:     "score",
				MinScore: 75,
				Page:     1,
				PerPage:  100,
			},
		},
		{
			name:     "non-numeric page falls back to 1",
			rawQuery: "page=abc",
			want: store.AlertReviewQuery{
				Status:   store.AlertStatusActive,
				Sort:     "score",
				MinScore: 75,
				Page:     1,
				PerPage:  25,
			},
		},
		{
			name:     "undismissed status preserved",
			rawQuery: "status=undismissed",
			want: store.AlertReviewQuery{
				Status:   store.AlertStatusUndismissed,
				Sort:     "score",
				MinScore: 75,
				Page:     1,
				PerPage:  25,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e, s, _ := newAlertsUITestServer(t)
			s.EXPECT().
				ListAlertsForReview(mock.Anything, mock.MatchedBy(func(q *store.AlertReviewQuery) bool {
					return q != nil && *q == tt.want
				})).
				Return(store.AlertReviewResult{Items: []domain.AlertWithListing{}}, nil).
				Once()

			req := httptest.NewRequest(http.MethodGet, "/alerts.json?"+tt.rawQuery, http.NoBody)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		})
	}
}

// TestListPage_HTMXReturnsTablePartial verifies the HTMX header switches
// between the full page (containing <html>) and the table partial
// (containing <tbody> only).
func TestListPage_HTMXReturnsTablePartial(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		htmxHeader     string
		wantSubstr     string
		wantNotSubstr  string
		expectFullPage bool
	}{
		{
			name:           "no header renders full page",
			htmxHeader:     "",
			wantSubstr:     "<html",
			expectFullPage: true,
		},
		{
			name:          "HX-Request true renders partial",
			htmxHeader:    "true",
			wantSubstr:    "alerts-table",
			wantNotSubstr: "<html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e, s, _ := newAlertsUITestServer(t)
			s.EXPECT().
				ListAlertsForReview(mock.Anything, mock.Anything).
				Return(store.AlertReviewResult{
					Items:   []domain.AlertWithListing{},
					Total:   0,
					Page:    1,
					PerPage: 25,
				}, nil).
				Once()

			req := httptest.NewRequest(http.MethodGet, "/alerts", http.NoBody)
			if tt.htmxHeader != "" {
				req.Header.Set("HX-Request", tt.htmxHeader)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
			body := rec.Body.String()
			assert.Contains(t, body, tt.wantSubstr)
			if tt.wantNotSubstr != "" {
				assert.NotContains(t, body, tt.wantNotSubstr)
			}
		})
	}
}

// TestDismissOne covers the HTMX vs no-JS form-post branches: the former
// returns 200 No Content, the latter redirects back to /alerts.
func TestDismissOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		htmxHeader string
		wantStatus int
	}{
		{
			name:       "no-JS form post redirects",
			wantStatus: http.StatusSeeOther,
		},
		{
			name:       "HTMX returns 200",
			htmxHeader: "true",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e, s, _ := newAlertsUITestServer(t)
			s.EXPECT().
				DismissAlerts(mock.Anything, []string{"alert-1"}).
				Return(1, nil).
				Once()

			req := httptest.NewRequest(http.MethodPost, "/alerts/alert-1/dismiss", http.NoBody)
			if tt.htmxHeader != "" {
				req.Header.Set("HX-Request", tt.htmxHeader)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code, "body=%s", rec.Body.String())
		})
	}
}

// TestDismissBulk_BadRequest covers the empty-ids branch.
func TestDismissBulk_BadRequest(t *testing.T) {
	t.Parallel()

	e, _, _ := newAlertsUITestServer(t)

	body := strings.NewReader(url.Values{}.Encode())
	req := httptest.NewRequest(http.MethodPost, "/alerts/dismiss", body)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())
}

// TestRetry verifies a successful resend records a notification attempt
// with succeeded=true and renders the refreshed history partial.
func TestRetry(t *testing.T) {
	t.Parallel()

	e, s, n := newAlertsUITestServer(t)

	detail := &domain.AlertDetail{
		Alert:   domain.Alert{ID: "alert-1", Score: 88},
		Listing: domain.Listing{ID: "listing-1", Title: "Dell PowerEdge R720", Price: 250.00, Quantity: 1},
		Watch:   domain.Watch{ID: "watch-1", Name: "Servers"},
	}
	s.EXPECT().GetAlertDetail(mock.Anything, "alert-1").Return(detail, nil).Twice()
	n.EXPECT().SendAlert(mock.Anything, mock.Anything).Return(nil).Once()
	s.EXPECT().
		InsertNotificationAttempt(mock.Anything, "alert-1", true, 0, "").
		Return(nil).
		Once()

	req := httptest.NewRequest(http.MethodPost, "/alerts/alert-1/retry", http.NoBody)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
}
