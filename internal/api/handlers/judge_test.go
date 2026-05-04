package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	"github.com/donaldgifford/server-price-tracker/pkg/judge"
)

// stubRunner is a one-shot JudgeRunner. Captures whether Run was
// called so the disabled-worker test can assert it wasn't invoked.
type stubRunner struct {
	judged int
	err    error
	calls  int
}

func (s *stubRunner) Run(_ context.Context) (int, error) {
	s.calls++
	return s.judged, s.err
}

// TestJudgeHandler_RunsWorkerAndReturnsCount: happy path.
func TestJudgeHandler_RunsWorkerAndReturnsCount(t *testing.T) {
	t.Parallel()

	r := &stubRunner{judged: 7}

	_, api := humatest.New(t)
	handlers.RegisterJudgeRoutes(api, handlers.NewJudgeHandler(r))

	resp := api.Post("/api/v1/judge/run", strings.NewReader("{}"))
	require.Equal(t, http.StatusOK, resp.Code, "body=%s", resp.Body.String())
	assert.Contains(t, resp.Body.String(), `"judged":7`)
	assert.Contains(t, resp.Body.String(), `"budget_exhausted":false`)
	assert.Equal(t, 1, r.calls)
}

// TestJudgeHandler_BudgetExhaustedReturns200: budget hit is a normal
// outcome, surfaced via budget_exhausted=true rather than 5xx.
func TestJudgeHandler_BudgetExhaustedReturns200(t *testing.T) {
	t.Parallel()

	r := &stubRunner{judged: 4, err: judge.ErrJudgeBudgetExhausted}

	_, api := humatest.New(t)
	handlers.RegisterJudgeRoutes(api, handlers.NewJudgeHandler(r))

	resp := api.Post("/api/v1/judge/run", strings.NewReader("{}"))
	require.Equal(t, http.StatusOK, resp.Code, "body=%s", resp.Body.String())
	assert.Contains(t, resp.Body.String(), `"judged":4`)
	assert.Contains(t, resp.Body.String(), `"budget_exhausted":true`)
}

// TestJudgeHandler_WorkerErrorReturns500 verifies that an unexpected
// worker error surfaces as 500 rather than being swallowed.
func TestJudgeHandler_WorkerErrorReturns500(t *testing.T) {
	t.Parallel()

	r := &stubRunner{err: errors.New("postgres down")}

	_, api := humatest.New(t)
	handlers.RegisterJudgeRoutes(api, handlers.NewJudgeHandler(r))

	resp := api.Post("/api/v1/judge/run", strings.NewReader("{}"))
	assert.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.Contains(t, resp.Body.String(), "postgres down")
}

// TestJudgeHandler_DisabledReturns503 verifies that a nil worker
// produces 503 so callers can distinguish "not configured" from a
// 200 response with judged=0 (a legitimately empty batch).
func TestJudgeHandler_DisabledReturns503(t *testing.T) {
	t.Parallel()

	_, api := humatest.New(t)
	handlers.RegisterJudgeRoutes(api, handlers.NewJudgeHandler(nil))

	resp := api.Post("/api/v1/judge/run", strings.NewReader("{}"))
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
}
