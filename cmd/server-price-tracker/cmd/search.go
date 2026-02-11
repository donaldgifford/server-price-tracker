package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/donaldgifford/server-price-tracker/internal/config"
)

var searchLimit int

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search eBay for server hardware listings",
	Long:  "Sends a search request to the API server and displays raw eBay results.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	searchCmd.Flags().IntVar(&searchLimit, "limit", 10, "maximum number of results")
	rootCmd.AddCommand(searchCmd)
}

type searchPayload struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func runSearch(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	payload, err := json.Marshal(searchPayload{
		Query: args[0],
		Limit: searchLimit,
	})
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}

	apiURL := fmt.Sprintf(
		"http://%s:%d/api/v1/search",
		cfg.Server.Host,
		cfg.Server.Port,
	)

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
		// Fall back to raw output if JSON indentation fails.
		fmt.Println(string(body))
		return nil
	}

	fmt.Println(pretty.String())
	return nil
}
