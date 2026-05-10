package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	croncli "github.com/hrygo/hotplex/internal/cli/cron"
)

func newCronDeleteCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "delete <id|name>",
		Short: "Delete a cron job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(context.Background(), configPath, func(store croncli.Store) error {
				job, err := croncli.ResolveJob(store, context.Background(), args[0])
				if err != nil {
					return err
				}

				if err := store.Delete(context.Background(), job.ID); err != nil {
					return fmt.Errorf("delete job: %w", err)
				}

				warnIfGatewayNotNotified(croncli.NotifyGateway())
				fmt.Printf("Deleted job %s (%s)\n", job.ID, job.Name)
				return nil
			})
		},
	}
	configFlag(cmd, &configPath)
	return cmd
}
