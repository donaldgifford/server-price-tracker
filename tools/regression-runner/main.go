// Package main is the operator-facing CLI for running the extraction
// regression suite against a configured LLM backend.
//
// Workflow (IMPL-0019 Phase 6):
//
//  1. Operator labels ~100 listings into testdata/golden_classifications.json
//     (manual today; tools/dataset-bootstrap is the planned helper).
//  2. Operator runs `make test-regression` — which invokes
//     `go test -tags regression ./pkg/extract/...` — for the table
//     output, OR `go run ./tools/regression-runner --config <path>` for
//     the table + JSON modes with backend selection.
//  3. Operator pastes the per-component accuracy lines into the PR
//     description per .github/PULL_REQUEST_TEMPLATE.md.
//
// The runner intentionally has no CI presence — fork-PR security
// concerns + API-key exfiltration risks rule out a CI workflow. The
// PR template checkbox is the gate.
//
// This is the minimum-viable runner: it loads config, builds the
// configured LLM backend, runs the dataset, and prints accuracy. The
// `--backends` comparison flag and Langfuse `classify_prompt:<sha>`
// dataset-run annotation are parked as follow-ups (see IMPL-0019
// Phase 6 unchecked tasks).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/config"
	"github.com/donaldgifford/server-price-tracker/pkg/extract"
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
	Accuracy   float64           `json:"accuracy_percent"`
	Duration   time.Duration     `json:"duration"`
	PerComp    []componentResult `json:"per_component"`
	Mismatches []mismatch        `json:"mismatches,omitempty"`
}

type mismatch struct {
	Title    string               `json:"title"`
	Expected domain.ComponentType `json:"expected"`
	Actual   domain.ComponentType `json:"actual"`
	Error    string               `json:"error,omitempty"`
}

func main() {
	configPath := flag.String("config", "configs/config.dev.yaml", "path to YAML config file")
	datasetPath := flag.String("dataset", "testdata/golden_classifications.json", "path to golden dataset")
	jsonOut := flag.Bool("json", false, "emit JSON instead of a human-readable table")
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

	backend := buildBackend(cfg, logger)
	if backend == nil {
		fatal("could not construct LLM backend %q from config", cfg.LLM.Backend)
	}

	extractor := extract.NewLLMExtractor(backend, extract.WithLogger(logger))

	result := runDataset(context.Background(), extractor, dataset, cfg.LLM.Backend, modelOf(cfg))

	if *jsonOut {
		emitJSON(&result)
		return
	}
	emitTable(&result)
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

// buildBackend mirrors cmd/server-price-tracker/cmd/serve.go::buildLLMBackend
// closely. Kept inline (instead of exported) so the runner depends only
// on stable public extract.* constructors — refactoring serve.go later
// should not touch this file.
func buildBackend(cfg *config.Config, logger *slog.Logger) extract.LLMBackend {
	switch cfg.LLM.Backend {
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
		logger.Error("unknown LLM backend", "backend", cfg.LLM.Backend)
		return nil
	}
}

func modelOf(cfg *config.Config) string {
	switch cfg.LLM.Backend {
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
	correct := 0

	for i := range dataset {
		item := &dataset[i]
		bucket, ok := perComp[item.ExpectedComponent]
		if !ok {
			bucket = &componentResult{Component: item.ExpectedComponent}
			perComp[item.ExpectedComponent] = bucket
		}
		bucket.Total++

		actual, _, err := extractor.ClassifyAndExtract(ctx, item.Title, item.ItemSpecifics)
		if err != nil {
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
	if total := len(dataset); total > 0 {
		overall = float64(correct) / float64(total) * 100
	}

	return runResult{
		Backend:    backendName,
		Model:      modelName,
		Total:      len(dataset),
		Correct:    correct,
		Accuracy:   overall,
		Duration:   time.Since(start),
		PerComp:    results,
		Mismatches: mismatches,
	}
}

func emitTable(r *runResult) {
	fmt.Printf("Regression run — backend=%s model=%s duration=%s\n",
		r.Backend, r.Model, r.Duration.Round(time.Millisecond))
	fmt.Printf("Overall accuracy: %d/%d (%.1f%%)\n\n", r.Correct, r.Total, r.Accuracy)

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
