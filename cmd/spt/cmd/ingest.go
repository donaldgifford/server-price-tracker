package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func ingestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ingest",
		Short: "Trigger manual ingestion",
		Long:  "Triggers the ingestion pipeline to poll eBay for all enabled watches.",
		RunE: func(_ *cobra.Command, _ []string) error {
			c := newClient()
			if err := c.TriggerIngestion(context.Background()); err != nil {
				return err
			}

			fmt.Println("Ingestion triggered.")
			return nil
		},
	}
}
