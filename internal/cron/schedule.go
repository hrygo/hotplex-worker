package cron

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// NextRun computes the next fire time for a schedule relative to now.
func NextRun(s CronSchedule, now time.Time) (time.Time, error) {
	switch s.Kind {
	case ScheduleAt:
		return nextRunAt(s, now)
	case ScheduleEvery:
		return nextRunEvery(s, now)
	case ScheduleCron:
		return nextRunCron(s, now)
	default:
		return time.Time{}, fmt.Errorf("cron: unknown schedule kind: %s", s.Kind)
	}
}

func nextRunAt(s CronSchedule, now time.Time) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s.At)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron: parse at time: %w", err)
	}
	if !t.After(now) {
		return time.Time{}, nil // already past, no next run
	}
	return t, nil
}

func nextRunEvery(s CronSchedule, now time.Time) (time.Time, error) {
	if s.EveryMs <= 0 {
		return time.Time{}, fmt.Errorf("cron: every_ms must be positive")
	}
	return now.Add(time.Duration(s.EveryMs) * time.Millisecond), nil
}

func nextRunCron(s CronSchedule, now time.Time) (time.Time, error) {
	loc := time.Local
	if s.TZ != "" {
		var err error
		loc, err = time.LoadLocation(s.TZ)
		if err != nil {
			return time.Time{}, fmt.Errorf("cron: load tz %q: %w", s.TZ, err)
		}
	}

	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := p.Parse(s.Expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron: parse expr %q: %w", s.Expr, err)
	}
	return sched.Next(now.In(loc)), nil
}

// ValidateSchedule checks that the schedule is well-formed.
func ValidateSchedule(s CronSchedule) error {
	switch s.Kind {
	case ScheduleAt:
		if s.At == "" {
			return fmt.Errorf("cron: at schedule requires 'at' field")
		}
		if _, err := time.Parse(time.RFC3339, s.At); err != nil {
			return fmt.Errorf("cron: invalid at timestamp: %w", err)
		}
	case ScheduleEvery:
		if s.EveryMs <= 0 {
			return fmt.Errorf("cron: every schedule requires positive every_ms")
		}
		if s.EveryMs < 60_000 {
			return fmt.Errorf("cron: every_ms minimum is 60000 (1 minute), got %d", s.EveryMs)
		}
	case ScheduleCron:
		if s.Expr == "" {
			return fmt.Errorf("cron: cron schedule requires 'expr' field")
		}
		p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := p.Parse(s.Expr); err != nil {
			return fmt.Errorf("cron: invalid cron expression: %w", err)
		}
	default:
		return fmt.Errorf("cron: unknown schedule kind: %s", s.Kind)
	}
	return nil
}
