package croncli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cron"
)

func TestParseSchedule(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		kind    cron.ScheduleKind
		wantErr bool
	}{
		{"cron expression", "cron:*/5 * * * *", cron.ScheduleCron, false},
		{"every duration", "every:30m", cron.ScheduleEvery, false},
		{"every hours", "every:2h", cron.ScheduleEvery, false},
		{"at timestamp", "at:2026-01-01T00:00:00Z", cron.ScheduleAt, false},
		{"at with timezone", "at:2026-06-15T10:00:00+08:00", cron.ScheduleAt, false},
		{"missing colon", "invalid", "", true},
		{"empty kind", ":value", "", true},
		{"unknown kind", "daily:09:00", "", true},
		{"every too short", "every:30s", "", true},
		{"every invalid", "every:abc", "", true},
		{"at invalid format", "at:2026-13-40", "", true},
		{"empty value", "cron:", cron.ScheduleCron, false}, // validated later by ValidateJob
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched, err := ParseSchedule(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.kind, sched.Kind)
		})
	}
}

func TestParseSchedule_CronExpr(t *testing.T) {
	sched, err := ParseSchedule("cron:0 */2 * * *")
	require.NoError(t, err)
	require.Equal(t, cron.ScheduleCron, sched.Kind)
	require.Equal(t, "0 */2 * * *", sched.Expr)
}

func TestParseSchedule_EveryMs(t *testing.T) {
	sched, err := ParseSchedule("every:30m")
	require.NoError(t, err)
	require.Equal(t, cron.ScheduleEvery, sched.Kind)
	require.Equal(t, int64(30*60*1000), sched.EveryMs)
}

func TestPrepareJobForCreate(t *testing.T) {
	job, err := PrepareJobForCreate(
		"test-job", "every:5m", "say hello", "a test",
		"/tmp/work", "bot-1", "owner-1", 300, nil,
	)
	require.NoError(t, err)
	require.Equal(t, "test-job", job.Name)
	require.Equal(t, "say hello", job.Payload.Message)
	require.Equal(t, "bot-1", job.BotID)
	require.Equal(t, "owner-1", job.OwnerID)
	require.Equal(t, cron.ScheduleEvery, job.Schedule.Kind)
	require.Equal(t, int64(5*60*1000), job.Schedule.EveryMs)
	require.True(t, job.Enabled)
}

func TestPrepareJobForCreate_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		jobName string
		sched   string
		msg     string
		botID   string
		ownerID string
	}{
		{"missing name", "", "every:5m", "msg", "bot", "owner"},
		{"missing schedule", "job", "", "msg", "bot", "owner"},
		{"missing message", "job", "every:5m", "", "bot", "owner"},
		{"missing bot_id", "job", "every:5m", "msg", "", "owner"},
		{"missing owner_id", "job", "every:5m", "msg", "bot", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PrepareJobForCreate(tt.jobName, tt.sched, tt.msg, "", "", tt.botID, tt.ownerID, 0, nil)
			require.Error(t, err)
		})
	}
}

func TestFormatSchedule(t *testing.T) {
	require.Equal(t, "*/5 * * * *", FormatSchedule(cron.CronSchedule{Kind: cron.ScheduleCron, Expr: "*/5 * * * *"}))
	require.Equal(t, "every 30m0s", FormatSchedule(cron.CronSchedule{Kind: cron.ScheduleEvery, EveryMs: 30 * 60 * 1000}))
}

func TestFormatTimeMs(t *testing.T) {
	require.Equal(t, "-", FormatTimeMs(0))
	require.Equal(t, "-", FormatTimeMs(-1))
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC).UnixMilli()
	require.Contains(t, FormatTimeMs(ts), "2026")
}

func TestFormatDurationMs(t *testing.T) {
	require.Equal(t, "-", FormatDurationMs(0))
	require.Equal(t, "1m0s", FormatDurationMs(60_000))
}

func TestFormatCost(t *testing.T) {
	require.Equal(t, "-", FormatCost(0))
	require.Equal(t, "$0.0012", FormatCost(0.0012))
}
