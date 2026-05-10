package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	croncli "github.com/hrygo/hotplex/internal/cli/cron"
)

func newCronListCmd() *cobra.Command {
	var (
		configPath string
		jsonOutput bool
		enabled    bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cron jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(context.Background(), configPath, func(store croncli.Store) error {
				jobs, err := store.List(context.Background(), enabled)
				if err != nil {
					return err
				}

				if jsonOutput {
					return printJSON(jobs)
				}

				if len(jobs) == 0 {
					fmt.Println("No cron jobs found.")
					return nil
				}

				tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(tw, "ID\tNAME\tSCHEDULE\tENABLED\tNEXT RUN")
				for _, j := range jobs {
					enabledStr := "yes"
					if !j.Enabled {
						enabledStr = "no"
					}
					_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
						shortID(j.ID), j.Name, croncli.FormatSchedule(j.Schedule),
						enabledStr, croncli.FormatTimeMs(j.State.NextRunAtMs))
				}
				return tw.Flush()
			})
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&enabled, "enabled", false, "show enabled jobs only")
	return cmd
}
