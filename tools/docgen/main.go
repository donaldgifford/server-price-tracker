// Package main generates CLI reference documentation from the spt command tree.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra/doc"

	"github.com/donaldgifford/server-price-tracker/cmd/spt/cmd"
)

func main() {
	output := flag.String("output", "docs/cli", "output directory for generated markdown")
	flag.Parse()

	if err := os.MkdirAll(*output, 0o750); err != nil {
		log.Fatalf("creating output directory: %v", err)
	}

	root := cmd.Root()
	root.DisableAutoGenTag = true

	if err := doc.GenMarkdownTree(root, *output); err != nil {
		log.Fatalf("generating docs: %v", err)
	}

	fmt.Printf("CLI docs generated in %s/\n", *output)
}
