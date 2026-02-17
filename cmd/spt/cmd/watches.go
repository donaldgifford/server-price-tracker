package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func watchCmd() *cobra.Command {
	watchRoot := &cobra.Command{
		Use:   "watches",
		Short: "Manage watches",
		Long: "Manage saved search watches that define eBay queries, component types,\n" +
			"filters, and score thresholds for deal alerts.",
	}

	watchRoot.AddCommand(
		watchListCmd(),
		watchGetCmd(),
		watchCreateCmd(),
		watchEnableCmd(),
		watchDisableCmd(),
		watchDeleteCmd(),
	)

	return watchRoot
}

func watchListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all watches",
		Example: `  spt watches list
  spt watches list --output json`,
		RunE: func(_ *cobra.Command, _ []string) error {
			c := newClient()
			watches, err := c.ListWatches(context.Background())
			if err != nil {
				return err
			}
			if jsonOutput() {
				return outputJSON(watches)
			}
			if len(watches) == 0 {
				fmt.Println("No watches found.")
				return nil
			}
			return printWatchTable(watches)
		},
	}
}

func watchGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show watch details",
		Example: `  spt watches get abc123
  spt watches get abc123 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			w, err := c.GetWatch(context.Background(), args[0])
			if err != nil {
				return err
			}
			if jsonOutput() {
				return outputJSON(w)
			}
			return printWatchDetail(w)
		},
	}
}

func watchCreateCmd() *cobra.Command {
	var (
		watchName       string
		watchQuery      string
		watchType       string
		watchThreshold  int
		watchFilterArgs []string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new watch",
		Long: "Create a new watch that defines an eBay search query, component type,\n" +
			"score threshold, and optional filters. The watch will be enabled by\n" +
			"default and start matching listings on the next ingestion cycle.",
		Example: `  # Create a basic RAM watch
  spt watches create --name "DDR4 ECC 32GB" --query "DDR4 ECC 32GB RDIMM" --type ram

  # Create a watch with a custom threshold and filters
  spt watches create --name "Dell R630" --query "Dell PowerEdge R630" \
    --type server --threshold 80 \
    --filter "min_price=100" --filter "max_price=500"`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if watchName == "" || watchQuery == "" {
				return fmt.Errorf("--name and --query are required")
			}
			filters, err := handlers.ParseFilters(watchFilterArgs)
			if err != nil {
				return fmt.Errorf("parsing filters: %w", err)
			}
			w := &domain.Watch{
				Name:           watchName,
				SearchQuery:    watchQuery,
				ComponentType:  domain.ComponentType(watchType),
				ScoreThreshold: watchThreshold,
				Filters:        filters,
				Enabled:        true,
			}
			c := newClient()
			created, err := c.CreateWatch(context.Background(), w)
			if err != nil {
				return err
			}
			if jsonOutput() {
				return outputJSON(created)
			}
			fmt.Printf("Watch created: %s (%s)\n", created.Name, created.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&watchName, "name", "", "watch name")
	cmd.Flags().StringVar(&watchQuery, "query", "", "eBay search query")
	cmd.Flags().
		StringVar(&watchType, "type", "", "component type (ram, drive, server, cpu, nic)")
	cmd.Flags().IntVar(&watchThreshold, "threshold", 75, "score threshold for alerts")
	cmd.Flags().StringArrayVar(&watchFilterArgs, "filter", nil, "filters (key=value)")

	return cmd
}

func watchEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "enable <id>",
		Short:   "Enable a watch",
		Example: `  spt watches enable abc123`,
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runWatchSetEnabled(args[0], true)
		},
	}
}

func watchDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "disable <id>",
		Short:   "Disable a watch",
		Example: `  spt watches disable abc123`,
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runWatchSetEnabled(args[0], false)
		},
	}
}

func watchDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <id>",
		Short:   "Delete a watch",
		Example: `  spt watches delete abc123`,
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			if err := c.DeleteWatch(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Watch %s deleted.\n", args[0])
			return nil
		},
	}
}

func runWatchSetEnabled(id string, enabled bool) error {
	c := newClient()
	if err := c.SetWatchEnabled(context.Background(), id, enabled); err != nil {
		return err
	}

	action := "enabled"
	if !enabled {
		action = "disabled"
	}
	fmt.Printf("Watch %s %s.\n", id, action)
	return nil
}
