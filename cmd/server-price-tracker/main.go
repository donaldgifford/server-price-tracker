// Package main is the entry point for the server-price-tracker.
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
