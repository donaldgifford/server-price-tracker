package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

func extractCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "extract [title]",
		Short: "Extract structured attributes from an eBay listing title",
		Long:  "Sends a title to the API server for LLM-based classification and attribute extraction.",
		Args:  cobra.ExactArgs(1),
		RunE:  runExtract,
	}
}

type extractPayload struct {
	Title string `json:"title"`
}

func runExtract(cmd *cobra.Command, args []string) error {
	opts := getOptions()

	payload, err := json.Marshal(extractPayload{Title: args[0]})
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}

	apiURL := opts.APIURL + "/api/v1/extract"

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
		return fmt.Errorf("calling extract API: %w", err)
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
