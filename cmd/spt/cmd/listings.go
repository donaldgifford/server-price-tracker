package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	apiclient "github.com/donaldgifford/server-price-tracker/internal/api/client"
)

func listingsCmd() *cobra.Command {
	listingsRoot := &cobra.Command{
		Use:   "listings",
		Short: "Query listings",
		Long: "Query and inspect listings that have been ingested and scored\n" +
			"by the Server Price Tracker pipeline.",
	}

	listingsRoot.AddCommand(
		listingsListCmd(),
		listingsGetCmd(),
	)

	return listingsRoot
}

func listingsListCmd() *cobra.Command {
	var (
		componentType string
		productKey    string
		minScore      int
		maxScore      int
		limit         int
		offset        int
		orderBy       string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List listings with optional filters",
		Long: "List ingested listings with optional filters for component type,\n" +
			"product key, score range, and sorting.",
		Example: `  # List all listings
  spt listings list

  # Filter by component type and minimum score
  spt listings list --type ram --min-score 70

  # Sort by price with pagination
  spt listings list --order-by price --limit 20 --offset 40

  # Filter by product key
  spt listings list --product-key "ram:ddr4:ecc_reg:32gb:2666"`,
		RunE: func(_ *cobra.Command, _ []string) error {
			c := newClient()
			resp, err := c.ListListings(context.Background(), &apiclient.ListListingsParams{
				ComponentType: componentType,
				ProductKey:    productKey,
				MinScore:      minScore,
				MaxScore:      maxScore,
				Limit:         limit,
				Offset:        offset,
				OrderBy:       orderBy,
			})
			if err != nil {
				return err
			}

			if jsonOutput() {
				return outputJSON(resp)
			}

			if len(resp.Listings) == 0 {
				fmt.Println("No listings found.")
				return nil
			}

			fmt.Printf("Showing %d of %d listings\n\n", len(resp.Listings), resp.Total)
			return printListingsTable(resp.Listings)
		},
	}
	cmd.Flags().StringVar(&componentType, "type", "", "component type filter")
	cmd.Flags().StringVar(&productKey, "product-key", "", "product key filter")
	cmd.Flags().IntVar(&minScore, "min-score", 0, "minimum score filter")
	cmd.Flags().IntVar(&maxScore, "max-score", 0, "maximum score filter")
	cmd.Flags().IntVar(&limit, "limit", 50, "number of results")
	cmd.Flags().IntVar(&offset, "offset", 0, "result offset")
	cmd.Flags().
		StringVar(&orderBy, "order-by", "", "sort order (score, price, first_seen_at)")

	return cmd
}

func listingsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "get <id>",
		Short:   "Show listing details",
		Example: `  spt listings get abc123`,
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			l, err := c.GetListing(context.Background(), args[0])
			if err != nil {
				return err
			}

			if jsonOutput() {
				return outputJSON(l)
			}

			return printListingDetail(l)
		},
	}
}
