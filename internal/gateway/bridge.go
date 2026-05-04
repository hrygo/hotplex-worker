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
	Get(id string) (*session.SessionInfo, error)
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
	workerEnvWhitelist []string      // extra whitelist entries from worker.env_whitelist config

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
	return &Bridge{
		log:                deps.Log.With("component", "bridge"),
		hub:                deps.Hub,
		sm:                 deps.SM,
		wf:                 defaultWorkerFactory{},
		collector:          deps.EventCollector,
		retryCtrl:          deps.RetryCtrl,
		agentConfigDir:     deps.AgentConfigDir,
		turnTimeout:        deps.TurnTimeout,
		workerEnv:          deps.WorkerEnv,
		workerEnvWhitelist: deps.WorkerEnvWhitelist,
		retryCancel:        make(map[string]chan struct{}),
		accum:              make(map[string]*sessionAccumulator),
		crashTracker:       make(map[string]*crashHistory),
	}
}

// SetWorkerFactory replaces the default worker factory. Used by tests to inject
// simulated workers without requiring external CLI binaries.
func (b *Bridge) SetWorkerFactory(wf WorkerFactory) {
	b.wf = wf
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

	workerInfo := worker.SessionInfo{
		SessionID:       id,
		UserID:          userID,
		ProjectDir:      workDir,
		AllowedTools:    si.AllowedTools,
		ConfigEnv:       b.workerEnv,
		ConfigWhitelist: b.workerEnvWhitelist,
	}

	// Inject Slack context for CLI subcommand auto-resolution.
	if chID, ok := platformKey["channel_id"]; ok && chID != "" {
		if workerInfo.Env == nil {
			workerInfo.Env = make(map[string]string)
		}
		workerInfo.Env["HOTPLEX_SLACK_CHANNEL_ID"] = chID
		if threadTS, ok := platformKey["thread_ts"]; ok && threadTS != "" {
			workerInfo.Env["HOTPLEX_SLACK_THREAD_TS"] = threadTS
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

	si, err := b.sm.Get(id)
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

	workerInfo := worker.SessionInfo{
		SessionID:       si.ID,
		UserID:          si.UserID,
		AllowedTools:    si.AllowedTools,
		WorkerSessionID: si.WorkerSessionID,
		ProjectDir:      workDir,
		ConfigEnv:       b.workerEnv,
		ConfigWhitelist: b.workerEnvWhitelist,
	}

	// Inject Slack context for CLI subcommand auto-resolution (same as StartSession).
	if si.PlatformKey != nil {
		if chID, ok := si.PlatformKey["channel_id"]; ok && chID != "" {
			if workerInfo.Env == nil {
				workerInfo.Env = make(map[string]string)
			}
			workerInfo.Env["HOTPLEX_SLACK_CHANNEL_ID"] = chID
			if threadTS, ok := si.PlatformKey["thread_ts"]; ok && threadTS != "" {
				workerInfo.Env["HOTPLEX_SLACK_THREAD_TS"] = threadTS
			}
		}
	}

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
	si, err := b.sm.Get(sessionID)
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
	si, err := b.sm.Get(oldSessionID)
	if err != nil {
		return nil, fmt.Errorf("switch-workdir: get session: %w", err)
	}

	if !si.State.IsActive() {
		return nil, fmt.Errorf("switch-workdir: session not active (state: %s)", si.State)
	}

	expanded, err := config.ExpandAndAbs(newWorkDir)
	if err != nil {
		return nil, fmt.Errorf("switch-workdir: expand work dir: %w", err)
	}
	if err := security.ValidateWorkDir(expanded); err != nil {
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
	targetSI, err := b.sm.Get(newID)
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

func (b *Bridge) sendError(sessionID string, code events.ErrorCode, format string, args ...any) {
	env := events.NewEnvelope(aep.NewID(), sessionID, b.hub.NextSeq(sessionID), events.Error, events.ErrorData{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	})
	_ = b.hub.SendToSession(context.Background(), env)
}
