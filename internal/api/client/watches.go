package client

import (
	"context"
	"fmt"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// ListWatches returns all watches.
func (c *Client) ListWatches(ctx context.Context) ([]domain.Watch, error) {
	var watches []domain.Watch
	if err := c.get(ctx, "/api/v1/watches", &watches); err != nil {
		return nil, err
	}
	return watches, nil
}

// GetWatch returns a single watch by ID.
func (c *Client) GetWatch(ctx context.Context, id string) (*domain.Watch, error) {
	var w domain.Watch
	if err := c.get(ctx, "/api/v1/watches/"+id, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// CreateWatch creates a new watch.
func (c *Client) CreateWatch(ctx context.Context, w *domain.Watch) (*domain.Watch, error) {
	var created domain.Watch
	if err := c.post(ctx, "/api/v1/watches", w, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// UpdateWatch updates an existing watch.
func (c *Client) UpdateWatch(ctx context.Context, w *domain.Watch) (*domain.Watch, error) {
	var updated domain.Watch
	if err := c.put(ctx, "/api/v1/watches/"+w.ID, w, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

// SetWatchEnabled enables or disables a watch.
func (c *Client) SetWatchEnabled(ctx context.Context, id string, enabled bool) error {
	body := map[string]bool{"enabled": enabled}
	return c.put(ctx, fmt.Sprintf("/api/v1/watches/%s/enabled", id), body, nil)
}

// DeleteWatch deletes a watch by ID.
func (c *Client) DeleteWatch(ctx context.Context, id string) error {
	return c.del(ctx, "/api/v1/watches/"+id, nil)
}
