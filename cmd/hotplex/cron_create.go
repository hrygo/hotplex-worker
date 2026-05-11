package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	croncli "github.com/hrygo/hotplex/internal/cli/cron"
)

func newCronCreateCmd() *cobra.Command {
	var (
		configPath     string
		name           string
		schedule       string
		message        string
		description    string
		workDir        string
		botID          string
		ownerID        string
		timeoutSec     int
		allowedTools   string
		deleteAfterRun bool
		silent         bool
		maxRetries     int
		maxRuns        int
		expiresAt      string
		platform       string
		platformKey    string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a cron job",
		Long: `Create a new cron job.

Required flags: --name, --schedule, --message, --bot-id, --owner-id

For recurring jobs (every/cron), --max-runs and --expires-at are also required.
One-shot jobs (at) do not require lifecycle constraints.

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

				var platformKeyMap map[string]string
				if platformKey != "" {
					if err := json.Unmarshal([]byte(platformKey), &platformKeyMap); err != nil {
						return fmt.Errorf("invalid --platform-key: expected JSON object, got %q", platformKey)
					}
				}

				job, err := croncli.PrepareJobForCreate(name, schedule, message, description, workDir, botID, ownerID, timeoutSec, tools, croncli.JobCreateOptions{
					DeleteAfterRun: deleteAfterRun,
					Silent:         silent,
					MaxRetries:     maxRetries,
					MaxRuns:        maxRuns,
					ExpiresAt:      expiresAt,
					Platform:       platform,
					PlatformKey:    platformKeyMap,
				})
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
	cmd.Flags().BoolVar(&deleteAfterRun, "delete-after-run", false, "delete one-shot job after execution")
	cmd.Flags().BoolVar(&silent, "silent", false, "suppress result delivery (self-maintenance tasks)")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 0, "max retries for failed one-shot jobs")
	cmd.Flags().IntVar(&maxRuns, "max-runs", 0, "max executions before auto-disable (required for every/cron)")
	cmd.Flags().StringVar(&expiresAt, "expires-at", "", "auto-disable after this time RFC3339 (required for every/cron)")
	cmd.Flags().StringVar(&platform, "platform", "", "target delivery platform (slack|feishu|cron), auto-detected from env if unset")
	cmd.Flags().StringVar(&platformKey, "platform-key", "", "platform routing key as JSON, e.g. '{\"channel_id\":\"C123\"}'")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("schedule")
	_ = cmd.MarkFlagRequired("message")
	_ = cmd.MarkFlagRequired("bot-id")
	_ = cmd.MarkFlagRequired("owner-id")
	return cmd
}
