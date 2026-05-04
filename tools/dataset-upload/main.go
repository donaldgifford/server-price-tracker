// Package main is the operator-facing CLI for uploading the curated
// regression dataset (testdata/golden_classifications.json) to
// Langfuse as one DatasetItem per row.
//
// Workflow (IMPL-0019 Phase 6):
//
//  1. Operator runs tools/dataset-bootstrap → audits the JSON →
//     saves testdata/golden_classifications.json.
//
//  2. `go run ./tools/dataset-upload --config <path>
//     --langfuse-dataset-id <id>`
//     reads the JSON file and POSTs one DatasetItem per row. Each
//     item's ID is a deterministic sha256-trunc-8 hash of the row's
//     title — the same algorithm tools/regression-runner uses for
//     DatasetItemID in its CreateDatasetRun annotation, so runs and
//     items align without any other coordination.
//
//  3. Operator runs `make test-regression` (or
//     `tools/regression-runner --langfuse-dataset-id <id>`) and the
//     `classify_prompt:<sha>` annotation lands on the right items.
//
// The upload is idempotent — Langfuse treats explicit IDs as upserts,
// so re-running the tool after a dataset edit refreshes existing
// rows without duplicating them.
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
	"os"
	"path/filepath"

	"github.com/donaldgifford/server-price-tracker/internal/config"
	"github.com/donaldgifford/server-price-tracker/pkg/observability/langfuse"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// goldenItem mirrors the JSON shape testdata/golden_classifications.json
// uses. Kept in sync by hand with the same struct in
// tools/regression-runner; refactoring to a shared package is a
// follow-up.
type goldenItem struct {
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

func run() error {
	configPath := flag.String("config", "configs/config.dev.yaml", "path to YAML config file")
	datasetPath := flag.String(
		"dataset", "testdata/golden_classifications.json",
		"path to the labelled golden dataset",
	)
	langfuseDatasetID := flag.String(
		"langfuse-dataset-id", "",
		"Langfuse dataset ID to upload into (required)",
	)
	flag.Parse()

	if *langfuseDatasetID == "" {
		return errors.New("--langfuse-dataset-id is required")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("loading config %s: %w", *configPath, err)
	}
	if !cfg.Observability.Langfuse.Enabled {
		return errors.New("observability.langfuse.enabled must be true in config to upload")
	}

	dataset, err := loadDataset(*datasetPath)
	if err != nil {
		return fmt.Errorf("loading dataset %s: %w", *datasetPath, err)
	}
	if len(dataset) == 0 {
		return errors.New("dataset is empty — bootstrap with tools/dataset-bootstrap first")
	}

	client, err := langfuse.NewHTTPClient(
		cfg.Observability.Langfuse.Endpoint,
		cfg.Observability.Langfuse.PublicKey,
		cfg.Observability.Langfuse.SecretKey,
	)
	if err != nil {
		return fmt.Errorf("constructing Langfuse client: %w", err)
	}

	ctx := context.Background()
	uploaded := 0
	for i := range dataset {
		item := &dataset[i]
		if err := client.CreateDatasetItem(ctx, *langfuseDatasetID, datasetItem(item)); err != nil {
			logger.Warn("CreateDatasetItem failed; skipping",
				"title", truncate(item.Title, 60), "error", err)
			continue
		}
		uploaded++
	}

	logger.Info("upload finished",
		"uploaded", uploaded, "total", len(dataset), "dataset_id", *langfuseDatasetID)
	if uploaded == 0 {
		return errors.New("no items uploaded — check Langfuse credentials and endpoint reachability")
	}
	return nil
}

// datasetItem converts a goldenItem to a langfuse.DatasetItem, using
// the same deterministic title hash the regression-runner uses for
// DatasetItemID so runs and items align.
func datasetItem(g *goldenItem) *langfuse.DatasetItem {
	return &langfuse.DatasetItem{
		ID: titleHash(g.Title),
		Input: map[string]any{
			"title":          g.Title,
			"item_specifics": g.ItemSpecifics,
		},
		ExpectedOutput: map[string]any{
			"component_type": string(g.ExpectedComponent),
			"product_key":    g.ExpectedProductKey,
		},
		Metadata: map[string]string{
			"source": "golden_classifications.json",
		},
	}
}

// titleHash matches tools/regression-runner.titleHash. Kept duplicated
// (rather than exporting) so this small CLI stays free of cross-tool
// coupling; both copies are tested and the algorithm is unlikely to
// change.
func titleHash(title string) string {
	sum := sha256.Sum256([]byte(title))
	return hex.EncodeToString(sum[:8])
}

func loadDataset(path string) ([]goldenItem, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	// G304: path is operator-supplied via --dataset; this CLI is
	// operator-only with no untrusted input surface.
	raw, err := os.ReadFile(abs) //nolint:gosec // operator-supplied dataset path
	if err != nil {
		return nil, err
	}
	var items []goldenItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	return items, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
