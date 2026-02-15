// Package cmd implements the CLI commands for server-price-tracker.
package cmd

import "github.com/danielgtaylor/huma/v2/humacli"

// Options defines the CLI options for the server-price-tracker service.
// humacli binds these to flags and environment variables (SERVICE_ prefix).
type Options struct {
	Config string `doc:"Config file path"                short:"c" default:"config.yaml"`
	APIURL string `doc:"API server URL for CLI commands"           default:"http://localhost:8080"`
}

var (
	cli        humacli.CLI
	parsedOpts *Options
)

// Run initializes and runs the CLI.
func Run() {
	cli = humacli.New(func(hooks humacli.Hooks, opts *Options) {
		// Store parsed options so subcommands can access them via getOptions().
		parsedOpts = opts

		hooks.OnStart(func() {
			if err := startServer(opts); err != nil {
				panic(err)
			}
		})
	})

	// Add subcommands.
	root := cli.Root()
	root.Use = "server-price-tracker"
	root.Short = "Monitor eBay for server hardware deals"
	root.Long = "An API-first service that monitors eBay listings for server hardware, " +
		"extracts structured attributes via LLM, scores listings against price baselines, " +
		"and sends deal alerts."

	root.AddCommand(migrateCommand())
	root.AddCommand(versionCommand())
	root.AddCommand(watchCommand())
	root.AddCommand(listingsCommand())
	root.AddCommand(searchCommand())
	root.AddCommand(extractCommand())
	root.AddCommand(baselinesCommand())

	cli.Run()
}

// getOptions returns the parsed CLI options.
// Safe to call from any subcommand's RunE — options are populated
// by humacli's PersistentPreRun before any command executes.
func getOptions() *Options {
	return parsedOpts
}
