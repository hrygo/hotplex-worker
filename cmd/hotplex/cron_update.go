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
	var configPath string
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

				if applyFlags(cmd, job) {
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
	cmd.Flags().String("schedule", "", "schedule expression")
	cmd.Flags().StringP("message", "m", "", "prompt message")
	cmd.Flags().String("description", "", "job description")
	cmd.Flags().String("work-dir", "", "working directory")
	cmd.Flags().String("bot-id", "", "bot ID")
	cmd.Flags().String("owner-id", "", "owner ID")
	cmd.Flags().Int("timeout", 0, "execution timeout in seconds")
	cmd.Flags().String("allowed-tools", "", "comma-separated tool list")
	cmd.Flags().Bool("enabled", true, "enable or disable the job")
	cmd.Flags().Bool("delete-after-run", false, "delete one-shot job after execution")
	cmd.Flags().Bool("silent", false, "suppress result delivery (self-maintenance tasks)")
	cmd.Flags().Int("max-retries", 0, "max retries for failed one-shot jobs")
	cmd.Flags().Int("max-runs", 0, "max executions before auto-disable (required for every/cron)")
	cmd.Flags().String("expires-at", "", "auto-disable after this time RFC3339 (required for every/cron)")
	return cmd
}

// applyFlags applies changed CLI flags to the job and returns true if any were changed.
func applyFlags(cmd *cobra.Command, job *cron.CronJob) bool {
	changed := false

	if cmd.Flags().Changed("schedule") {
		raw, _ := cmd.Flags().GetString("schedule")
		if sched, err := croncli.ParseSchedule(raw); err == nil {
			job.Schedule = sched
			changed = true
		}
	}
	if cmd.Flags().Changed("message") {
		job.Payload.Message, _ = cmd.Flags().GetString("message")
		changed = true
	}
	if cmd.Flags().Changed("description") {
		job.Description, _ = cmd.Flags().GetString("description")
		changed = true
	}
	if cmd.Flags().Changed("work-dir") {
		job.WorkDir, _ = cmd.Flags().GetString("work-dir")
		changed = true
	}
	if cmd.Flags().Changed("bot-id") {
		job.BotID, _ = cmd.Flags().GetString("bot-id")
		changed = true
	}
	if cmd.Flags().Changed("owner-id") {
		job.OwnerID, _ = cmd.Flags().GetString("owner-id")
		changed = true
	}
	if cmd.Flags().Changed("timeout") {
		job.TimeoutSec, _ = cmd.Flags().GetInt("timeout")
		changed = true
	}
	if cmd.Flags().Changed("allowed-tools") {
		raw, _ := cmd.Flags().GetString("allowed-tools")
		job.Payload.AllowedTools = strings.Split(raw, ",")
		changed = true
	}
	if cmd.Flags().Changed("enabled") {
		job.Enabled, _ = cmd.Flags().GetBool("enabled")
		changed = true
	}
	if cmd.Flags().Changed("delete-after-run") {
		job.DeleteAfterRun, _ = cmd.Flags().GetBool("delete-after-run")
		changed = true
	}
	if cmd.Flags().Changed("silent") {
		job.Silent, _ = cmd.Flags().GetBool("silent")
		changed = true
	}
	if cmd.Flags().Changed("max-retries") {
		job.MaxRetries, _ = cmd.Flags().GetInt("max-retries")
		changed = true
	}
	if cmd.Flags().Changed("max-runs") {
		job.MaxRuns, _ = cmd.Flags().GetInt("max-runs")
		changed = true
	}
	if cmd.Flags().Changed("expires-at") {
		job.ExpiresAt, _ = cmd.Flags().GetString("expires-at")
		changed = true
	}

	return changed
}
