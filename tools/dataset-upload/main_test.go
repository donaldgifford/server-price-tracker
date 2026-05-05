package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/server-price-tracker/internal/regression"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func TestDatasetItem_PopulatesIDAndShape(t *testing.T) {
	t.Parallel()

	g := &regression.Item{
		Title:              "Dell R740xd",
		ItemSpecifics:      map[string]string{"Brand": "Dell"},
		ExpectedComponent:  domain.ComponentServer,
		ExpectedProductKey: "server:dell:r740xd:sff:configured",
	}

	item := datasetItem(g)
	assert.Equal(t, regression.TitleHash("Dell R740xd"), item.ID)
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
