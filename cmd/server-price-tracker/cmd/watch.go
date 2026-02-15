package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	apiclient "github.com/donaldgifford/server-price-tracker/internal/api/client"
	"github.com/donaldgifford/server-price-tracker/internal/api/handlers"
	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

func watchCommand() *cobra.Command {
	var jsonOutput bool

	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Manage watches",
	}
	watchCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	watchCmd.AddCommand(
		watchAddCommand(&jsonOutput),
		&cobra.Command{
			Use:   "list",
			Short: "List all watches",
			RunE: func(_ *cobra.Command, _ []string) error {
				c := newClient()
				watches, err := c.ListWatches(context.Background())
				if err != nil {
					return err
				}
				if jsonOutput {
					return outputJSON(watches)
				}
				if len(watches) == 0 {
					fmt.Println("No watches found.")
					return nil
				}
				return printWatchTable(watches)
			},
		},
		&cobra.Command{
			Use:   "show [id]",
			Short: "Show watch details",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				c := newClient()
				w, err := c.GetWatch(context.Background(), args[0])
				if err != nil {
					return err
				}
				if jsonOutput {
					return outputJSON(w)
				}
				return printWatchDetail(w)
			},
		},
		&cobra.Command{
			Use:   "enable [id]",
			Short: "Enable a watch",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return runWatchSetEnabled(args[0], true)
			},
		},
		&cobra.Command{
			Use:   "disable [id]",
			Short: "Disable a watch",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return runWatchSetEnabled(args[0], false)
			},
		},
		&cobra.Command{
			Use:   "remove [id]",
			Short: "Remove a watch",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				c := newClient()
				if err := c.DeleteWatch(context.Background(), args[0]); err != nil {
					return err
				}
				fmt.Printf("Watch %s removed.\n", args[0])
				return nil
			},
		},
	)

	return watchCmd
}

func watchAddCommand(jsonOutput *bool) *cobra.Command {
	var (
		watchName       string
		watchQuery      string
		watchType       string
		watchThreshold  int
		watchFilterArgs []string
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a new watch",
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
			if *jsonOutput {
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

func newClient() *apiclient.Client {
	opts := getOptions()
	return apiclient.New(opts.APIURL)
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
