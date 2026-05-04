package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/donaldgifford/server-price-tracker/pkg/judge"
)

// JudgeRunner is the subset of judge.Worker the HTTP handler needs.
// Defining it as a local interface lets the handler test pass a mock
// without dragging the whole worker constructor into the test setup.
type JudgeRunner interface {
	Run(ctx context.Context) (int, error)
}

// JudgeHandler exposes a manual-trigger endpoint for the LLM-as-judge
// worker. Production usage is the cron entry; the HTTP endpoint exists
// for operator backfill (e.g., `spt judge run` after a model upgrade
// where you want to re-grade recent alerts on demand).
type JudgeHandler struct {
	worker JudgeRunner
}

// NewJudgeHandler builds a handler over a worker. Worker may be nil
// when the judge feature is disabled — the handler responds 503 in
// that case so callers can detect "not configured" without 404
// confusion.
func NewJudgeHandler(w JudgeRunner) *JudgeHandler {
	return &JudgeHandler{worker: w}
}

// JudgeRunInput is the request body for the manual trigger. All
// fields are advisory — Worker.Run already honours its config defaults
// — so they're documented but not enforced server-side.
type JudgeRunInput struct {
	Body struct {
		Since time.Duration `json:"since,omitempty" doc:"Lookback override (ignored — worker uses its config)"`
		Limit int           `json:"limit,omitempty" doc:"Batch-size override (ignored — worker uses its config)"`
	}
}

// JudgeRunOutput is the response body: how many alerts were judged
// this run, plus a flag indicating whether the daily budget was the
// reason the worker stopped early.
type JudgeRunOutput struct {
	Body struct {
		Judged          int  `json:"judged"            doc:"Number of alerts evaluated this run"`
		BudgetExhausted bool `json:"budget_exhausted"  doc:"True when the daily USD cap halted the run early"`
	}
}

// RunJudge executes one judge tick synchronously. Returns 503 when the
// judge worker isn't configured (judge.enabled = false at boot).
//
// `_ = in` — the request body is currently advisory; we may wire
// per-call overrides in a future iteration but the v1 surface uses the
// worker's compile-time config so spend is bounded by deployment, not
// by HTTP callers.
func (h *JudgeHandler) RunJudge(ctx context.Context, _ *JudgeRunInput) (*JudgeRunOutput, error) {
	if h.worker == nil {
		return nil, huma.Error503ServiceUnavailable("judge worker not configured")
	}
	judged, err := h.worker.Run(ctx)
	resp := &JudgeRunOutput{}
	resp.Body.Judged = judged
	if err != nil {
		if errors.Is(err, judge.ErrJudgeBudgetExhausted) {
			resp.Body.BudgetExhausted = true
			return resp, nil
		}
		return nil, huma.Error500InternalServerError("judge run failed: " + err.Error())
	}
	return resp, nil
}

// RegisterJudgeRoutes registers the manual-trigger endpoint. Mounts
// regardless of whether the worker is configured — the handler
// responds 503 in the disabled case so the OpenAPI surface stays
// stable across deployments.
func RegisterJudgeRoutes(api huma.API, h *JudgeHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "run-judge",
		Method:      http.MethodPost,
		Path:        "/api/v1/judge/run",
		Summary:     "Run the LLM-as-judge worker on demand",
		Description: "Triggers one tick of the LLM-as-judge worker, grading any un-judged alerts in the configured lookback window. Honours the configured daily USD budget cap. Returns 503 when the judge feature is disabled.",
		Tags:        []string{"judge"},
	}, h.RunJudge)
}
