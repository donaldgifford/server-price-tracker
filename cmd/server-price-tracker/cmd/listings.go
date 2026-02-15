package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	apiclient "github.com/donaldgifford/server-price-tracker/internal/api/client"
)

func listingsCommand() *cobra.Command {
	var jsonOutput bool

	listingsCmd := &cobra.Command{
		Use:   "listings",
		Short: "Manage listings",
	}
	listingsCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	var (
		listComponentType string
		listProductKey    string
		listMinScore      int
		listMaxScore      int
		listLimit         int
		listOffset        int
		listOrderBy       string
	)

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List listings",
		RunE: func(_ *cobra.Command, _ []string) error {
			c := newClient()
			resp, err := c.ListListings(context.Background(), &apiclient.ListListingsParams{
				ComponentType: listComponentType,
				ProductKey:    listProductKey,
				MinScore:      listMinScore,
				MaxScore:      listMaxScore,
				Limit:         listLimit,
				Offset:        listOffset,
				OrderBy:       listOrderBy,
			})
			if err != nil {
				return err
			}

			if jsonOutput {
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
	listCmd.Flags().StringVar(&listComponentType, "type", "", "component type filter")
	listCmd.Flags().StringVar(&listProductKey, "product-key", "", "product key filter")
	listCmd.Flags().IntVar(&listMinScore, "min-score", 0, "minimum score filter")
	listCmd.Flags().IntVar(&listMaxScore, "max-score", 0, "maximum score filter")
	listCmd.Flags().IntVar(&listLimit, "limit", 50, "number of results")
	listCmd.Flags().IntVar(&listOffset, "offset", 0, "result offset")
	listCmd.Flags().
		StringVar(&listOrderBy, "order-by", "", "sort order (score, price, first_seen_at)")

	showCmd := &cobra.Command{
		Use:   "show [id]",
		Short: "Show listing details",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			l, err := c.GetListing(context.Background(), args[0])
			if err != nil {
				return err
			}

			if jsonOutput {
				return outputJSON(l)
			}

			return printListingDetail(l)
		},
	}

	rescoreCmd := &cobra.Command{
		Use:   "rescore",
		Short: "Rescore all listings",
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

	listingsCmd.AddCommand(listCmd, showCmd, rescoreCmd)

	return listingsCmd
}
