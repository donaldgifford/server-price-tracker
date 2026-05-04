// Package main is the operator-facing CLI for bootstrapping the
// regression dataset (testdata/golden_classifications.json).
//
// Workflow (IMPL-0019 Phase 6):
//
//  1. `go run ./tools/dataset-bootstrap --config <path>
//     [--per-component 12] > candidates.json`
//     pulls a stratified sample of recent listings from the DB,
//     defaults to 12 per ComponentType. The output is the same JSON
//     shape testdata/golden_classifications.json expects, with
//     `expected_component` pre-filled from each listing's existing
//     `component_type` and `expected_product_key` from `product_key`.
//
//  2. Operator opens candidates.json, audits each row, corrects any
//     misclassifications by hand, deletes any rows that aren't
//     suitable for regression (e.g., ambiguous accessory bundles),
//     and saves as testdata/golden_classifications.json.
//
//  3. `make test-regression` (which runs tools/regression-runner)
//     uses the saved file as the gating dataset for prompt-affecting
//     PRs.
//
// The point of the pre-fill is that the LLM has already done the
// classification work for the operator on every active listing — the
// operator's job is to *audit* (catch bad LLM labels) rather than
// label from scratch. Stratification keeps the sample balanced so a
// rare ComponentType doesn't get drowned out by RAM listings.
//
// The runner is operator-only; no CI surface, no untrusted input.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"

	"github.com/donaldgifford/server-price-tracker/internal/config"
	"github.com/donaldgifford/server-price-tracker/internal/store"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// candidateItem mirrors the goldenItem shape consumed by
// tools/regression-runner and pkg/extract/regression_test.go. Kept in
// sync by hand — there's no shared package for these test types.
type candidateItem struct {
	Title              string               `json:"title"`
	ItemSpecifics      map[string]string    `json:"item_specifics"`
	ExpectedComponent  domain.ComponentType `json:"expected_component"`
	ExpectedProductKey string               `json:"expected_product_key,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run is the real entry point. Split out from main so deferred closes
// run before any non-zero exit (gocritic exitAfterDefer).
func run() error {
	configPath := flag.String("config", "configs/config.dev.yaml", "path to YAML config file")
	perComponent := flag.Int(
		"per-component", 12,
		"target sample size per ComponentType (stratified across all active types)",
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("loading config %s: %w", *configPath, err)
	}

	ctx := context.Background()
	st, err := store.NewPostgresStore(ctx, cfg.Database.DSN())
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer st.Close()

	candidates := stratifiedSample(ctx, st, *perComponent, logger)
	if len(candidates) == 0 {
		fmt.Fprintln(os.Stderr, "no listings matched — is the database populated?")
		return nil
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(candidates); err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	return nil
}

// stratifiedSample queries the store once per known ComponentType and
// concatenates the results. Returning per-type buckets keeps the
// output sorted by ComponentType so the operator can audit category-
// by-category. ComponentTypes with no matching listings are skipped
// silently — the operator can re-run after seeding more data. Per-
// type query failures are logged but never abort the run because a
// fresh checkout may not have all ComponentTypes populated yet.
func stratifiedSample(
	ctx context.Context,
	st store.Store,
	perComponent int,
	logger *slog.Logger,
) []candidateItem {
	types := []domain.ComponentType{
		domain.ComponentRAM,
		domain.ComponentDrive,
		domain.ComponentServer,
		domain.ComponentCPU,
		domain.ComponentNIC,
		domain.ComponentGPU,
		domain.ComponentWorkstation,
		domain.ComponentDesktop,
		domain.ComponentOther,
	}

	out := make([]candidateItem, 0, perComponent*len(types))
	for _, ct := range types {
		ctStr := string(ct)
		listings, _, err := st.ListListings(ctx, &store.ListingQuery{
			ComponentType: &ctStr,
			Limit:         perComponent,
			OrderBy:       "first_seen_at",
		})
		if err != nil {
			logger.Warn("listing query failed; skipping bucket",
				"component_type", ct, "error", err)
			continue
		}
		for i := range listings {
			out = append(out, candidateFromListing(&listings[i]))
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ExpectedComponent < out[j].ExpectedComponent
	})

	return out
}

// candidateFromListing builds a candidate row from a domain.Listing.
// item_specifics is left empty because the original eBay specifics
// aren't persisted on the listings row — the operator can attach them
// manually if a particular row needs them for accurate audit.
func candidateFromListing(l *domain.Listing) candidateItem {
	return candidateItem{
		Title:              l.Title,
		ItemSpecifics:      map[string]string{},
		ExpectedComponent:  l.ComponentType,
		ExpectedProductKey: l.ProductKey,
	}
}
