package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	croncli "github.com/hrygo/hotplex/internal/cli/cron"
	"github.com/hrygo/hotplex/internal/cron"
)

func newCronUpdateCmd() *cobra.Command {
	var (
		configPath   string
		schedule     string
		message      string
		description  string
		workDir      string
		botID        string
		ownerID      string
		timeoutSec   int
		allowedTools string
		enabled      *bool
	)
	cmd := &cobra.Command{
		Use:   "update <id|name>",
		Short: "Update a cron job",
		Long: `Update an existing cron job. Only specified flags are modified.

Schedule format:
  --schedule "cron:*/5 * * * *"
  --schedule "every:30m"
  --schedule "at:2026-01-01T00:00:00Z"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(context.Background(), configPath, func(store croncli.Store) error {
				job, err := croncli.ResolveJob(store, context.Background(), args[0])
				if err != nil {
					return err
				}

				if applyFlags(cmd, job, schedule, message, description, workDir, botID, ownerID, timeoutSec, allowedTools, enabled) {
					if err := cron.ValidateJob(job); err != nil {
						return err
					}

					job.UpdatedAtMs = time.Now().UnixMilli()

					if cmd.Flags().Changed("schedule") {
						next, err := cron.NextRun(job.Schedule, time.Now())
						if err != nil {
							return fmt.Errorf("compute next run: %w", err)
						}
						job.State.NextRunAtMs = next.UnixMilli()
					}

					if err := store.Update(context.Background(), job); err != nil {
						return fmt.Errorf("update job: %w", err)
					}

					warnIfGatewayNotNotified(croncli.NotifyGateway())
					fmt.Printf("Updated job %s (%s)\n", job.ID, job.Name)
				} else {
					fmt.Println("No changes specified.")
				}
				return nil
			})
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().StringVar(&schedule, "schedule", "", "schedule expression")
	cmd.Flags().StringVarP(&message, "message", "m", "", "prompt message")
	cmd.Flags().StringVar(&description, "description", "", "job description")
	cmd.Flags().StringVar(&workDir, "work-dir", "", "working directory")
	cmd.Flags().StringVar(&botID, "bot-id", "", "bot ID")
	cmd.Flags().StringVar(&ownerID, "owner-id", "", "owner ID")
	cmd.Flags().IntVar(&timeoutSec, "timeout", 0, "execution timeout in seconds")
	cmd.Flags().StringVar(&allowedTools, "allowed-tools", "", "comma-separated tool list")
	enabled = cmd.Flags().Bool("enabled", true, "enable or disable the job")
	return cmd
}

// applyFlags applies changed CLI flags to the job and returns true if any were changed.
func applyFlags(cmd *cobra.Command, job *cron.CronJob, schedule, message, description, workDir, botID, ownerID string, timeoutSec int, allowedTools string, enabled *bool) bool {
	changed := false

	if cmd.Flags().Changed("schedule") {
		if sched, err := croncli.ParseSchedule(schedule); err == nil {
			job.Schedule = sched
			changed = true
		}
	}
	if cmd.Flags().Changed("message") {
		job.Payload.Message = message
		changed = true
	}
	if cmd.Flags().Changed("description") {
		job.Description = description
		changed = true
	}
	if cmd.Flags().Changed("work-dir") {
		job.WorkDir = workDir
		changed = true
	}
	if cmd.Flags().Changed("bot-id") {
		job.BotID = botID
		changed = true
	}
	if cmd.Flags().Changed("owner-id") {
		job.OwnerID = ownerID
		changed = true
	}
	if cmd.Flags().Changed("timeout") {
		job.TimeoutSec = timeoutSec
		changed = true
	}
	if cmd.Flags().Changed("allowed-tools") {
		job.Payload.AllowedTools = strings.Split(allowedTools, ",")
		changed = true
	}
	if cmd.Flags().Changed("enabled") {
		job.Enabled = *enabled
		changed = true
	}

	return changed
}
