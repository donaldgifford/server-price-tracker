package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func searchCmd() *cobra.Command {
	var searchLimit int

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search eBay for server hardware listings",
		Long:  "Sends a search request to the API server and displays raw eBay results.",
		Example: `  spt search "DDR4 ECC 32GB RDIMM"
  spt search "Dell PowerEdge R630" --limit 25`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, args[0], searchLimit)
		},
	}
	cmd.Flags().IntVar(&searchLimit, "limit", 10, "maximum number of results")

	return cmd
}

type searchPayload struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func runSearch(cmd *cobra.Command, query string, limit int) error {
	payload, err := json.Marshal(searchPayload{
		Query: query,
		Limit: limit,
	})
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}

	apiURL := viper.GetString("server") + "/api/v1/search"

	req, err := http.NewRequestWithContext(
		cmd.Context(),
		http.MethodPost,
		apiURL,
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling search API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned %d: %s", resp.StatusCode, body)
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		fmt.Println(string(body))
		return nil
	}

	fmt.Println(pretty.String())
	return nil
}
