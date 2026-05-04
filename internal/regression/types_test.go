package regression_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/server-price-tracker/internal/regression"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestTitleHash_Stable(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",
		"Dell PowerEdge R740xd 2.5\" SFF",
		"32GB DDR4 ECC RDIMM 2666MHz",
		"NVIDIA RTX 3090 24GB",
		"Dell — PowerEdge — R740xd",
	}

	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			a := regression.TitleHash(in)
			b := regression.TitleHash(in)
			assert.Equal(t, a, b, "deterministic")
			assert.Len(t, a, 16, "8-byte hex prefix")

			// Pin to the algorithm so a future refactor that changes
			// the digest breaks loudly. Both operator CLIs depend on
			// this exact hash for runs/items alignment in Langfuse.
			sum := sha256.Sum256([]byte(in))
			assert.Equal(t, hex.EncodeToString(sum[:8]), a)
		})
	}

	// Different inputs produce different IDs.
	assert.NotEqual(t, regression.TitleHash("foo"), regression.TitleHash("bar"))
}

func TestLoadDataset_MissingFileReturnsNilNil(t *testing.T) {
	t.Parallel()

	items, err := regression.LoadDataset(filepath.Join(t.TempDir(), "does-not-exist.json"))
	require.NoError(t, err)
	assert.Nil(t, items)
}

func TestLoadDataset_ParsesValidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "golden.json")
	body := `[
		{"title":"Dell R740xd","item_specifics":{"Brand":"Dell"},"expected_component":"server","expected_product_key":"server:dell:r740xd:sff:configured"},
		{"title":"32GB DDR4 ECC","item_specifics":{},"expected_component":"ram"}
	]`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	items, err := regression.LoadDataset(path)
	require.NoError(t, err)
	require.Len(t, items, 2)

	assert.Equal(t, "Dell R740xd", items[0].Title)
	assert.Equal(t, domain.ComponentServer, items[0].ExpectedComponent)
	assert.Equal(t, "server:dell:r740xd:sff:configured", items[0].ExpectedProductKey)
	assert.Equal(t, "Dell", items[0].ItemSpecifics["Brand"])

	assert.Equal(t, domain.ComponentRAM, items[1].ExpectedComponent)
	assert.Empty(t, items[1].ExpectedProductKey)
}

func TestLoadDataset_MalformedJSONErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "broken.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	_, err := regression.LoadDataset(path)
	require.Error(t, err)
}
