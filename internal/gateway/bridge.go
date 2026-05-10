package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// resetGenerationer is an optional interface for workers that support
// reset-aware crash handling via a monotonic generation counter.
type resetGenerationer interface {
	IncResetGeneration() int64
	LoadResetGeneration() int64
}

// bridgeSM is the narrow subset of SessionManager that Bridge needs.
type bridgeSM interface {
	// SessionReader
	Get(ctx context.Context, id string) (*session.SessionInfo, error)
	GetWorker(id string) worker.Worker
	// SessionWorkerManager
	AttachWorker(id string, w worker.Worker) error
	DetachWorker(id string)
	DetachWorkerIf(id string, expected worker.Worker) bool
	UpdateWorkerSessionID(ctx context.Context, id, workerSessionID string) error
	// Lifecycle + transitions
	CreateWithBot(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, platform string, platformKey map[string]string, workDir, title string) (*session.SessionInfo, error)
	Delete(ctx context.Context, id string) error
	Transition(ctx context.Context, id string, to events.SessionState) error
	ResetExpiry(ctx context.Context, id string) error
}

// Bridge connects the gateway to the session manager.
// It runs the read pump in a goroutine and proxies worker events to the hub.
type Bridge struct {
	log       *slog.Logger
	hub       *Hub
	sm        bridgeSM
	collector *eventstore.Collector // optional; nil means event storage disabled
	wf        WorkerFactory
	retryCtrl *LLMRetryController

	fwdWg         sync.WaitGroup // tracks active forwardEvents goroutines
	closed        atomic.Bool    // set during shutdown to skip crash detection
	retryCancelMu sync.Mutex
	retryCancel   map[string]chan struct{} // sessionID → cancel channel

	agentConfigDir     string        // agent config directory path; "" = disabled
	turnTimeout        time.Duration // per-turn timeout; 0 = disabled
	workerEnv          []string      // extra env vars from worker.environment config
	workerEnvBlocklist []string      // extra blocklist entries from worker.env_blocklist config
	cronEnv            []string      // env vars injected only into cron platform sessions
	mcpConfigJSON      atomic.Value  // pre-serialized MCP config JSON string; "" = not configured

	accum   map[string]*sessionAccumulator // per-session stats accumulator
	accumMu sync.Mutex

	crashTracker   map[string]*crashHistory // per-session crash loop detection
	crashTrackerMu sync.Mutex
}

type crashHistory struct {
	count     int
	firstSeen time.Time
}

const (
	crashLoopMax    = 3               // max consecutive crashes before abort
	crashLoopWindow = 5 * time.Minute // window for counting consecutive crashes
)

// NewBridge creates a new bridge.
func NewBridge(deps BridgeDeps) *Bridge {
	b := &Bridge{
		log:                deps.Log.With("component", "bridge"),
		hub:                deps.Hub,
		sm:                 deps.SM,
		wf:                 defaultWorkerFactory{},
		collector:          deps.EventCollector,
		retryCtrl:          deps.RetryCtrl,
		agentConfigDir:     deps.AgentConfigDir,
		turnTimeout:        deps.TurnTimeout,
		workerEnv:          deps.WorkerEnv,
		workerEnvBlocklist: deps.WorkerEnvBlocklist,
		cronEnv:            deps.CronEnv,
		retryCancel:        make(map[string]chan struct{}),
		accum:              make(map[string]*sessionAccumulator),
		crashTracker:       make(map[string]*crashHistory),
	}
	b.mcpConfigJSON.Store(deps.MCPConfigJSON)
	return b
}

// SetWorkerFactory replaces the default worker factory. Used by tests to inject
// simulated workers without requiring external CLI binaries.
func (b *Bridge) SetWorkerFactory(wf WorkerFactory) {
	b.wf = wf
}

// UpdateMCPConfig atomically updates the MCP config JSON. Used by config hot-reload.
func (b *Bridge) UpdateMCPConfig(json string) {
	b.mcpConfigJSON.Store(json)
}

// StartSession creates a new session and starts a worker.
func (b *Bridge) StartSession(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, workDir, platform string, platformKey map[string]string, title string) error {
	if b.closed.Load() {
		return fmt.Errorf("bridge: rejecting new session during shutdown")
	}

	// Create session in DB with bot_id and allowed_tools.
	si, err := b.sm.CreateWithBot(ctx, id, userID, botID, wt, allowedTools, platform, platformKey, workDir, title)
	if err != nil {
		return fmt.Errorf("bridge: create session: %w", err)
	}

	workerInfo := b.buildWorkerInfo(id, userID, workDir, si)
	injectSlackEnv(&workerInfo, platformKey)
	workerInfo.Env = injectGatewayContext(workerInfo.Env, platform, botID, userID, platformKey, id, workDir)

	// Inject cron-specific env vars (e.g. admin API creds) only for cron sessions.
	if platform == "cron" && len(b.cronEnv) > 0 {
		for _, kv := range b.cronEnv {
			if i := strings.IndexByte(kv, '='); i >= 0 {
				workerInfo.Env[kv[:i]] = kv[i+1:]
			}
		}
	}

	if _, err := b.createAndLaunchWorker(workerLaunchParams{
		ctx:         ctx,
		wt:          wt,
		workerInfo:  workerInfo,
		platform:    platform,
		botID:       botID,
		forwardOpts: &forwardOpts{workDir: workDir},
	},
		func(ctx context.Context, w worker.Worker, info worker.SessionInfo) error {
			if err := w.Start(ctx, info); err != nil {
				_ = b.sm.Delete(ctx, id)
				return fmt.Errorf("bridge: start worker: %w", err)
			}
			return nil
		},
		func(_ worker.Worker, _ error) {
			_ = b.sm.Delete(ctx, id)
		},
	); err != nil {
		return err
	}

	// Transition to RUNNING. (StateNotifier will emit state event automatically)
	if err := b.sm.Transition(ctx, id, events.StateRunning); err != nil {
		b.log.Warn("bridge: transition to running failed", "session_id", id, "worker_type", wt, "err", err)
	}

	return nil
}

// ResumeSession reattaches to an existing session.
// workDir overrides the stored project directory (used by platform sessions that need a consistent workspace).
func (b *Bridge) ResumeSession(ctx context.Context, id, workDir string) error {
	return b.resumeWithOpts(ctx, id, workDir, forwardOpts{resumed: true, workDir: workDir})
}

// resumeWithOpts is the internal implementation of ResumeSession that accepts
// forwardOpts for controlling retry behavior.
func (b *Bridge) resumeWithOpts(ctx context.Context, id, workDir string, opts forwardOpts) error {
	if b.closed.Load() {
		return fmt.Errorf("bridge: rejecting resume during shutdown")
	}

	si, err := b.sm.Get(ctx, id)
	if err != nil {
		return err
	}

	if si.State == events.StateDeleted {
		return session.ErrSessionNotFound
	}

	// Capture pending input before terminating so it can be re-delivered to the new worker.
	// This prevents input loss when ResumeSession is called concurrently (e.g., a
	// second user message arrives while attemptResumeFallback is starting a fresh worker).
	var pendingInput string
	if existing := b.sm.GetWorker(id); existing != nil {
		if ir, ok := existing.(worker.InputRecoverer); ok {
			pendingInput = ir.LastInput()
		}
		_ = existing.Terminate(context.Background())
		b.sm.DetachWorker(id)
	}

	workerInfo := b.buildWorkerInfo(si.ID, si.UserID, workDir, si)
	injectSlackEnv(&workerInfo, si.PlatformKey)
	workerInfo.Env = injectGatewayContext(workerInfo.Env, si.Platform, si.BotID, si.UserID, si.PlatformKey, id, workDir)
	w, err := b.createAndLaunchWorker(workerLaunchParams{
		ctx:         ctx,
		wt:          si.WorkerType,
		workerInfo:  workerInfo,
		platform:    si.Platform,
		botID:       si.BotID,
		forwardOpts: &opts,
	},
		func(ctx context.Context, w worker.Worker, info worker.SessionInfo) error {
			// Transition IDLE/RESUMED/TERMINATED sessions to RUNNING.
			if si.State != events.StateRunning {
				if err := b.sm.Transition(ctx, id, events.StateRunning); err != nil {
					return err
				}
			}
			// Zombie GC may delete session files; fall back to fresh start if missing.
			if fc, ok := w.(worker.SessionFileChecker); ok && !fc.HasSessionFiles(info.SessionID) {
				b.log.Info("bridge: session files missing, falling back to fresh start",
					"session_id", id)
				if err := w.Start(ctx, info); err != nil {
					return fmt.Errorf("bridge: fresh start after missing files: %w", err)
				}
				opts.resumed = false
				return nil
			}
			if err := w.Resume(ctx, info); err != nil {
				return fmt.Errorf("bridge: resume start: %w", err)
			}
			return nil
		},
		nil, // no extra cleanup on attach failure for resume
	)
	if err != nil {
		return err
	}

	// Refresh ExpiresAt so a reactivated session isn't immediately killed by GC max_lifetime.
	if err := b.sm.ResetExpiry(ctx, id); err != nil {
		b.log.Warn("bridge: resume reset expiry failed", "session_id", id, "err", err)
	}

	// Re-deliver pending input that was captured before the old worker was terminated.
	// This covers the case where a concurrent message triggered ResumeSession while
	// attemptResumeFallback was starting a fresh worker — the fresh worker's buffered
	// input would otherwise be lost when the old worker is terminated here.
	if pendingInput != "" {
		b.log.Info("bridge: re-delivering pending input to resumed worker",
			"session_id", id, "content_len", len(pendingInput))
		if err := w.Input(ctx, pendingInput, nil); err != nil {
			b.log.Warn("bridge: pending input re-delivery failed",
				"session_id", id, "err", err)
		}
	}

	// Notify client of current state.
	stateToNotify := si.State
	if stateToNotify == events.StateTerminated || stateToNotify == events.StateIdle {
		stateToNotify = events.StateRunning // We just transitioned it
	}
	stateEvt := events.NewEnvelope(aep.NewID(), id, b.hub.NextSeq(id), events.State, events.StateData{
		State: stateToNotify,
	})
	if err := b.hub.SendToSession(ctx, stateEvt); err != nil {
		b.log.Warn("bridge: resume state notify failed", "session_id", id, "err", err)
	}

	return nil
}

// copyEnvelope delegates to events.Clone, which performs a deep copy of
// map[string]any Event.Data to eliminate shared map headers.
// This prevents data races when Hub.Run encodes the clone concurrently with
// Bridge.forwardEvents encoding the original (e.g., for msgStore.Append).
var _ = events.Clone // compile-time check that Clone is accessible

// StartPlatformSession creates a session for a platform message if it doesn't already exist.
// Implements messaging.SessionStarter. Idempotent: returns nil if session exists with a live worker.
//
// Decision logic (state-based with Resume→Start fallback):
//  1. No DB record → Create + Start (--session-id)
//  2. Worker alive → Reuse (forward message)
//  3. No worker, state=CREATED → Start (--session-id)
//  4. No worker, state=RUNNING/IDLE/TERMINATED → Resume (--resume)
//     If Resume fails (files gone/corrupted), fall back to Start (--session-id)
func (b *Bridge) StartPlatformSession(ctx context.Context, sessionID, ownerID, workerType, workDir, platform string, platformKey map[string]string, botID string) error {
	b.log.Debug("bridge: StartPlatformSession called", "session_id", sessionID, "owner_id", ownerID, "worker_type", workerType, "work_dir", workDir, "platform", platform, "platform_key", platformKey, "bot_id", botID)
	si, err := b.sm.Get(ctx, sessionID)
	if err == nil {
		if w := b.sm.GetWorker(sessionID); w != nil {
			// Only reuse if session is still active. TERMINATED sessions with a stale
			// worker pointer must fall through to ResumeSession to ensure the message
			// is delivered, not silently dropped (bug: worker pointer non-nil after
			// transitionState nils it, but only after SIGTERM completes asynchronously).
			if si.State.IsActive() {
				return nil
			}
		}
		// Orphan: session record exists but worker is gone.
		if si.State == events.StateCreated {
			b.log.Info("bridge: orphan platform session unstarted, starting fresh", "session_id", sessionID)
			return b.startOrResumeOnInUse(ctx, sessionID, ownerID, worker.WorkerType(workerType), workDir, platform, platformKey, botID)
		}
		// RUNNING/IDLE/TERMINATED — try Resume to preserve conversation history.
		// If Resume fails (session files deleted or corrupted), fall back to Start.
		b.log.Info("bridge: orphan platform session, resuming", "session_id", sessionID, "state", si.State)
		if err := b.ResumeSession(ctx, sessionID, workDir); err != nil {
			b.log.Warn("bridge: resume failed, falling back to new session",
				"session_id", sessionID, "state", si.State, "err", err)
			return b.startOrResumeOnInUse(ctx, sessionID, ownerID, worker.WorkerType(workerType), workDir, platform, platformKey, botID)
		}
		return nil
	}

	wt := worker.WorkerType(workerType)
	if wt == "" {
		return fmt.Errorf("bridge: no worker_type configured for platform session %s", sessionID)
	}

	return b.startOrResumeOnInUse(ctx, sessionID, ownerID, wt, workDir, platform, platformKey, botID)
}

// startOrResumeOnInUse attempts StartSession; if the worker reports its session
// files are already in use (leftover from a crashed session), falls back to
// ResumeSession to recover the existing conversation history.
func (b *Bridge) startOrResumeOnInUse(ctx context.Context, sessionID, ownerID string, wt worker.WorkerType, workDir, platform string, platformKey map[string]string, botID string) error {
	if err := b.StartSession(ctx, sessionID, ownerID, botID, wt, nil, workDir, platform, platformKey, ""); err != nil {
		if isWorkerInUseError(err) {
			b.log.Info("bridge: worker rejected as in-use, switching to resume", "session_id", sessionID, "err", err)
			return b.ResumeSession(ctx, sessionID, workDir)
		}
		return err
	}
	return nil
}

// ResetSession terminates the worker, deletes session files, and starts fresh.
// Crash recovery: orphan sessions try Resume first; if files are gone,
// StartPlatformSession falls back to Start(--session-id).
func (b *Bridge) ResetSession(ctx context.Context, sessionID string) error {
	w := b.sm.GetWorker(sessionID)
	if w == nil {
		return fmt.Errorf("bridge: reset: no worker for session %s", sessionID)
	}

	// Increment reset generation so OLD forwardEvents detects the reset
	// after its recv channel closes and exits cleanly without crash handling.
	// The generation counter is monotonic, eliminating the race that existed
	// with the previous boolean flag (where ResetSession reset the flag to
	// false before OLD forwardEvents could check it).
	if rg, ok := w.(resetGenerationer); ok {
		rg.IncResetGeneration()
	}

	// Worker-level reset: Terminate → delete session files → Start fresh.
	if err := w.ResetContext(ctx); err != nil {
		return fmt.Errorf("bridge: reset worker: %w", err)
	}

	// Workers that reset in-place (no process restart, no Conn replacement)
	// keep their existing forwardEvents goroutine. Spawning a new one would
	// create two goroutines reading from the same recvCh.
	if ipr, ok := w.(worker.InPlaceReseter); ok && ipr.InPlaceReset() {
		return nil
	}

	// Start new forwardEvents goroutine for the restarted worker.
	// Track with fwdWg so Shutdown() waits for it (previously missing).
	b.fwdWg.Add(1)
	go func() {
		defer b.fwdWg.Done()
		b.forwardEvents(w, sessionID, forwardOpts{})
	}()

	return nil
}

// SwitchWorkDirResult holds the result of a workdir switch operation.
type SwitchWorkDirResult struct {
	OldSessionID string
	NewSessionID string
	WorkDir      string
	Resumed      bool // true = resumed existing session with conversation history
}

// SwitchWorkDir terminates the current session's worker, transitions it to idle,
// and creates a new session with the given workDir. The new session inherits
// the same user, bot, worker type, and platform context.
// If the target directory has an existing session, it is resumed to preserve
// conversation history. Otherwise a fresh session is created.
func (b *Bridge) SwitchWorkDir(ctx context.Context, oldSessionID, newWorkDir string) (*SwitchWorkDirResult, error) {
	si, err := b.sm.Get(ctx, oldSessionID)
	if err != nil {
		return nil, fmt.Errorf("switch-workdir: get session: %w", err)
	}

	if !si.State.IsActive() {
		return nil, fmt.Errorf("switch-workdir: session not active (state: %s)", si.State)
	}

	expanded, err := validateAndExpandWorkDir(newWorkDir)
	if err != nil {
		return nil, fmt.Errorf("switch-workdir: %w", err)
	}

	// Terminate old worker and park old session.
	if w := b.sm.GetWorker(oldSessionID); w != nil {
		if err := w.Terminate(ctx); err != nil {
			b.log.Warn("switch-workdir: worker terminate failed", "session_id", oldSessionID, "err", err)
		}
		b.sm.DetachWorker(oldSessionID)
	}

	if err := b.sm.Transition(ctx, oldSessionID, events.StateIdle); err != nil {
		b.log.Warn("switch-workdir: transition to idle failed", "session_id", oldSessionID, "err", err)
	}

	// Derive target session key using the new workDir.
	var newID string
	if si.Platform != "" && len(si.PlatformKey) > 0 {
		var pc session.PlatformContext
		pc.Platform = si.Platform
		pc.WorkDir = expanded
		pc.FromMap(si.PlatformKey)
		newID = session.DerivePlatformSessionKey(si.UserID, si.WorkerType, pc)
	} else {
		newID = aep.NewSessionID()
	}

	// Try to resume existing target session first (preserve conversation history).
	resumed := false
	targetSI, err := b.sm.Get(ctx, newID)
	if err == nil && targetSI.State != events.StateDeleted {
		if b.sm.GetWorker(newID) != nil {
			b.log.Warn("switch-workdir: target session already has active worker", "session_id", newID)
		} else if err := b.ResumeSession(ctx, newID, expanded); err != nil {
			b.log.Warn("switch-workdir: resume failed, creating fresh session",
				"session_id", newID, "state", targetSI.State, "err", err)
		} else {
			resumed = true
			b.log.Info("switch-workdir: resumed existing session",
				"old_session_id", oldSessionID,
				"new_session_id", newID,
				"work_dir", expanded,
			)
		}
	}

	if !resumed {
		if err := b.StartSession(ctx, newID, si.UserID, si.BotID, si.WorkerType, si.AllowedTools, expanded, si.Platform, si.PlatformKey, si.Title); err != nil {
			return nil, fmt.Errorf("switch-workdir: start session: %w", err)
		}
		b.log.Info("switch-workdir: created fresh session",
			"old_session_id", oldSessionID,
			"new_session_id", newID,
			"work_dir", expanded,
		)
	}

	return &SwitchWorkDirResult{
		OldSessionID: oldSessionID,
		NewSessionID: newID,
		WorkDir:      expanded,
		Resumed:      resumed,
	}, nil
}

// isWorkerInUseError checks if the worker rejected the session because its
// session files already exist on disk (e.g. from a mid-start crash).
func isWorkerInUseError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "already in use")
}

// Shutdown signals the bridge that the gateway is shutting down.
// It sets the closed flag so forwardEvents goroutines skip crash detection,
// then waits for all forwardEvents goroutines to complete or ctx to expire.
func (b *Bridge) Shutdown(ctx context.Context) {
	b.closed.Store(true)
	done := make(chan struct{})
	go func() {
		b.fwdWg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		b.log.Warn("bridge: shutdown timed out, some forwardEvents goroutines still running")
	}
}

// buildNotifyEnvelope creates a synthetic Message event for user notifications.
func buildNotifyEnvelope(sessionID, msg string, seq int64) *events.Envelope {
	return events.NewEnvelope(aep.NewID(), sessionID, seq, events.Message, map[string]any{"content": msg})
}

// sanitizeLastInput filters control-like text from lastInput before re-delivery
// during crash recovery. When a worker crashes, the last user input is captured
// for crash recovery. If that input matches a control command pattern ($gc, /reset,
// etc.), re-delivering it would cause the new worker to interpret it as a command,
// triggering another termination — defeating the purpose of crash recovery.
func sanitizeLastInput(input string) string {
	if input == "" {
		return ""
	}
	// Single-line control command: discard entirely.
	if messaging.ParseControlCommand(input) != nil {
		return ""
	}
	// Multi-line: filter out lines that are control commands.
	lines := strings.Split(input, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		if messaging.ParseControlCommand(strings.TrimSpace(line)) != nil {
			continue
		}
		filtered = append(filtered, line)
	}
	if len(filtered) == 0 {
		return ""
	}
	return strings.Join(filtered, "\n")
}

// firstNonEmpty returns the first non-empty string from the given values.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// buildWorkerInfo constructs a worker.SessionInfo from session metadata,
// carrying over bridge-level config (workerEnv, blocklist).
func (b *Bridge) buildWorkerInfo(sessionID, userID, workDir string, si *session.SessionInfo) worker.SessionInfo {
	info := worker.SessionInfo{
		SessionID:       sessionID,
		UserID:          userID,
		ProjectDir:      workDir,
		AllowedTools:    si.AllowedTools,
		WorkerSessionID: si.WorkerSessionID,
		ConfigEnv:       b.workerEnv,
		ConfigBlocklist: b.workerEnvBlocklist,
	}

	// MCP config injection — 3 scenarios:
	// 1. Cron platform: suppress all MCP to save ~600 MB per worker
	// 2. Configured MCP servers: restrict workers to declared servers only
	// 3. Not configured: no injection → Claude Code default discovery
	if si.Platform == "cron" {
		info.MCPConfig = `{"mcpServers":{}}`
		info.StrictMCPConfig = true
	} else if mcp, _ := b.mcpConfigJSON.Load().(string); mcp != "" {
		info.MCPConfig = mcp
		info.StrictMCPConfig = true
	}

	return info
}

// injectSlackEnv injects HOTPLEX_SLACK_CHANNEL_ID and HOTPLEX_SLACK_THREAD_TS
// into the worker env map for CLI subcommand auto-resolution.
func injectSlackEnv(info *worker.SessionInfo, platformKey map[string]string) {
	if platformKey == nil {
		return
	}
	if chID := platformKey["channel_id"]; chID != "" {
		if info.Env == nil {
			info.Env = make(map[string]string)
		}
		info.Env["HOTPLEX_SLACK_CHANNEL_ID"] = chID
		if threadTS := platformKey["thread_ts"]; threadTS != "" {
			info.Env["HOTPLEX_SLACK_THREAD_TS"] = threadTS
		}
	}
}

// validateAndExpandWorkDir expands a work directory path and validates it for safety.
// Combines config.ExpandAndAbs + security.ValidateWorkDir into a single call to prevent
// accidental omission of the security check at call sites.
func validateAndExpandWorkDir(input string) (string, error) {
	expanded, err := config.ExpandAndAbs(input)
	if err != nil {
		return "", fmt.Errorf("expand work dir: %w", err)
	}
	if err := security.ValidateWorkDir(expanded); err != nil {
		return "", fmt.Errorf("unsafe work dir: %w", err)
	}
	return expanded, nil
}

// injectGatewayContext injects unified GATEWAY_* environment variables into
// the worker env map. These vars provide platform-agnostic runtime context so
// workers can call platform APIs, construct paths, and understand their session
// without parsing logs or gateway internals.
//
// Existing HOTPLEX_SLACK_* vars are preserved for backward compatibility.
func injectGatewayContext(env map[string]string, platform, botID, userID string, platformKey map[string]string, sessionID, workDir string) map[string]string {
	if env == nil {
		env = make(map[string]string)
	}
	env["GATEWAY_PLATFORM"] = platform
	env["GATEWAY_BOT_ID"] = botID
	env["GATEWAY_USER_ID"] = userID
	env["GATEWAY_SESSION_ID"] = sessionID
	if workDir != "" {
		env["GATEWAY_WORK_DIR"] = workDir
	}
	if chID := firstNonEmpty(platformKey["channel_id"], platformKey["chat_id"]); chID != "" {
		env["GATEWAY_CHANNEL_ID"] = chID
	}
	if threadID := firstNonEmpty(platformKey["thread_ts"], platformKey["message_id"]); threadID != "" {
		env["GATEWAY_THREAD_ID"] = threadID
	}
	if teamID := platformKey["team_id"]; teamID != "" {
		env["GATEWAY_TEAM_ID"] = teamID
	}
	return env
}

func (b *Bridge) sendError(sessionID string, code events.ErrorCode, format string, args ...any) {
	env := events.NewEnvelope(aep.NewID(), sessionID, b.hub.NextSeq(sessionID), events.Error, events.ErrorData{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	})
	_ = b.hub.SendToSession(context.Background(), env)
}
