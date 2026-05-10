package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	croncli "github.com/hrygo/hotplex/internal/cli/cron"
)

func newCronGetCmd() *cobra.Command {
	var (
		configPath string
		jsonOutput bool
	)
	cmd := &cobra.Command{
		Use:   "get <id|name>",
		Short: "Get cron job details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(context.Background(), configPath, func(store croncli.Store) error {
				job, err := croncli.ResolveJob(store, context.Background(), args[0])
				if err != nil {
					return err
				}

				if jsonOutput {
					return printJSON(job)
				}

				tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintf(tw, "ID:\t%s\n", job.ID)
				_, _ = fmt.Fprintf(tw, "Name:\t%s\n", job.Name)
				if job.Description != "" {
					_, _ = fmt.Fprintf(tw, "Description:\t%s\n", job.Description)
				}
				_, _ = fmt.Fprintf(tw, "Schedule:\t%s\n", croncli.FormatSchedule(job.Schedule))
				_, _ = fmt.Fprintf(tw, "Enabled:\t%v\n", job.Enabled)
				_, _ = fmt.Fprintf(tw, "Message:\t%s\n", job.Payload.Message)
				if job.WorkDir != "" {
					_, _ = fmt.Fprintf(tw, "Work Dir:\t%s\n", job.WorkDir)
				}
				_, _ = fmt.Fprintf(tw, "Bot ID:\t%s\n", job.BotID)
				_, _ = fmt.Fprintf(tw, "Owner ID:\t%s\n", job.OwnerID)
				if job.TimeoutSec > 0 {
					_, _ = fmt.Fprintf(tw, "Timeout:\t%ds\n", job.TimeoutSec)
				}
				if job.Silent {
					_, _ = fmt.Fprintf(tw, "Silent:\t%v\n", job.Silent)
				}
				_, _ = fmt.Fprintf(tw, "Next Run:\t%s\n", croncli.FormatTimeMs(job.State.NextRunAtMs))
				_, _ = fmt.Fprintf(tw, "Last Run:\t%s\n", croncli.FormatTimeMs(job.State.LastRunAtMs))
				if job.State.LastStatus != "" {
					_, _ = fmt.Fprintf(tw, "Last Status:\t%s\n", job.State.LastStatus)
				}
				if job.State.ConsecutiveErrs > 0 {
					_, _ = fmt.Fprintf(tw, "Consecutive Errors:\t%d\n", job.State.ConsecutiveErrs)
				}
				_, _ = fmt.Fprintf(tw, "Created:\t%s\n", croncli.FormatTimeMs(job.CreatedAtMs))
				_, _ = fmt.Fprintf(tw, "Updated:\t%s\n", croncli.FormatTimeMs(job.UpdatedAtMs))
				return tw.Flush()
			})
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
