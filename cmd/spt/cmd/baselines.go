package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func baselinesCmd() *cobra.Command {
	baselinesRoot := &cobra.Command{
		Use:   "baselines",
		Short: "Manage price baselines",
	}

	baselinesRoot.AddCommand(
		baselinesListCmd(),
		baselinesGetCmd(),
		baselinesRefreshCmd(),
	)

	return baselinesRoot
}

func baselinesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all baselines",
		RunE: func(_ *cobra.Command, _ []string) error {
			c := newClient()
			baselines, err := c.ListBaselines(context.Background())
			if err != nil {
				return err
			}

			if jsonOutput() {
				return outputJSON(baselines)
			}

			if len(baselines) == 0 {
				fmt.Println("No baselines found.")
				return nil
			}

			return printBaselinesTable(baselines)
		},
	}
}

func baselinesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <product-key>",
		Short: "Show baseline details",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			b, err := c.GetBaseline(context.Background(), args[0])
			if err != nil {
				return err
			}

			if jsonOutput() {
				return outputJSON(b)
			}

			return printBaselineDetail(b)
		},
	}
}

func baselinesRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Trigger baseline refresh",
		RunE: func(_ *cobra.Command, _ []string) error {
			c := newClient()
			if err := c.RefreshBaselines(context.Background()); err != nil {
				return err
			}

			fmt.Println("Baseline refresh completed.")
			return nil
		},
	}
}
