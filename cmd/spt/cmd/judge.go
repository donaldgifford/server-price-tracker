package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// judgeCmd is the parent command for LLM-as-judge operations. Today
// only `run` is exposed; future subcommands (e.g. status, replay) can
// be added here without disturbing the root command file.
func judgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "judge",
		Short: "LLM-as-judge worker controls",
		Long:  "Manual triggers and inspection helpers for the LLM-as-judge worker (DESIGN-0016 / IMPL-0019 Phase 5).",
	}
	cmd.AddCommand(judgeRunCmd())
	return cmd
}

// judgeRunCmd issues POST /api/v1/judge/run against the configured
// server and prints the verdict count plus a budget-exhausted flag
// when applicable. Honours the server-side daily USD cap; the CLI
// never tries to second-guess it.
func judgeRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "run",
		Short:   "Run one tick of the LLM-as-judge worker",
		Long:    "Triggers one synchronous run of the LLM-as-judge worker against the configured server. Prints the number of alerts judged plus a budget-exhausted flag when the daily USD cap halts the run early.",
		Example: `  spt judge run`,
		RunE: func(_ *cobra.Command, _ []string) error {
			c := newClient()
			res, err := c.RunJudge(context.Background())
			if err != nil {
				return err
			}
			fmt.Printf("Judged %d alerts.\n", res.Judged)
			if res.BudgetExhausted {
				fmt.Println("Daily budget exhausted — remaining alerts will be picked up next run.")
			}
			return nil
		},
	}
}
