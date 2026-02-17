package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func reextractCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reextract",
		Short: "Re-extract listings with incomplete data",
		Long: "Re-runs LLM extraction on listings with quality issues\n" +
			"(e.g., missing RAM speed from PC module numbers).",
		Example: `  spt reextract
  spt reextract --type ram
  spt reextract --type ram --limit 50`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			componentType, err := cmd.Flags().GetString("type")
			if err != nil {
				return err
			}
			limit, err := cmd.Flags().GetInt("limit")
			if err != nil {
				return err
			}

			c := newClient()
			count, err := c.ReExtract(context.Background(), componentType, limit)
			if err != nil {
				return err
			}

			fmt.Printf("Re-extracted %d listings.\n", count)
			return nil
		},
	}

	cmd.Flags().String("type", "", "component type filter (e.g., ram, drive, cpu)")
	cmd.Flags().Int("limit", 0, "max listings to process (default 100)")

	return cmd
}
