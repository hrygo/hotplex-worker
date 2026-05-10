package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	croncli "github.com/hrygo/hotplex/internal/cli/cron"
)

func newCronCreateCmd() *cobra.Command {
	var (
		configPath   string
		name         string
		schedule     string
		message      string
		description  string
		workDir      string
		botID        string
		ownerID      string
		timeoutSec   int
		allowedTools string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a cron job",
		Long: `Create a new cron job.

Required flags: --name, --schedule, --message, --bot-id, --owner-id

Schedule format:
  --schedule "cron:*/5 * * * *"
  --schedule "every:30m"
  --schedule "at:2026-01-01T00:00:00Z"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(context.Background(), configPath, func(store croncli.Store) error {
				var tools []string
				if allowedTools != "" {
					tools = strings.Split(allowedTools, ",")
				}

				job, err := croncli.PrepareJobForCreate(name, schedule, message, description, workDir, botID, ownerID, timeoutSec, tools)
				if err != nil {
					return err
				}

				if err := store.Create(context.Background(), job); err != nil {
					return fmt.Errorf("create job: %w", err)
				}

				warnIfGatewayNotNotified(croncli.NotifyGateway())

				fmt.Printf("Created job %s (%s)\n", job.ID, job.Name)
				fmt.Printf("  Schedule: %s\n", croncli.FormatSchedule(job.Schedule))
				fmt.Printf("  Next run: %s\n", croncli.FormatTimeMs(job.State.NextRunAtMs))
				return nil
			})
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().StringVar(&name, "name", "", "job name (required)")
	cmd.Flags().StringVar(&schedule, "schedule", "", "schedule expression (required)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "prompt message (required)")
	cmd.Flags().StringVar(&description, "description", "", "job description")
	cmd.Flags().StringVar(&workDir, "work-dir", "", "working directory")
	cmd.Flags().StringVar(&botID, "bot-id", "", "bot ID (required)")
	cmd.Flags().StringVar(&ownerID, "owner-id", "", "owner ID (required)")
	cmd.Flags().IntVar(&timeoutSec, "timeout", 0, "execution timeout in seconds")
	cmd.Flags().StringVar(&allowedTools, "allowed-tools", "", "comma-separated tool list")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("schedule")
	_ = cmd.MarkFlagRequired("message")
	_ = cmd.MarkFlagRequired("bot-id")
	_ = cmd.MarkFlagRequired("owner-id")
	return cmd
}
