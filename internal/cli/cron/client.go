package croncli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/cron"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
)

// Re-export types for callers that only import this package.
type (
	Store   = cron.Store
	CronJob = cron.CronJob
)

const defaultHotplexConfig = "~/.hotplex/config.yaml"

// gatewayState mirrors cmd/hotplex/pid.go gatewayState to avoid circular imports.
type gatewayState struct {
	PID        int    `json:"pid"`
	ConfigPath string `json:"config,omitempty"`
	DevMode    bool   `json:"dev,omitempty"`
}

// OpenStore opens the config, initializes the DB, and returns cron store + eventstore + cleanup.
func OpenStore(ctx context.Context, configPath string) (cron.Store, *eventstore.SQLiteStore, func(), error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, nil, nil, err
	}

	ss, err := session.NewSQLiteStore(ctx, cfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open session store: %w", err)
	}

	cronStore := cron.NewSQLiteStore(ss.DB(), slog.Default())
	evStore := eventstore.NewSQLiteStore(ss.DB())
	cleanup := func() { _ = ss.Close() }

	return cronStore, evStore, cleanup, nil
}

// ResolveJob resolves a job by ID or name.
func ResolveJob(store cron.Store, ctx context.Context, idOrName string) (*cron.CronJob, error) {
	job, err := store.Get(ctx, idOrName)
	if err == nil {
		return job, nil
	}
	job, err = store.GetByName(ctx, idOrName)
	if err != nil {
		return nil, fmt.Errorf("job not found: %s", idOrName)
	}
	return job, nil
}

// ParseSchedule parses the CLI --schedule flag format: "cron:expr" | "every:duration" | "at:timestamp".
func ParseSchedule(raw string) (cron.CronSchedule, error) {
	idx := strings.Index(raw, ":")
	if idx <= 0 {
		return cron.CronSchedule{}, fmt.Errorf("invalid schedule format, expected kind:value (e.g. cron:*/5 * * * *, every:30m, at:2026-01-01T00:00:00Z)")
	}
	kind := cron.ScheduleKind(raw[:idx])
	value := raw[idx+1:]

	switch kind {
	case cron.ScheduleCron:
		return cron.CronSchedule{Kind: kind, Expr: value}, nil
	case cron.ScheduleAt:
		if strings.HasPrefix(value, "+") {
			d, err := time.ParseDuration(value[1:])
			if err != nil {
				return cron.CronSchedule{}, fmt.Errorf("invalid relative duration in at schedule: %w", err)
			}
			if d < time.Minute {
				return cron.CronSchedule{}, fmt.Errorf("relative duration must be at least 1 minute")
			}
			if d > 72*time.Hour {
				return cron.CronSchedule{}, fmt.Errorf("relative duration must not exceed 72 hours")
			}
			abs := time.Now().Add(d).Format(time.RFC3339)
			return cron.CronSchedule{Kind: kind, At: abs}, nil
		}
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return cron.CronSchedule{}, fmt.Errorf("invalid at timestamp: %w", err)
		}
		return cron.CronSchedule{Kind: kind, At: value}, nil
	case cron.ScheduleEvery:
		ms, err := parseDurationMs(value)
		if err != nil {
			return cron.CronSchedule{}, err
		}
		return cron.CronSchedule{Kind: kind, EveryMs: ms}, nil
	default:
		return cron.CronSchedule{}, fmt.Errorf("unknown schedule kind: %s (use cron/every/at)", kind)
	}
}

// parseDurationMs parses human-friendly durations: "30m", "1h", "2h30m", "90s".
func parseDurationMs(s string) (int64, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	ms := d.Milliseconds()
	if ms < 60_000 {
		return 0, fmt.Errorf("minimum interval is 1 minute, got %s", s)
	}
	return ms, nil
}

// JobCreateOptions groups lifecycle and optional parameters for job creation.
type JobCreateOptions struct {
	DeleteAfterRun  bool
	Silent          bool
	MaxRetries      int
	MaxRuns         int
	ExpiresAt       string
	Platform        string
	PlatformKey     map[string]string
	WorkerType      string
	Attach          bool   // --attach: attached_session mode
	TargetSessionID string // auto-filled from $GATEWAY_SESSION_ID
}

// resolvePlatform resolves the target platform and routing key with three-level priority:
// 1. CLI flags (explicit --platform / --platform-key)
// 2. Environment variables (GATEWAY_PLATFORM, GATEWAY_CHANNEL_ID, GATEWAY_THREAD_ID)
// 3. Default "cron" (backward compatible)
func resolvePlatform(cliPlatform string, cliPlatformKey map[string]string) (string, map[string]string) {
	if cliPlatform != "" {
		return cliPlatform, cliPlatformKey
	}

	envPlatform := os.Getenv("GATEWAY_PLATFORM")
	switch envPlatform {
	case "slack":
		key := map[string]string{}
		if ch := os.Getenv("GATEWAY_CHANNEL_ID"); ch != "" {
			key["channel_id"] = ch
		}
		if ts := os.Getenv("GATEWAY_THREAD_ID"); ts != "" {
			key["thread_ts"] = ts
		}
		return "slack", key
	case "feishu":
		key := map[string]string{}
		if chatID := os.Getenv("GATEWAY_CHANNEL_ID"); chatID != "" {
			key["chat_id"] = chatID
		}
		return "feishu", key
	}

	return "cron", nil
}

// PrepareJobForCreate builds a CronJob from CLI flags.
func PrepareJobForCreate(name, scheduleRaw, message, description, workDir, botID, ownerID string, timeoutSec int, allowedTools []string, opts JobCreateOptions) (*cron.CronJob, error) {
	sched, err := ParseSchedule(scheduleRaw)
	if err != nil {
		return nil, err
	}

	platform, platformKey := resolvePlatform(opts.Platform, opts.PlatformKey)

	payloadKind := cron.PayloadIsolatedSession
	if opts.Attach {
		payloadKind = cron.PayloadAttachedSession
	}

	job := &cron.CronJob{
		Name:           name,
		Description:    description,
		Enabled:        true,
		Schedule:       sched,
		Payload:        cron.CronPayload{Kind: payloadKind, Message: message, TargetSessionID: opts.TargetSessionID, AllowedTools: allowedTools, WorkerType: opts.WorkerType},
		WorkDir:        workDir,
		BotID:          botID,
		OwnerID:        ownerID,
		Platform:       platform,
		PlatformKey:    platformKey,
		TimeoutSec:     timeoutSec,
		DeleteAfterRun: opts.DeleteAfterRun,
		Silent:         opts.Silent,
		MaxRetries:     opts.MaxRetries,
		MaxRuns:        opts.MaxRuns,
		ExpiresAt:      opts.ExpiresAt,
	}

	// Default lifecycle constraints for recurring jobs.
	if sched.Kind != cron.ScheduleAt {
		if job.MaxRuns <= 0 {
			job.MaxRuns = 10
		}
		if job.ExpiresAt == "" {
			job.ExpiresAt = time.Now().Add(24 * time.Hour).Format(time.RFC3339)
		}
	}

	if err := cron.ValidateJob(job); err != nil {
		return nil, err
	}

	job.ID = cron.GenerateJobID()

	next, err := cron.NextRun(sched, time.Now())
	if err != nil {
		return nil, fmt.Errorf("cron: compute initial next run: %w", err)
	}
	job.State.NextRunAtMs = next.UnixMilli()

	return job, nil
}

// TriggerViaAdmin calls the gateway admin API to trigger a job run.
func TriggerViaAdmin(ctx context.Context, configPath, jobID string) error {
	// If the user didn't specify --config, try reading the gateway's actual
	// config path from the PID file to avoid loading a different .env.
	if configPath == "" || configPath == defaultHotplexConfig {
		if gwCfg := gatewayConfigPath(); gwCfg != "" {
			configPath = gwCfg
		}
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	addr := cfg.Admin.Addr
	if addr == "" {
		addr = "localhost:9999"
	}
	token := firstAdminToken(cfg)
	if token == "" {
		return fmt.Errorf("no admin token configured")
	}

	url := "http://" + addr + "/api/cron/jobs/" + url.PathEscape(jobID) + "/run"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("trigger request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusAccepted:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("job not found: %s", jobID)
	case http.StatusServiceUnavailable:
		return fmt.Errorf("cron scheduler not running")
	default:
		return fmt.Errorf("trigger failed: HTTP %d", resp.StatusCode)
	}
}

// QueryHistory queries the eventstore for a cron job's execution history.
func QueryHistory(ctx context.Context, store cron.Store, evStore *eventstore.SQLiteStore, idOrName string) (*eventstore.TurnStats, error) {
	job, err := ResolveJob(store, ctx, idOrName)
	if err != nil {
		return nil, err
	}

	sessionKey := session.DerivePlatformSessionKey(
		job.OwnerID, worker.TypeClaudeCode,
		session.PlatformContext{
			Platform: "cron",
			BotID:    job.BotID,
			UserID:   job.OwnerID,
			WorkDir:  job.WorkDir,
			ChatID:   job.ID,
		},
	)

	return evStore.QueryTurnStats(ctx, sessionKey)
}

// NotifyGateway sends SIGHUP to the running gateway to reload the cron index.
func NotifyGateway() error {
	pidPath := filepath.Join(config.HotplexHome(), ".pids", "gateway.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return nil // gateway not running, nothing to notify
	}

	var state gatewayState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse gateway PID file: %w", err)
	}
	if state.PID <= 0 {
		return nil
	}

	return sendReloadSignal(state.PID)
}

// gatewayConfigPath reads the gateway PID file and returns the config path
// the running gateway is using. Returns "" if the gateway is not running or
// the PID file doesn't exist.
func gatewayConfigPath() string {
	pidPath := filepath.Join(config.HotplexHome(), ".pids", "gateway.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return ""
	}
	var state gatewayState
	if err := json.Unmarshal(data, &state); err != nil {
		return ""
	}
	return state.ConfigPath
}

func loadConfig(configPath string) (*config.Config, error) {
	configPath, err := config.ExpandAndAbs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	loadEnvFile(filepath.Dir(configPath))

	cfg, err := config.Load(configPath, config.LoadOptions{})
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func loadEnvFile(dir string) {
	envPath := filepath.Join(dir, ".env")
	f, err := os.Open(envPath)
	if err != nil {
		return
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
		if os.Getenv(key) == "" && !security.IsProtected(key) {
			os.Setenv(key, val)
		}
	}
}

// firstAdminToken returns the first admin token from config (deterministic order).
func firstAdminToken(cfg *config.Config) string {
	if len(cfg.Admin.TokenScopes) > 0 {
		return slices.Sorted(maps.Keys(cfg.Admin.TokenScopes))[0]
	}
	if len(cfg.Admin.Tokens) > 0 {
		return cfg.Admin.Tokens[0]
	}
	return ""
}

// FormatSchedule returns a human-readable schedule description.
func FormatSchedule(s cron.CronSchedule) string {
	switch s.Kind {
	case cron.ScheduleCron:
		return s.Expr
	case cron.ScheduleEvery:
		return "every " + time.Duration(s.EveryMs*int64(time.Millisecond)).String()
	case cron.ScheduleAt:
		t, _ := time.Parse(time.RFC3339, s.At)
		if !t.IsZero() {
			return "at " + t.Format("2006-01-02 15:04:05")
		}
		return "at " + s.At
	default:
		return string(s.Kind)
	}
}

// FormatTimeMs formats a unix millisecond timestamp.
func FormatTimeMs(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04:05")
}

// FormatDurationMs formats milliseconds as a human-readable duration.
func FormatDurationMs(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return time.Duration(ms * int64(time.Millisecond)).Round(time.Second).String()
}

// FormatCost formats a USD cost value.
func FormatCost(usd float64) string {
	if usd <= 0 {
		return "-"
	}
	return "$" + strconv.FormatFloat(usd, 'f', 4, 64)
}
