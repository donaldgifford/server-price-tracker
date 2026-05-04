// Package handlers — alerts_ui.go implements the server-rendered alert
// review UI from DESIGN-0010 / IMPL-0015 Phase 4.
//
// These handlers serve HTML, not JSON — they live outside the Huma /api/v1
// surface and are wired directly onto Echo. The list and table partial
// share the same templ AlertRow component so the dismiss/retry HTMX swap
// path can never drift from the full-page render.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"

	"github.com/donaldgifford/server-price-tracker/internal/api/web/components"
	"github.com/donaldgifford/server-price-tracker/internal/metrics"
	"github.com/donaldgifford/server-price-tracker/internal/notify"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

const (
	defaultMinScore = 75
	defaultPerPage  = 25
	maxPerPage      = 100

	// htmxHeaderValue is the literal HTMX sets on the HX-Request header on
	// every AJAX-triggered request; lets handlers branch between full-page
	// renders and partial swaps.
	htmxHeaderValue = "true"
)

func isHTMX(c echo.Context) bool {
	return c.Request().Header.Get("HX-Request") == htmxHeaderValue
}

// AlertsUIDeps bundles the collaborators an AlertsUIHandler needs. Pass
// the store and notifier as interfaces so handler tests can swap them
// for mocks.
//
// Langfuse is the optional Langfuse client used to record dismissals
// as `operator_dismissed` scores (IMPL-0019 Phase 4). NoopClient is
// the safe default; a real client is wired in serve.go when
// observability.langfuse.enabled is true. Failures from Score writes
// never fail a dismiss — telemetry is best-effort.
//
// LangfuseEndpoint is the bare base URL ("https://langfuse.example.com")
// used to render trace deep-links in templ. Empty → the View Trace
// button is suppressed across the UI.
type AlertsUIDeps struct {
	Store            store.Store
	Notifier         notify.Notifier
	Langfuse         langfuse.Client
	LangfuseEndpoint string
	JudgeEnabled     bool
	AlertsURLBase    string // unused today; threaded through for the summary embed in Phase 6
	Logger           *slog.Logger
}

// AlertsUIHandler serves the /alerts route group.
type AlertsUIHandler struct {
	deps AlertsUIDeps
}

// tableOpts derives the per-render TableOptions struct from deps so the
// Langfuse/Judge feature flags propagate uniformly to every component
// that renders alert rows.
func (h *AlertsUIHandler) tableOpts() components.TableOptions {
	return components.TableOptions{
		LangfuseEndpoint: h.deps.LangfuseEndpoint,
		JudgeEnabled:     h.deps.JudgeEnabled,
	}
}

// NewAlertsUIHandler returns a handler ready to register against an Echo
// route group. Optional dependencies (Langfuse client, slog logger)
// fall back to no-op defaults when omitted so callers don't have to
// branch on "is observability enabled".
//
// Takes deps by pointer so the (now larger, ~88-byte) struct stays off
// the call stack — gocritic's hugeParam threshold flags pass-by-value
// at this size. The deref makes a copy for the handler so caller
// mutations after construction don't leak in.
func NewAlertsUIHandler(deps *AlertsUIDeps) *AlertsUIHandler {
	d := *deps
	if d.Langfuse == nil {
		d.Langfuse = langfuse.NoopClient{}
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &AlertsUIHandler{deps: d}
}

// scoreOperatorDismissed posts the operator_dismissed score on every
// trace ID returned from Store.DismissAlerts. Best-effort: failures
// are logged at debug only and never fail the dismiss.
//
// Score value is 1.0 — the alert was dismissed, full stop. The Phase 5
// judge worker will produce its own quality score against the same
// trace, and the dismissed flag becomes the operator-truth label that
// the regression set is graded against.
func (h *AlertsUIHandler) scoreOperatorDismissed(ctx context.Context, traceIDs []string) {
	if len(traceIDs) == 0 {
		return
	}
	for _, traceID := range traceIDs {
		if err := h.deps.Langfuse.Score(ctx, traceID, "operator_dismissed", 1.0, ""); err != nil {
			h.deps.Logger.Debug("langfuse Score (operator_dismissed) failed", "error", err, "trace_id", traceID)
		}
	}
}

// RegisterAlertsUIRoutes wires every handler method onto e under the
// /alerts route group. Caller must guard registration on cfg.Web.Enabled.
func RegisterAlertsUIRoutes(e *echo.Echo, h *AlertsUIHandler) {
	e.GET("/alerts", h.ListPage)
	e.GET("/alerts.json", h.ListJSON)
	e.GET("/alerts/:id", h.DetailPage)
	e.POST("/alerts/:id/dismiss", h.DismissOne)
	e.POST("/alerts/dismiss", h.DismissBulk)
	e.POST("/alerts/:id/restore", h.Restore)
	e.POST("/alerts/:id/retry", h.Retry)
}

// ListPage renders the full alerts page on a normal GET, or just the
// table partial when the request carries the HX-Request header (HTMX
// search-as-you-type, filter changes, dismiss/restore swaps).
func (h *AlertsUIHandler) ListPage(c echo.Context) error {
	q := parseAlertsListQuery(c)

	result, err := h.deps.Store.ListAlertsForReview(c.Request().Context(), &q)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "listing alerts: "+err.Error())
	}

	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	w := c.Response().Writer
	opts := h.tableOpts()
	if isHTMX(c) {
		return components.AlertsTable(result, c.Request().URL.RawQuery, opts).Render(c.Request().Context(), w)
	}
	return components.AlertsPage(components.AlertsPageData{
		Result:    result,
		Query:     q,
		RawQuery:  c.Request().URL.RawQuery,
		TableOpts: opts,
	}).Render(c.Request().Context(), w)
}

// ListJSON serves the same data as ListPage as JSON. Useful for
// scripts and for sanity-checking the search/filter URL state.
func (h *AlertsUIHandler) ListJSON(c echo.Context) error {
	q := parseAlertsListQuery(c)
	result, err := h.deps.Store.ListAlertsForReview(c.Request().Context(), &q)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "listing alerts: "+err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// DetailPage renders the full per-alert detail view.
func (h *AlertsUIHandler) DetailPage(c echo.Context) error {
	id := c.Param("id")
	d, err := h.deps.Store.GetAlertDetail(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "alert not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "fetching alert detail: "+err.Error())
	}
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	return components.AlertDetailPage(components.AlertDetailData{
		Detail:           d,
		LangfuseEndpoint: h.deps.LangfuseEndpoint,
	}).Render(c.Request().Context(), c.Response().Writer)
}

// DismissOne dismisses a single alert by path-param id. Returns the
// updated row partial for HTMX clients, or redirects to /alerts for
// a no-JS plain form submit.
func (h *AlertsUIHandler) DismissOne(c echo.Context) error {
	id := c.Param("id")
	n, traceIDs, err := h.deps.Store.DismissAlerts(c.Request().Context(), []string{id})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "dismissing alert: "+err.Error())
	}
	for range n {
		metrics.AlertsDismissedTotal.Inc()
	}
	h.scoreOperatorDismissed(c.Request().Context(), traceIDs)
	if isHTMX(c) {
		return c.NoContent(http.StatusOK)
	}
	return c.Redirect(http.StatusSeeOther, "/alerts")
}

// DismissBulk dismisses every alert in the request's `ids` form values
// (or JSON body when Content-Type is application/json).
func (h *AlertsUIHandler) DismissBulk(c echo.Context) error {
	ids, err := readIDs(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if len(ids) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "no alert ids provided")
	}

	n, traceIDs, err := h.deps.Store.DismissAlerts(c.Request().Context(), ids)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "dismissing alerts: "+err.Error())
	}
	for range n {
		metrics.AlertsDismissedTotal.Inc()
	}
	h.scoreOperatorDismissed(c.Request().Context(), traceIDs)

	if isHTMX(c) {
		// Return the refreshed table for swap-in-place.
		q := parseAlertsListQuery(c)
		result, err := h.deps.Store.ListAlertsForReview(c.Request().Context(), &q)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "refreshing alerts: "+err.Error())
		}
		c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
		return components.AlertsTable(result, c.Request().URL.RawQuery, h.tableOpts()).
			Render(c.Request().Context(), c.Response().Writer)
	}
	return c.Redirect(http.StatusSeeOther, "/alerts")
}

// Restore clears dismissed_at on a single alert.
func (h *AlertsUIHandler) Restore(c echo.Context) error {
	id := c.Param("id")
	if _, err := h.deps.Store.RestoreAlerts(c.Request().Context(), []string{id}); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "restoring alert: "+err.Error())
	}
	if isHTMX(c) {
		return c.NoContent(http.StatusOK)
	}
	return c.Redirect(http.StatusSeeOther, "/alerts/"+id)
}

// Retry re-sends the alert via Discord, bypassing the
// HasSuccessfulNotification idempotency guard. Always sends a rich
// per-alert embed regardless of summary mode (per resolved Q3).
//
// Returns the updated NotificationHistory partial so the detail page
// reflects the new attempt without a reload.
func (h *AlertsUIHandler) Retry(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	d, err := h.deps.Store.GetAlertDetail(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "alert not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "fetching alert: "+err.Error())
	}

	payload := buildRetryPayload(d)
	sendErr := h.deps.Notifier.SendAlert(ctx, payload)

	errText := ""
	if sendErr != nil {
		errText = sendErr.Error()
	}
	if attemptErr := h.deps.Store.InsertNotificationAttempt(
		ctx, id, sendErr == nil, 0, errText,
	); attemptErr != nil {
		// We sent (or tried) — log but don't fail the request just because
		// the audit row didn't land. The /metrics counter stays correct.
		c.Logger().Warn("recording retry attempt: ", attemptErr)
	}

	// Re-fetch so the rendered partial reflects the brand-new attempt.
	d, err = h.deps.Store.GetAlertDetail(ctx, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "refreshing alert detail: "+err.Error())
	}

	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	return components.NotificationHistory(d.NotificationHistory).
		Render(ctx, c.Response().Writer)
}

// buildRetryPayload synthesizes a notify.AlertPayload from the alert
// detail. Keeps the retry path consistent with what the engine sends
// during normal scheduled notifications.
func buildRetryPayload(d *domain.AlertDetail) *notify.AlertPayload {
	unitPrice := d.Listing.Price
	if d.Listing.Quantity > 1 {
		unitPrice = d.Listing.Price / float64(d.Listing.Quantity)
	}
	return &notify.AlertPayload{
		WatchName:     d.Watch.Name,
		ListingTitle:  d.Listing.Title,
		EbayURL:       d.Listing.ItemURL,
		ImageURL:      d.Listing.ImageURL,
		Price:         strconv.FormatFloat(d.Listing.Price, 'f', 2, 64),
		UnitPrice:     strconv.FormatFloat(unitPrice, 'f', 2, 64),
		Score:         d.Alert.Score,
		Seller:        d.Listing.SellerName,
		Condition:     string(d.Listing.ConditionNorm),
		ComponentType: string(d.Listing.ComponentType),
	}
}

// parseAlertsListQuery extracts AlertReviewQuery from URL query params.
// Invalid values silently fall back to defaults (rather than 400ing) so
// shared/bookmarked URLs keep working when constants change.
func parseAlertsListQuery(c echo.Context) store.AlertReviewQuery {
	q := store.AlertReviewQuery{
		Search:        c.QueryParam("q"),
		ComponentType: c.QueryParam("type"),
		WatchID:       c.QueryParam("watch"),
		Status:        parseStatus(c.QueryParam("status")),
		Sort:          parseSort(c.QueryParam("sort")),
		MinScore:      parseIntDefault(c.QueryParam("min_score"), defaultMinScore, 0, 100),
		Page:          parseIntDefault(c.QueryParam("page"), 1, 1, 0),
		PerPage:       parseIntDefault(c.QueryParam("per_page"), defaultPerPage, 1, maxPerPage),
	}
	return q
}

func parseStatus(raw string) store.AlertReviewStatus {
	switch store.AlertReviewStatus(raw) {
	case store.AlertStatusActive,
		store.AlertStatusDismissed,
		store.AlertStatusNotified,
		store.AlertStatusUndismissed,
		store.AlertStatusAll:
		return store.AlertReviewStatus(raw)
	default:
		return store.AlertStatusActive
	}
}

func parseSort(raw string) string {
	switch raw {
	case "score", "created", "watch":
		return raw
	default:
		return "score"
	}
}

// parseIntDefault parses raw as an int, falling back to fallback when
// raw is empty or invalid. min/max clamp the result; pass max=0 for "no
// upper bound".
func parseIntDefault(raw string, fallback, minVal, maxVal int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if v < minVal {
		return minVal
	}
	if maxVal > 0 && v > maxVal {
		return maxVal
	}
	return v
}

// readIDs accepts either form-encoded `ids` values (HTML form POST) or a
// JSON body of `{"ids":[...]}` so the same endpoint serves both the
// HTMX bulk-dismiss form and any JSON consumers.
func readIDs(c echo.Context) ([]string, error) {
	ct := c.Request().Header.Get(echo.HeaderContentType)
	if strings.HasPrefix(ct, echo.MIMEApplicationJSON) {
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(c.Request().Body).Decode(&body); err != nil {
			return nil, errInvalidJSON
		}
		return body.IDs, nil
	}
	if err := c.Request().ParseForm(); err != nil {
		return nil, err
	}
	return c.Request().Form["ids"], nil
}

var errInvalidJSON = errors.New("invalid JSON body: expected {\"ids\": [...]}")

// Compile-time check that the handler stays Echo-shaped.
var _ context.Context = context.Background()
