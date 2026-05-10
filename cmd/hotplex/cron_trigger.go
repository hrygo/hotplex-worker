package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	croncli "github.com/hrygo/hotplex/internal/cli/cron"
)

func newCronTriggerCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "trigger <id|name>",
		Short: "Trigger a cron job execution",
		Long: `Trigger an immediate execution of a cron job via the gateway admin API.

Requires the gateway to be running.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(context.Background(), configPath, func(store croncli.Store) error {
				job, err := croncli.ResolveJob(store, context.Background(), args[0])
				if err != nil {
					return err
				}

				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				if err := croncli.TriggerViaAdmin(ctx, configPath, job.ID); err != nil {
					return err
				}

				fmt.Printf("Triggered job %s (%s)\n", job.ID, job.Name)
				return nil
			})
		},
	}
	configFlag(cmd, &configPath)
	return cmd
}
