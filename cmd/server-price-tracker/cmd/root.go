// Package cmd implements the CLI commands for server-price-tracker.
package cmd

import (
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	apiURL  string
)

var rootCmd = &cobra.Command{
	Use:   "server-price-tracker",
	Short: "Monitor eBay for server hardware deals",
	Long:  "An API-first service that monitors eBay listings for server hardware, extracts structured attributes via LLM, scores listings against price baselines, and sends deal alerts.",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "config file path")
	rootCmd.PersistentFlags().
		StringVar(&apiURL, "api-url", "http://localhost:8080", "API server URL")
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
