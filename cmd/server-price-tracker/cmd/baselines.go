package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func baselinesCommand() *cobra.Command {
	var jsonOutput bool

	baselinesCmd := &cobra.Command{
		Use:   "baselines",
		Short: "Manage baselines",
	}
	baselinesCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all baselines",
		RunE: func(_ *cobra.Command, _ []string) error {
			c := newClient()
			baselines, err := c.ListBaselines(context.Background())
			if err != nil {
				return err
			}

			if jsonOutput {
				return outputJSON(baselines)
			}

			if len(baselines) == 0 {
				fmt.Println("No baselines found.")
				return nil
			}

			return printBaselinesTable(baselines)
		},
	}

	showCmd := &cobra.Command{
		Use:   "show [product-key]",
		Short: "Show baseline details",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			b, err := c.GetBaseline(context.Background(), args[0])
			if err != nil {
				return err
			}

			if jsonOutput {
				return outputJSON(b)
			}

			return printBaselineDetail(b)
		},
	}

	refreshCmd := &cobra.Command{
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

	baselinesCmd.AddCommand(listCmd, showCmd, refreshCmd)

	return baselinesCmd
}
