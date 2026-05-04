// Package regression carries the shared shape of the
// `testdata/golden_classifications.json` regression dataset and the
// helpers all four operator CLIs (`tools/dataset-bootstrap`,
// `tools/dataset-upload`, `tools/regression-runner`, plus the build-
// tagged `pkg/extract/regression_test.go` stub) consume.
//
// Splitting these out of the individual tools eliminates the
// "kept in sync by hand" tax — a divergence between any two copies
// silently breaks the Phase 6 pipeline (uploaded `DatasetItem` IDs
// have to match `DatasetRunItem` IDs, which depends on every CLI
// computing `TitleHash` the same way).
package regression

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// Item is one row in `testdata/golden_classifications.json`. The
// JSON tags are load-bearing — they're the on-disk format every
// operator CLI agrees on.
type Item struct {
	Title              string               `json:"title"`
	ItemSpecifics      map[string]string    `json:"item_specifics"`
	ExpectedComponent  domain.ComponentType `json:"expected_component"`
	ExpectedProductKey string               `json:"expected_product_key,omitempty"`
}

// TitleHash returns the deterministic dataset-item ID derived from a
// listing title. sha256-trunc-8 hex (16 chars). Stable across all
// operator CLIs so DatasetItem uploads and DatasetRun annotations
// align in Langfuse without any out-of-band coordination.
//
// Changing the algorithm here is a breaking change for every
// previously-uploaded Langfuse dataset — the new IDs won't match
// existing items. Don't.
func TitleHash(title string) string {
	sum := sha256.Sum256([]byte(title))
	return hex.EncodeToString(sum[:8])
}

// LoadDataset reads and parses the golden dataset at path. Returns
// (nil, nil) when the file doesn't exist so callers can skip cleanly
// on a fresh checkout — fail-soft is the right call because the
// dataset is an operator-bootstrapped artifact, not a build input.
func LoadDataset(path string) ([]Item, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	// G304: path is operator-supplied via a CLI flag; these tools
	// have no untrusted input surface.
	raw, err := os.ReadFile(abs) //nolint:gosec // operator-supplied dataset path
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Item
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	return items, nil
}
