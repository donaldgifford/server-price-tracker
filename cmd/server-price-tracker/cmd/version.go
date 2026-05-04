package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/donaldgifford/server-price-tracker/internal/version"
)

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("server-price-tracker %s (%s)\n", version.Semver, version.CommitSHA)
		},
	}
}
