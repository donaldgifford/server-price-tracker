package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func jobsCmd() *cobra.Command {
	jobsRoot := &cobra.Command{
		Use:   "jobs",
		Short: "View scheduler job history",
		Long: "View the execution history of scheduled jobs (ingestion, baseline_refresh,\n" +
			"re_extraction). Each job records status, duration, and any errors.",
	}

	jobsRoot.AddCommand(
		jobsListCmd(),
		jobsHistoryCmd(),
	)

	return jobsRoot
}

func jobsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List latest run per job",
		Example: `  spt jobs list
  spt jobs list --output json`,
		RunE: func(_ *cobra.Command, _ []string) error {
			c := newClient()
			runs, err := c.ListJobs(context.Background())
			if err != nil {
				return err
			}
			if jsonOutput() {
				return outputJSON(runs)
			}
			if len(runs) == 0 {
				fmt.Println("No job runs found.")
				return nil
			}
			return printJobRunsTable(runs)
		},
	}
}

func jobsHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <job_name>",
		Short: "Show run history for a job",
		Args:  cobra.ExactArgs(1),
		Example: `  spt jobs history ingestion
  spt jobs history baseline_refresh --output json`,
		RunE: func(_ *cobra.Command, args []string) error {
			c := newClient()
			runs, err := c.GetJobHistory(context.Background(), args[0])
			if err != nil {
				return err
			}
			if jsonOutput() {
				return outputJSON(runs)
			}
			if len(runs) == 0 {
				fmt.Printf("No runs found for job %q.\n", args[0])
				return nil
			}
			return printJobRunsTable(runs)
		},
	}
}
