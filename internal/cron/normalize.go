package cron

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var threatPatterns = []string{
	"ignore previous instructions",
	"system prompt override",
	"you are now",
	"ignore all above",
	"forget your instructions",
	"disregard your training",
}

// ValidateJobPrompt scans for obvious prompt injection patterns.
func ValidateJobPrompt(prompt string) error {
	if prompt == "" {
		return errors.New("cron: prompt must not be empty")
	}
	if len(prompt) > 4096 {
		return fmt.Errorf("cron: prompt exceeds 4KB limit (%d bytes)", len(prompt))
	}
	lower := strings.ToLower(prompt)
	for _, pat := range threatPatterns {
		if strings.Contains(lower, pat) {
			return fmt.Errorf("cron: potential prompt injection detected")
		}
	}
	return nil
}

// ValidateJob performs full validation on a CronJob before creation/update.
func ValidateJob(job *CronJob) error {
	if job.Name == "" {
		return errors.New("cron: name is required")
	}
	if job.OwnerID == "" {
		return errors.New("cron: owner_id is required")
	}
	if job.BotID == "" {
		return errors.New("cron: bot_id is required")
	}
	if err := ValidateSchedule(job.Schedule); err != nil {
		return err
	}
	if err := ValidateJobPrompt(job.Payload.Message); err != nil {
		return err
	}
	// Attached session validation.
	if job.Payload.Kind == PayloadAttachedSession {
		if job.Payload.TargetSessionID == "" {
			return errors.New("cron: target_session_id is required for attached_session")
		}
		if job.Schedule.Kind == ScheduleCron {
			return errors.New("cron: attached_session does not support cron expression schedules")
		}
	}
	// Recurring jobs must have lifecycle constraints to prevent infinite execution.
	if job.Schedule.Kind != ScheduleAt {
		if job.MaxRuns <= 0 {
			return errors.New("cron: max_runs is required for recurring jobs (every/cron)")
		}
		if job.ExpiresAt == "" {
			return errors.New("cron: expires_at is required for recurring jobs (every/cron)")
		}
		if _, err := time.Parse(time.RFC3339, job.ExpiresAt); err != nil {
			return fmt.Errorf("cron: invalid expires_at: %w", err)
		}
	}
	return nil
}
