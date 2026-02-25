// Package cmd implements the spt CLI commands.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	apiclient "github.com/donaldgifford/server-price-tracker/internal/api/client"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:   "spt",
		Short: "CLI client for Server Price Tracker",
		Long: "spt is a command-line client for the Server Price Tracker API.\n" +
			"It lets you manage watches, query listings, trigger ingestion,\n" +
			"and run extractions from the terminal.",
	}
)

// Root returns the root cobra command for documentation generation.
func Root() *cobra.Command {
	return rootCmd
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().
		StringVar(&cfgFile, "config", "", "config file (default $HOME/.spt.yaml)")
	rootCmd.PersistentFlags().
		String("server", "http://localhost:8080", "API server URL")
	rootCmd.PersistentFlags().
		String("output", "table", "output format (table, json)")

	cobra.CheckErr(viper.BindPFlag("server", rootCmd.PersistentFlags().Lookup("server")))
	cobra.CheckErr(viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output")))

	rootCmd.AddCommand(watchCmd())
	rootCmd.AddCommand(listingsCmd())
	rootCmd.AddCommand(searchCmd())
	rootCmd.AddCommand(extractCmd())
	rootCmd.AddCommand(baselinesCmd())
	rootCmd.AddCommand(ingestCmd())
	rootCmd.AddCommand(rescoreCmd())
	rootCmd.AddCommand(reextractCmd())
	rootCmd.AddCommand(jobsCmd())
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".spt")
	}

	viper.SetEnvPrefix("SPT")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

func newClient() *apiclient.Client {
	return apiclient.New(viper.GetString("server"))
}

func jsonOutput() bool {
	return viper.GetString("output") == "json"
}
