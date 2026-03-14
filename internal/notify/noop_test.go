package notify

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoOpNotifier_SendAlert(t *testing.T) {
	t.Parallel()

	n := NewNoOpNotifier(slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := n.SendAlert(context.Background(), &AlertPayload{
		WatchName:    "test-watch",
		ListingTitle: "Samsung 32GB DDR4",
		Score:        85,
	})
	require.NoError(t, err)
}

func TestNoOpNotifier_SendBatchAlert(t *testing.T) {
	t.Parallel()

	n := NewNoOpNotifier(slog.New(slog.NewTextHandler(io.Discard, nil)))
	alerts := []AlertPayload{
		{WatchName: "test-watch", ListingTitle: "Samsung 32GB DDR4", Score: 85},
		{WatchName: "test-watch", ListingTitle: "Micron 16GB DDR4", Score: 78},
	}

	err := n.SendBatchAlert(context.Background(), alerts, "test-watch")
	require.NoError(t, err)
}

func TestNoOpNotifier_SendBatchAlert_Empty(t *testing.T) {
	t.Parallel()

	n := NewNoOpNotifier(slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := n.SendBatchAlert(context.Background(), nil, "empty-watch")
	require.NoError(t, err)
}

// compile-time interface check.
var _ Notifier = (*NoOpNotifier)(nil)
