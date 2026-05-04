// Package main is the operator-facing CLI for running the extraction
// regression suite against one or more configured LLM backends.
//
// Workflow (IMPL-0019 Phase 6):
//
//  1. Operator labels ~100 listings into testdata/golden_classifications.json
//     (manual today; tools/dataset-bootstrap is the planned helper).
//  2. Operator runs `make test-regression` to gate prompt-affecting
//     PRs, OR `go run ./tools/regression-runner --config <path>
//     [--backends ollama,anthropic]` for single-backend or
//     side-by-side comparison runs.
//  3. Operator pastes the per-component accuracy lines (or
//     comparison table) into the PR description per
//     .github/PULL_REQUEST_TEMPLATE.md.
//
// The runner intentionally has no CI presence — fork-PR security
// concerns + API-key exfiltration risks rule out a CI workflow. The
// PR template checkbox is the gate.
//
// Cost ($/1k extractions) in the comparison view is currently emitted
// as "—" because per-extraction token usage is not surfaced through
// the LLMExtractor return path; surfacing it is parked as a follow-up
// alongside the Langfuse classify_prompt:<sha> dataset-run annotation.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/config"
	"github.com/donaldgifford/server-price-tracker/pkg/extract"
	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// goldenItem mirrors the JSON shape pkg/extract/regression_test.go
// expects in testdata/golden_classifications.json. Kept in sync by
// hand; if the test file's struct changes, this one must follow.
type goldenItem struct {
	Title              string               `json:"title"`
	ItemSpecifics      map[string]string    `json:"item_specifics"`
	ExpectedComponent  domain.ComponentType `json:"expected_component"`
	ExpectedProductKey string               `json:"expected_product_key,omitempty"`
}

// componentResult tallies accuracy for a single ComponentType.
type componentResult struct {
	Component domain.ComponentType `json:"component"`
	Total     int                  `json:"total"`
	Correct   int                  `json:"correct"`
}

// Accuracy returns the per-component accuracy as a percentage in
// [0.0, 100.0]. Returns 0 when Total is zero so an empty bucket prints
// as 0% rather than NaN.
func (r componentResult) Accuracy() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Correct) / float64(r.Total) * 100
}

// runResult is the JSON shape emitted under --json. Suitable for piping
// into `jq` or summarising in a Claude Code session.
type runResult struct {
	Backend    string            `json:"backend"`
	Model      string            `json:"model"`
	Total      int               `json:"total"`
	Correct    int               `json:"correct"`
	Errors     int               `json:"errors"`
	Accuracy   float64           `json:"accuracy_percent"`
	ErrorRate  float64           `json:"error_rate_percent"`
	Duration   time.Duration     `json:"duration"`
	P50Latency time.Duration     `json:"p50_latency"`
	P95Latency time.Duration     `json:"p95_latency"`
	PerComp    []componentResult `json:"per_component"`
	Mismatches []mismatch        `json:"mismatches,omitempty"`
}

// comparisonResult wraps multiple runResults for --backends side-by-side.
type comparisonResult struct {
	Backends []runResult `json:"backends"`
}

type mismatch struct {
	Title    string               `json:"title"`
	Expected domain.ComponentType `json:"expected"`
	Actual   domain.ComponentType `json:"actual"`
	Error    string               `json:"error,omitempty"`
}

func main() {
	configPath := flag.String("config", "configs/config.dev.yaml", "path to YAML config file")
	datasetPath := flag.String(
		"dataset", "testdata/golden_classifications.json",
		"path to golden dataset",
	)
	jsonOut := flag.Bool("json", false, "emit JSON instead of a human-readable table")
	backendsFlag := flag.String(
		"backends", "",
		"comma-separated list of backends to compare (e.g., ollama,anthropic); empty = single-backend mode using cfg.LLM.Backend",
	)
	langfuseDatasetID := flag.String(
		"langfuse-dataset-id", "",
		"Langfuse dataset ID for the uploaded golden_classifications "+
			"dataset; when set with langfuse enabled, posts a "+
			"CreateDatasetRun annotation tagged with the current commit SHA",
	)
	sha := flag.String(
		"sha", "",
		"override the run-name SHA (default: `git rev-parse HEAD` from the working tree)",
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal("loading config %s: %v", *configPath, err)
	}

	dataset, err := loadDataset(*datasetPath)
	if err != nil {
		fatal("loading dataset %s: %v", *datasetPath, err)
	}
	if len(dataset) == 0 {
		fmt.Fprintln(os.Stderr, "dataset is empty; bootstrap with tools/dataset-bootstrap")
		os.Exit(0)
	}

	backends := parseBackends(*backendsFlag, cfg.LLM.Backend)
	results := executeAll(cfg, logger, dataset, backends)
	if len(results) == 0 {
		fatal("no backends in --backends list could be constructed from config")
	}

	annotateLangfuse(cfg, logger, results, dataset, *langfuseDatasetID, *sha)

	emit(results, *jsonOut)
}

// executeAll runs the dataset against each requested backend and
// returns the non-nil results. The single-backend case is just the
// degenerate len(backends)==1 path through this same loop.
func executeAll(
	cfg *config.Config,
	logger *slog.Logger,
	dataset []goldenItem,
	backends []string,
) []runResult {
	out := make([]runResult, 0, len(backends))
	for _, b := range backends {
		r := executeBackend(cfg, logger, dataset, b)
		if r == nil {
			logger.Warn("skipping backend (not configured)", "backend", b)
			continue
		}
		out = append(out, *r)
	}
	return out
}

func emit(results []runResult, jsonOut bool) {
	if len(results) == 1 {
		if jsonOut {
			emitJSON(&results[0])
			return
		}
		emitTable(&results[0])
		return
	}

	c := comparisonResult{Backends: results}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(c); err != nil {
			fatal("encoding JSON: %v", err)
		}
		return
	}
	emitComparison(&c)
}

// parseBackends turns the --backends flag into a deduplicated slice,
// falling back to the config's single backend when the flag is empty.
func parseBackends(flagVal, fallback string) []string {
	if flagVal == "" {
		return []string{fallback}
	}
	parts := strings.Split(flagVal, ",")
	seen := make(map[string]bool, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// executeBackend builds the named backend from cfg, runs the dataset,
// and returns the result. Returns nil when the backend cannot be
// constructed (e.g., endpoint or API key missing).
func executeBackend(
	cfg *config.Config,
	logger *slog.Logger,
	dataset []goldenItem,
	backendName string,
) *runResult {
	backend := buildBackendByName(cfg, logger, backendName)
	if backend == nil {
		return nil
	}
	extractor := extract.NewLLMExtractor(backend, extract.WithLogger(logger))
	r := runDataset(context.Background(), extractor, dataset, backendName, modelOfBackend(cfg, backendName))
	return &r
}

// annotateLangfuse posts a CreateDatasetRun annotation tagged with the
// current commit SHA so operators can compare runs by SHA in the
// Langfuse UI. Skipped silently when:
//   - Langfuse is disabled in config
//   - --langfuse-dataset-id is empty (operator hasn't uploaded the
//     dataset yet, so DatasetItemIDs aren't known)
//   - HTTP client construction fails (logged at debug)
//
// One DatasetRun per backend so a multi-backend comparison run shows
// up as N rows in the Langfuse UI keyed by `<sha>:<backend>`.
func annotateLangfuse(
	cfg *config.Config,
	logger *slog.Logger,
	results []runResult,
	dataset []goldenItem,
	datasetID, shaOverride string,
) {
	if !cfg.Observability.Langfuse.Enabled {
		return
	}
	if datasetID == "" {
		logger.Debug("langfuse annotation skipped: --langfuse-dataset-id not set")
		return
	}

	sha := shaOverride
	if sha == "" {
		sha = currentCommitSHA(logger)
	}
	if sha == "" {
		logger.Warn("langfuse annotation skipped: could not determine commit SHA")
		return
	}

	client, err := langfuse.NewHTTPClient(
		cfg.Observability.Langfuse.Endpoint,
		cfg.Observability.Langfuse.PublicKey,
		cfg.Observability.Langfuse.SecretKey,
	)
	if err != nil {
		logger.Warn("langfuse annotation skipped: client construction failed", "error", err)
		return
	}

	ctx := context.Background()
	for i := range results {
		r := &results[i]
		run := &langfuse.DatasetRun{
			DatasetID:   datasetID,
			RunName:     fmt.Sprintf("classify_prompt:%s:%s", sha, r.Backend),
			Description: fmt.Sprintf("regression-runner accuracy=%.1f%% errors=%.1f%%", r.Accuracy, r.ErrorRate),
			Metadata: map[string]string{
				"backend":  r.Backend,
				"model":    r.Model,
				"sha":      sha,
				"accuracy": fmt.Sprintf("%.4f", r.Accuracy),
			},
			ItemResults: buildDatasetRunItems(dataset, r),
		}
		if err := client.CreateDatasetRun(ctx, run); err != nil {
			logger.Warn("langfuse CreateDatasetRun failed", "error", err, "backend", r.Backend)
			continue
		}
		logger.Info("langfuse annotation posted",
			"run_name", run.RunName, "items", len(run.ItemResults))
	}
}

// buildDatasetRunItems pairs each dataset row with the corresponding
// runResult mismatch (if any), using a deterministic hash of the title
// as DatasetItemID so the operator's upload step can produce matching
// IDs without coordinating with this binary.
func buildDatasetRunItems(dataset []goldenItem, r *runResult) []langfuse.DatasetRunItem {
	mismatchByTitle := make(map[string]mismatch, len(r.Mismatches))
	for _, m := range r.Mismatches {
		mismatchByTitle[m.Title] = m
	}

	items := make([]langfuse.DatasetRunItem, 0, len(dataset))
	for i := range dataset {
		item := &dataset[i]
		entry := langfuse.DatasetRunItem{DatasetItemID: titleHash(item.Title)}
		if m, ok := mismatchByTitle[item.Title]; ok {
			entry.Output = map[string]any{
				"expected": string(m.Expected),
				"actual":   string(m.Actual),
				"error":    m.Error,
			}
		} else {
			entry.Output = map[string]any{
				"expected": string(item.ExpectedComponent),
				"actual":   string(item.ExpectedComponent),
			}
		}
		items = append(items, entry)
	}
	return items
}

// titleHash returns a short, deterministic ID for a dataset title.
// Operators upload dataset items with the same hash as DatasetItemID so
// runs and items align without out-of-band coordination.
func titleHash(title string) string {
	sum := sha256.Sum256([]byte(title))
	return hex.EncodeToString(sum[:8])
}

// currentCommitSHA shells out to `git rev-parse HEAD`. Returns "" when
// git isn't available or the working tree isn't a repo — caller logs
// and skips the annotation. Trims trailing whitespace.
func currentCommitSHA(logger *slog.Logger) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		logger.Debug("git rev-parse HEAD failed", "error", err)
		return ""
	}
	return strings.TrimSpace(string(out))
}

func loadDataset(path string) ([]goldenItem, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	// G304: path is operator-supplied via --dataset and intended to
	// point at testdata/golden_classifications.json on the operator's
	// local checkout. The runner is operator-only (no CI surface, no
	// unauthenticated callers) so the inclusion-via-variable warning
	// does not apply.
	raw, err := os.ReadFile(abs) //nolint:gosec // operator-supplied dataset path; no untrusted input surface
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []goldenItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	return items, nil
}

// buildBackendByName mirrors cmd/server-price-tracker/cmd/serve.go::buildLLMBackend
// but takes an explicit backend name so a single config file can drive
// side-by-side comparison runs without mutating cfg.LLM.Backend.
func buildBackendByName(cfg *config.Config, logger *slog.Logger, name string) extract.LLMBackend {
	switch name {
	case "ollama":
		if cfg.LLM.Ollama.Endpoint == "" {
			logger.Warn("ollama endpoint not configured")
			return nil
		}
		timeout := cfg.LLM.Timeout
		if timeout == 0 {
			timeout = 120 * time.Second
		}
		return extract.NewOllamaBackend(
			cfg.LLM.Ollama.Endpoint,
			cfg.LLM.Ollama.Model,
			extract.WithOllamaHTTPClient(&http.Client{Timeout: timeout}),
		)
	case "anthropic":
		return extract.NewAnthropicBackend(
			extract.WithAnthropicModel(cfg.LLM.Anthropic.Model),
		)
	case "openai_compat":
		if cfg.LLM.OpenAICompat.Endpoint == "" {
			logger.Warn("openai_compat endpoint not configured")
			return nil
		}
		return extract.NewOpenAICompatBackend(
			cfg.LLM.OpenAICompat.Endpoint,
			cfg.LLM.OpenAICompat.Model,
		)
	default:
		logger.Error("unknown LLM backend", "backend", name)
		return nil
	}
}

func modelOfBackend(cfg *config.Config, name string) string {
	switch name {
	case "ollama":
		return cfg.LLM.Ollama.Model
	case "anthropic":
		return cfg.LLM.Anthropic.Model
	case "openai_compat":
		return cfg.LLM.OpenAICompat.Model
	}
	return ""
}

func runDataset(
	ctx context.Context,
	extractor *extract.LLMExtractor,
	dataset []goldenItem,
	backendName, modelName string,
) runResult {
	start := time.Now()
	perComp := map[domain.ComponentType]*componentResult{}
	var mismatches []mismatch
	latencies := make([]time.Duration, 0, len(dataset))
	correct := 0
	errCount := 0

	for i := range dataset {
		item := &dataset[i]
		bucket, ok := perComp[item.ExpectedComponent]
		if !ok {
			bucket = &componentResult{Component: item.ExpectedComponent}
			perComp[item.ExpectedComponent] = bucket
		}
		bucket.Total++

		callStart := time.Now()
		actual, _, err := extractor.ClassifyAndExtract(ctx, item.Title, item.ItemSpecifics)
		latencies = append(latencies, time.Since(callStart))
		if err != nil {
			errCount++
			mismatches = append(mismatches, mismatch{
				Title:    item.Title,
				Expected: item.ExpectedComponent,
				Actual:   actual,
				Error:    err.Error(),
			})
			continue
		}
		if actual == item.ExpectedComponent {
			bucket.Correct++
			correct++
			continue
		}
		mismatches = append(mismatches, mismatch{
			Title:    item.Title,
			Expected: item.ExpectedComponent,
			Actual:   actual,
		})
	}

	results := make([]componentResult, 0, len(perComp))
	for _, r := range perComp {
		results = append(results, *r)
	}
	sort.Slice(results, func(i, j int) bool {
		return string(results[i].Component) < string(results[j].Component)
	})

	overall := 0.0
	errorRate := 0.0
	if total := len(dataset); total > 0 {
		overall = float64(correct) / float64(total) * 100
		errorRate = float64(errCount) / float64(total) * 100
	}

	p50, p95 := percentiles(latencies)

	return runResult{
		Backend:    backendName,
		Model:      modelName,
		Total:      len(dataset),
		Correct:    correct,
		Errors:     errCount,
		Accuracy:   overall,
		ErrorRate:  errorRate,
		Duration:   time.Since(start),
		P50Latency: p50,
		P95Latency: p95,
		PerComp:    results,
		Mismatches: mismatches,
	}
}

// percentiles returns p50 and p95 of the latency sample. Uses
// nearest-rank — fine for sample sizes <= 1k where exact percentile
// computation isn't worth the complexity.
func percentiles(latencies []time.Duration) (p50, p95 time.Duration) {
	if len(latencies) == 0 {
		return 0, 0
	}
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	slices.Sort(sorted)
	idx50 := (len(sorted) - 1) * 50 / 100
	idx95 := (len(sorted) - 1) * 95 / 100
	return sorted[idx50], sorted[idx95]
}

func emitTable(r *runResult) {
	fmt.Printf("Regression run — backend=%s model=%s duration=%s\n",
		r.Backend, r.Model, r.Duration.Round(time.Millisecond))
	fmt.Printf("Overall accuracy: %d/%d (%.1f%%)  errors: %d (%.1f%%)  p50: %s  p95: %s\n\n",
		r.Correct, r.Total, r.Accuracy,
		r.Errors, r.ErrorRate,
		r.P50Latency.Round(time.Millisecond),
		r.P95Latency.Round(time.Millisecond),
	)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "Component\tCorrect\tTotal\tAccuracy"); err != nil {
		fatal("writing header: %v", err)
	}
	if _, err := fmt.Fprintln(w, "---------\t-------\t-----\t--------"); err != nil {
		fatal("writing separator: %v", err)
	}
	for _, c := range r.PerComp {
		if _, err := fmt.Fprintf(w, "%s\t%d\t%d\t%.1f%%\n",
			c.Component, c.Correct, c.Total, c.Accuracy()); err != nil {
			fatal("writing row: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		fatal("flushing table: %v", err)
	}

	if len(r.Mismatches) == 0 {
		return
	}
	fmt.Printf("\nMismatches (%d):\n", len(r.Mismatches))
	for _, m := range r.Mismatches {
		extra := ""
		if m.Error != "" {
			extra = " — error: " + m.Error
		}
		fmt.Printf("  %q expected=%s actual=%s%s\n",
			truncate(m.Title, 80), m.Expected, m.Actual, extra)
	}
}

// emitComparison renders the multi-backend table. $/1k extractions
// shows "—" today because per-extraction token usage is not surfaced
// through the LLMExtractor return path — see the package-level
// follow-up note.
func emitComparison(c *comparisonResult) {
	fmt.Printf("Backend comparison — %d backend(s)\n\n", len(c.Backends))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "Backend\tModel\tAccuracy\tErrors\tp50\tp95\t$/1k"); err != nil {
		fatal("writing header: %v", err)
	}
	if _, err := fmt.Fprintln(w, "-------\t-----\t--------\t------\t---\t---\t----"); err != nil {
		fatal("writing separator: %v", err)
	}
	for i := range c.Backends {
		r := &c.Backends[i]
		if _, err := fmt.Fprintf(w, "%s\t%s\t%.1f%%\t%.1f%%\t%s\t%s\t%s\n",
			r.Backend, r.Model, r.Accuracy, r.ErrorRate,
			r.P50Latency.Round(time.Millisecond),
			r.P95Latency.Round(time.Millisecond),
			"—",
		); err != nil {
			fatal("writing row: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		fatal("flushing table: %v", err)
	}
}

func emitJSON(r *runResult) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		fatal("encoding JSON: %v", err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
