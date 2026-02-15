// Package main is the entry point for the server-price-tracker.
//
// @title Server Price Tracker API
// @version 1.0.0
// @description API for monitoring eBay server hardware listings, extracting structured attributes via LLM, scoring deals, and sending alerts.
//
// @contact.name Donald Gifford
// @contact.url https://github.com/donaldgifford/server-price-tracker
//
// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html
//
// @servers.url http://localhost:8080
// @servers.description Local development server
package main

import (
	"os"

	"github.com/donaldgifford/server-price-tracker/cmd/server-price-tracker/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
