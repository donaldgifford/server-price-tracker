package client

import (
	"context"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ListBaselines returns all price baselines.
func (c *Client) ListBaselines(ctx context.Context) ([]domain.PriceBaseline, error) {
	var baselines []domain.PriceBaseline
	if err := c.get(ctx, "/api/v1/baselines", &baselines); err != nil {
		return nil, err
	}
	return baselines, nil
}

// GetBaseline returns a single baseline by product key.
func (c *Client) GetBaseline(
	ctx context.Context,
	productKey string,
) (*domain.PriceBaseline, error) {
	var b domain.PriceBaseline
	if err := c.get(ctx, "/api/v1/baselines/"+productKey, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// RefreshBaselines triggers a full baseline refresh.
func (c *Client) RefreshBaselines(ctx context.Context) error {
	return c.post(ctx, "/api/v1/baselines/refresh", nil, nil)
}

// TriggerIngestion triggers an immediate ingestion run.
func (c *Client) TriggerIngestion(ctx context.Context) error {
	return c.post(ctx, "/api/v1/ingest", nil, nil)
}
