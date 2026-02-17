package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func rescoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rescore",
		Short:   "Rescore all listings",
		Long:    "Recomputes the composite score for all listings using current baselines.",
		Example: `  spt rescore`,
		RunE: func(_ *cobra.Command, _ []string) error {
			c := newClient()
			scored, err := c.Rescore(context.Background())
			if err != nil {
				return err
			}

			fmt.Printf("Rescored %d listings.\n", scored)
			return nil
		},
	}
}
