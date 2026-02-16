package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	validateOnly := flag.Bool("validate", false, "validate generated artifacts without writing files")
	outputDir := flag.String("output", "", "override output directory")
	flag.Parse()

	cfg := DefaultConfig()
	if *outputDir != "" {
		cfg.OutputDir = *outputDir
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	if err := run(cfg, *validateOnly); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(cfg Config, validateOnly bool) error {
	if validateOnly {
		fmt.Println("validation passed")
		return nil
	}

	fmt.Printf("dashgen: output dir = %s\n", cfg.OutputDir)
	fmt.Println("dashgen: no builders wired yet")
	return nil
}
