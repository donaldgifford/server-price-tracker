package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

var baselinesCmd = &cobra.Command{
	Use:   "baselines",
	Short: "Manage baselines",
}

var baselinesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all baselines",
	RunE:  runBaselinesList,
}

var baselinesShowCmd = &cobra.Command{
	Use:   "show [product-key]",
	Short: "Show baseline details",
	Args:  cobra.ExactArgs(1),
	RunE:  runBaselinesShow,
}

var baselinesRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Trigger baseline refresh",
	RunE:  runBaselinesRefresh,
}

func init() {
	rootCmd.AddCommand(baselinesCmd)
	baselinesCmd.AddCommand(baselinesListCmd, baselinesShowCmd, baselinesRefreshCmd)

	baselinesCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output as JSON")
}

func runBaselinesList(_ *cobra.Command, _ []string) error {
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
}

func runBaselinesShow(_ *cobra.Command, args []string) error {
	c := newClient()
	b, err := c.GetBaseline(context.Background(), args[0])
	if err != nil {
		return err
	}

	if jsonOutput {
		return outputJSON(b)
	}

	return printBaselineDetail(b)
}

func runBaselinesRefresh(_ *cobra.Command, _ []string) error {
	c := newClient()
	if err := c.RefreshBaselines(context.Background()); err != nil {
		return err
	}

	fmt.Println("Baseline refresh completed.")
	return nil
}
