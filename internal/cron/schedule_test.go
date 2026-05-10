package cron

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNextRun_At(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	tests := []struct {
		name     string
		sched    CronSchedule
		now      time.Time
		wantErr  bool
		wantZero bool
	}{
		{
			name:  "future timestamp",
			sched: CronSchedule{Kind: ScheduleAt, At: future},
			now:   time.Now(),
		},
		{
			name:     "past timestamp returns zero",
			sched:    CronSchedule{Kind: ScheduleAt, At: past},
			now:      time.Now(),
			wantZero: true,
		},
		{
			name:    "invalid timestamp",
			sched:   CronSchedule{Kind: ScheduleAt, At: "not-a-time"},
			now:     time.Now(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NextRun(tt.sched, tt.now)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantZero {
				require.True(t, got.IsZero())
			} else {
				require.False(t, got.IsZero())
				require.True(t, got.After(tt.now))
			}
		})
	}
}

func TestNextRun_Every(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name    string
		sched   CronSchedule
		wantErr bool
	}{
		{
			name:  "1 hour interval",
			sched: CronSchedule{Kind: ScheduleEvery, EveryMs: 3600_000},
		},
		{
			name:  "5 minute interval",
			sched: CronSchedule{Kind: ScheduleEvery, EveryMs: 300_000},
		},
		{
			name:    "zero interval",
			sched:   CronSchedule{Kind: ScheduleEvery, EveryMs: 0},
			wantErr: true,
		},
		{
			name:    "negative interval",
			sched:   CronSchedule{Kind: ScheduleEvery, EveryMs: -1000},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NextRun(tt.sched, now)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			expected := now.Add(time.Duration(tt.sched.EveryMs) * time.Millisecond)
			require.WithinDuration(t, expected, got, time.Second)
		})
	}
}

func TestNextRun_Cron(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 10, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		sched   CronSchedule
		now     time.Time
		wantErr bool
	}{
		{
			name:  "every minute expression",
			sched: CronSchedule{Kind: ScheduleCron, Expr: "* * * * *"},
			now:   now,
		},
		{
			name:  "hourly at :30",
			sched: CronSchedule{Kind: ScheduleCron, Expr: "30 * * * *"},
			now:   now,
		},
		{
			name:  "daily at 9am UTC",
			sched: CronSchedule{Kind: ScheduleCron, Expr: "0 9 * * *"},
			now:   now,
		},
		{
			name:  "with timezone",
			sched: CronSchedule{Kind: ScheduleCron, Expr: "0 9 * * *", TZ: "Asia/Shanghai"},
			now:   now,
		},
		{
			name:    "invalid cron expression",
			sched:   CronSchedule{Kind: ScheduleCron, Expr: "invalid"},
			now:     now,
			wantErr: true,
		},
		{
			name:    "invalid timezone",
			sched:   CronSchedule{Kind: ScheduleCron, Expr: "* * * * *", TZ: "Invalid/Zone"},
			now:     now,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NextRun(tt.sched, tt.now)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.False(t, got.IsZero())
			require.True(t, got.After(tt.now) || got.Equal(tt.now))
		})
	}
}

func TestNextRun_UnknownKind(t *testing.T) {
	t.Parallel()
	_, err := NextRun(CronSchedule{Kind: "unknown"}, time.Now())
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown schedule kind")
}

func TestValidateSchedule(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	tests := []struct {
		name    string
		sched   CronSchedule
		wantErr bool
	}{
		{"valid at", CronSchedule{Kind: ScheduleAt, At: future}, false},
		{"at missing timestamp", CronSchedule{Kind: ScheduleAt, At: ""}, true},
		{"at invalid timestamp", CronSchedule{Kind: ScheduleAt, At: "bad"}, true},
		{"valid every", CronSchedule{Kind: ScheduleEvery, EveryMs: 60_000}, false},
		{"every zero interval", CronSchedule{Kind: ScheduleEvery, EveryMs: 0}, true},
		{"every negative interval", CronSchedule{Kind: ScheduleEvery, EveryMs: -1}, true},
		{"every sub-minute interval", CronSchedule{Kind: ScheduleEvery, EveryMs: 30_000}, true},
		{"valid cron", CronSchedule{Kind: ScheduleCron, Expr: "*/5 * * * *"}, false},
		{"cron missing expr", CronSchedule{Kind: ScheduleCron, Expr: ""}, true},
		{"cron invalid expr", CronSchedule{Kind: ScheduleCron, Expr: "bad"}, true},
		{"unknown kind", CronSchedule{Kind: "unknown"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSchedule(tt.sched)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNextRun_CronTimezone(t *testing.T) {
	t.Parallel()

	// 2026-05-10 08:00 UTC = 2026-05-10 16:00 Shanghai
	now := time.Date(2026, 5, 10, 8, 0, 0, 0, time.UTC)

	// "0 9 * * *" in Shanghai = 01:00 UTC next day
	sched := CronSchedule{Kind: ScheduleCron, Expr: "0 9 * * *", TZ: "Asia/Shanghai"}
	got, err := NextRun(sched, now)
	require.NoError(t, err)

	loc, _ := time.LoadLocation("Asia/Shanghai")
	inLoc := got.In(loc)
	require.Equal(t, 9, inLoc.Hour())
	require.Equal(t, 0, inLoc.Minute())
}
