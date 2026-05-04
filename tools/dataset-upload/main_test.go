package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestTitleHash_DeterministicAndStable(t *testing.T) {
	t.Parallel()

	titles := []string{
		"Dell PowerEdge R740xd 2.5\" SFF",
		"32GB DDR4 ECC RDIMM 2666MHz",
		"NVIDIA RTX 3090 24GB",
		"",
	}

	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			t.Parallel()
			a := titleHash(title)
			b := titleHash(title)
			assert.Equal(t, a, b, "deterministic")
			assert.Len(t, a, 16, "8-byte hex prefix == 16 chars")

			// Hand-check that we're using the same algorithm as
			// tools/regression-runner — a divergence here breaks the
			// runs-and-items alignment that this entire pipeline depends
			// on.
			sum := sha256.Sum256([]byte(title))
			expected := hex.EncodeToString(sum[:8])
			assert.Equal(t, expected, a)
		})
	}
}

func TestDatasetItem_PopulatesIDAndShape(t *testing.T) {
	t.Parallel()

	g := &goldenItem{
		Title:              "Dell R740xd",
		ItemSpecifics:      map[string]string{"Brand": "Dell"},
		ExpectedComponent:  domain.ComponentServer,
		ExpectedProductKey: "server:dell:r740xd:sff:configured",
	}

	item := datasetItem(g)
	assert.Equal(t, titleHash("Dell R740xd"), item.ID)
	assert.Equal(t, "Dell R740xd", item.Input["title"])
	assert.Equal(t, "server", item.ExpectedOutput["component_type"])
	assert.Equal(t, "server:dell:r740xd:sff:configured", item.ExpectedOutput["product_key"])
	assert.Equal(t, "golden_classifications.json", item.Metadata["source"])
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "hello", truncate("hello", 10))
	assert.Equal(t, "hello", truncate("hello", 5))
	assert.Equal(t, "hello...", truncate("hello world", 8))
}
