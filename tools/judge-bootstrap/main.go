// Package main is the operator-facing CLI for bootstrapping the
// LLM-as-judge few-shot example set (pkg/judge/examples.json).
//
// Two-step workflow (IMPL-0019 Phase 5):
//
//  1. `go run ./tools/judge-bootstrap --config <path> [--lookback 7d]
//     [--limit 30] > candidates.json`
//     pulls a stratified sample of recent alerts from the DB, converts
//     each to the same AlertContext shape the judge prompt consumes,
//     and emits a JSON array of candidates with empty `label` and
//     `verdict` fields for the operator to fill in.
//
//  2. Operator opens candidates.json, fills in:
//     - `label`: one of "deal" / "edge" / "noise"
//     - `verdict.score`: 0.0-1.0
//     - `verdict.reason`: short free-text justification (≤80 chars)
//     and saves the file (e.g., as labelled.json).
//
//  3. `go run ./tools/judge-bootstrap --apply labelled.json` reads,
//     validates, and writes pkg/judge/examples.json. Commit, redeploy,
//     and the new examples ship in the next judge tick.
//
// The fact that the *first* run with a zero-row examples.json works
// (the prompt template renders cleanly with no few-shot examples) is
// the design escape hatch — operators can opt into judge before
// labelling, then refresh examples.json once they have signal.
//
// The runner is operator-only; no CI surface, no untrusted input.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/donaldgifford/server-price-tracker/internal/config"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	"github.com/donaldgifford/server-price-tracker/pkg/judge"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// labelledCandidate is the JSON shape both modes use. In --list mode
// Label / Verdict are emitted as zero values for the operator to fill
// in; in --apply mode the operator-supplied values are validated and
// written through to pkg/judge/examples.json as a `judge.Example`.
type labelledCandidate struct {
	Label   string             `json:"label"`
	Alert   judge.AlertContext `json:"alert"`
	Verdict judge.Verdict      `json:"verdict"`
}

const examplesPath = "pkg/judge/examples.json"

var validLabels = []string{"deal", "edge", "noise"}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "configs/config.dev.yaml", "path to YAML config file")
	lookback := flag.Duration(
		"lookback", 7*24*time.Hour,
		"alerts.created_at window for the candidate query",
	)
	limit := flag.Int("limit", 30, "maximum number of candidates to emit")
	apply := flag.String(
		"apply", "",
		"path to a labelled candidate JSON file; when set, validate and write to "+examplesPath,
	)
	out := flag.String(
		"output", examplesPath,
		"target path for --apply mode (defaults to "+examplesPath+")",
	)
	flag.Parse()

	if *apply != "" {
		return runApply(*apply, *out)
	}
	return runList(*configPath, *lookback, *limit)
}

func runList(configPath string, lookback time.Duration, limit int) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config %s: %w", configPath, err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	ctx := context.Background()
	st, err := store.NewPostgresStore(ctx, cfg.Database.DSN())
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer st.Close()

	candidates, err := st.ListAlertsForJudging(ctx, &store.JudgeCandidatesQuery{
		Lookback: lookback,
		Limit:    limit,
	})
	if err != nil {
		return fmt.Errorf("listing alerts: %w", err)
	}
	if len(candidates) == 0 {
		fmt.Fprintln(os.Stderr, "no recent alerts matched — try increasing --lookback")
		return nil
	}
	logger.Info("pulled candidates", "count", len(candidates))

	out := stratify(candidates, limit)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	return nil
}

// stratify groups candidates by ComponentType and interleaves them so
// the operator's labelling queue isn't dominated by whichever type has
// the most recent volume. Limit caps the total emitted rows.
func stratify(candidates []domain.JudgeCandidate, limit int) []labelledCandidate {
	buckets := map[string][]labelledCandidate{}
	keys := []string{}
	for i := range candidates {
		c := &candidates[i]
		key := string(c.ComponentType)
		if _, ok := buckets[key]; !ok {
			keys = append(keys, key)
		}
		buckets[key] = append(buckets[key], labelledCandidate{
			Label: "",
			Alert: judge.AlertContext{
				AlertID:       c.AlertID,
				WatchName:     c.WatchName,
				ComponentType: c.ComponentType,
				ListingTitle:  c.ListingTitle,
				Condition:     c.Condition,
				PriceUSD:      c.PriceUSD,
				BaselineP25:   c.BaselineP25,
				BaselineP50:   c.BaselineP50,
				BaselineP75:   c.BaselineP75,
				SampleSize:    c.SampleSize,
				Score:         c.Score,
				Threshold:     c.Threshold,
				TraceID:       traceIDValue(c.TraceID),
				CreatedAt:     c.CreatedAt,
			},
		})
	}
	sort.Strings(keys)

	out := make([]labelledCandidate, 0, limit)
	round := 0
	for len(out) < limit {
		anyAdded := false
		for _, k := range keys {
			if round < len(buckets[k]) {
				out = append(out, buckets[k][round])
				anyAdded = true
				if len(out) >= limit {
					break
				}
			}
		}
		if !anyAdded {
			break
		}
		round++
	}
	return out
}

func traceIDValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func runApply(input, out string) error {
	// G304: input is operator-supplied via --apply; this CLI is
	// operator-only with no untrusted input surface.
	raw, err := os.ReadFile(input) //nolint:gosec // operator-supplied path
	if err != nil {
		return fmt.Errorf("reading %s: %w", input, err)
	}

	var labelled []labelledCandidate
	if err := json.Unmarshal(raw, &labelled); err != nil {
		return fmt.Errorf("parsing %s: %w", input, err)
	}
	if len(labelled) == 0 {
		return errors.New("input file has zero rows")
	}

	if err := validateLabels(labelled); err != nil {
		return err
	}

	examples := make([]judge.Example, 0, len(labelled))
	for i := range labelled {
		examples = append(examples, judge.Example{
			Label:   labelled[i].Label,
			Alert:   labelled[i].Alert,
			Verdict: labelled[i].Verdict,
		})
	}

	body, err := json.MarshalIndent(examples, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding examples: %w", err)
	}
	body = append(body, '\n')

	dir := filepath.Dir(out)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("ensuring output dir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(out, body, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", out, err)
	}

	fmt.Fprintf(os.Stderr, "wrote %d examples to %s\n", len(examples), out)
	return nil
}

func validateLabels(items []labelledCandidate) error {
	for i := range items {
		c := &items[i]
		if !slices.Contains(validLabels, c.Label) {
			return fmt.Errorf("row %d (alert_id=%s): label %q is not one of %v",
				i, c.Alert.AlertID, c.Label, validLabels)
		}
		if c.Verdict.Score < 0 || c.Verdict.Score > 1 {
			return fmt.Errorf("row %d (alert_id=%s): verdict.score %.2f is outside [0.0, 1.0]",
				i, c.Alert.AlertID, c.Verdict.Score)
		}
		if c.Verdict.Reason == "" {
			return fmt.Errorf("row %d (alert_id=%s): verdict.reason is empty",
				i, c.Alert.AlertID)
		}
	}
	return nil
}
