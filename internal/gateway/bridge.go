package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/noop"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// resetGenerationer is an optional interface for workers that support
// reset-aware crash handling via a monotonic generation counter.
type resetGenerationer interface {
	IncResetGeneration() int64
	LoadResetGeneration() int64
}

// Bridge connects the gateway to the session manager.
// It runs the read pump in a goroutine and proxies worker events to the hub.
type Bridge struct {
	log       *slog.Logger
	hub       *Hub
	sm        SessionManager
	msgStore  session.MessageStore // EVT-004: optional; nil means event persistence disabled
	wf        WorkerFactory
	retryCtrl *LLMRetryController

	fwdWg         sync.WaitGroup // tracks active forwardEvents goroutines
	closed        atomic.Bool    // set during shutdown to skip crash detection
	retryCancelMu sync.Mutex
	retryCancel   map[string]chan struct{} // sessionID → cancel channel

	agentConfigDir string        // agent config directory path; "" = disabled
	turnTimeout    time.Duration // per-turn timeout; 0 = disabled

	accum   map[string]*sessionAccumulator // per-session stats accumulator
	accumMu sync.Mutex
}

// NewBridge creates a new bridge. msgStore may be nil (event persistence disabled).
func NewBridge(log *slog.Logger, hub *Hub, sm SessionManager, msgStore session.MessageStore) *Bridge {
	return &Bridge{
		log:         log.With("component", "bridge"),
		hub:         hub,
		sm:          sm,
		msgStore:    msgStore,
		wf:          defaultWorkerFactory{},
		retryCancel: make(map[string]chan struct{}),
		accum:       make(map[string]*sessionAccumulator),
	}
}

// SetWorkerFactory replaces the default worker factory. Used by tests to inject
// simulated workers without requiring external CLI binaries.
func (b *Bridge) SetWorkerFactory(wf WorkerFactory) {
	b.wf = wf
}

// SetRetryController enables automatic LLM error retry.
func (b *Bridge) SetRetryController(ctrl *LLMRetryController) {
	b.retryCtrl = ctrl
}

// SetAgentConfigDir sets the directory from which agent personality/context
// files are loaded. Pass "" to disable agent config loading.
func (b *Bridge) SetAgentConfigDir(dir string) {
	b.agentConfigDir = dir
}

// SetTurnTimeout sets the maximum duration a single worker turn may run
// before being terminated. Pass 0 to disable turn-level timeouts.
func (b *Bridge) SetTurnTimeout(d time.Duration) {
	b.turnTimeout = d
}

// StartSession creates a new session and starts a worker.
func (b *Bridge) StartSession(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, workDir, platform string, platformKey map[string]string) error {
	if b.closed.Load() {
		return fmt.Errorf("bridge: rejecting new session during shutdown")
	}

	// Create session in DB with bot_id and allowed_tools.
	si, err := b.sm.CreateWithBot(ctx, id, userID, botID, wt, allowedTools, platform, platformKey)
	if err != nil {
		return fmt.Errorf("bridge: create session: %w", err)
	}

	// Create worker.
	w, err := b.wf.NewWorker(wt)
	if err != nil {
		return fmt.Errorf("bridge: create worker: %w", err)
	}

	// Attach worker.
	if err := b.sm.AttachWorker(id, w); err != nil {
		_ = b.sm.Delete(ctx, id)
		return fmt.Errorf("bridge: attach worker: %w", err)
	}

	// Start worker.
	workerInfo := worker.SessionInfo{
		SessionID:    id,
		UserID:       userID,
		ProjectDir:   workDir,
		Env:          nil,
		Args:         nil,
		AllowedTools: si.AllowedTools,
	}
	b.injectAgentConfig(&workerInfo, platform)
	if err := w.Start(ctx, workerInfo); err != nil {
		b.sm.DetachWorker(id)
		_ = b.sm.Delete(ctx, id)
		return fmt.Errorf("bridge: start worker: %w", err)
	}

	// Transition to RUNNING. (StateNotifier will emit state event automatically)
	if err := b.sm.Transition(ctx, id, events.StateRunning); err != nil {
		b.log.Warn("bridge: transition to running failed", "session_id", id, "worker_type", wt, "err", err)
	}

	// Forward worker events to hub. Goroutine exits when conn.Recv() is closed
	// (happens when the worker is killed via poolMgr.Close).
	b.fwdWg.Add(1)
	go func() {
		defer b.fwdWg.Done()
		b.forwardEvents(w, id, forwardOpts{})
	}()

	return nil
}

// forwardOpts configures the forwardEvents goroutine behavior.
type forwardOpts struct {
	resumed    bool   // true if this goroutine was spawned by ResumeSession
	workDir    string // workDir to use for resume retry
	retryDepth int    // number of resume retries attempted (limits to 1)
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

	if existing := b.sm.GetWorker(id); existing != nil {
		_ = existing.Terminate(context.Background())
		b.sm.DetachWorker(id)
	}

	// Create worker.
	w, err := b.wf.NewWorker(si.WorkerType)
	if err != nil {
		return fmt.Errorf("bridge: create worker: %w", err)
	}
	if noopw, ok := w.(*noop.Worker); ok {
		conn := noop.NewConn(id, si.UserID)
		noopw.SetConn(conn)
	}
	// Attach worker with quota.
	if err := b.sm.AttachWorker(id, w); err != nil {
		return fmt.Errorf("bridge: attach worker: %w", err)
	}

	// Transition IDLE/RESUMED/TERMINATED sessions to RUNNING.
	if si.State != events.StateRunning {
		if err := b.sm.Transition(ctx, id, events.StateRunning); err != nil {
			return err
		}
	}

	// Start worker.
	workerInfo := worker.SessionInfo{
		SessionID:       si.ID,
		UserID:          si.UserID,
		AllowedTools:    si.AllowedTools,
		WorkerSessionID: si.WorkerSessionID,
		ProjectDir:      workDir,
	}
	b.injectAgentConfig(&workerInfo, si.Platform)
	if err := w.Resume(ctx, workerInfo); err != nil {
		b.sm.DetachWorker(id)
		return fmt.Errorf("bridge: resume start: %w", err)
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

	// Forward worker events to hub. Same as StartSession — goroutine exits when
	// conn.Recv() closes (worker killed via poolMgr.Close or worker exit).
	b.fwdWg.Add(1)
	go func() {
		defer b.fwdWg.Done()
		b.forwardEvents(w, id, opts)
	}()

	return nil
}

// copyEnvelope delegates to events.Clone, which performs a deep copy of
// map[string]any Event.Data to eliminate shared map headers.
// This prevents data races when Hub.Run encodes the clone concurrently with
// Bridge.forwardEvents encoding the original (e.g., for msgStore.Append).
var _ = events.Clone // compile-time check that Clone is accessible

func (b *Bridge) persistWorkerSessionID(w worker.Worker, sessionID string) {
	handler, ok := w.(worker.WorkerSessionIDHandler)
	if !ok {
		return
	}
	workerSID := handler.GetWorkerSessionID()
	if workerSID == "" {
		return
	}
	if err := b.sm.UpdateWorkerSessionID(context.Background(), sessionID, workerSID); err != nil {
		b.log.Warn("bridge: failed to persist worker session ID", "session_id", sessionID, "worker_session_id", workerSID, "err", err)
	} else {
		b.log.Debug("bridge: persisted worker session ID", "session_id", sessionID, "worker_session_id", workerSID)
	}
}

// forwardEvents proxies worker events to the hub with seq assignment.
// EVT-004: if msgStore is configured, it appends to the event log on done events.
// AEP-020: after the recv channel closes, calls Worker.Wait() to determine exit
// code and sets DoneData.Success accordingly (non-zero exit = crash = success=false).
func (b *Bridge) forwardEvents(w worker.Worker, sessionID string, opts forwardOpts) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("bridge: panic in forwardEvents", "session_id", sessionID, "panic", r, "stack", string(debug.Stack()))
		}
	}()
	workerType := w.Type()
	b.log.Debug("bridge: forwardEvents goroutine started", "session_id", sessionID, "worker_type", workerType, "resumed", opts.resumed)
	startTime := time.Now()
	firstEvent := true
	doneReceived := false

	// Capture reset generation at goroutine start. If a reset happens while
	// this goroutine is running, the generation will differ when we check
	// after the recv channel closes, and we exit cleanly without crash handling.
	var myGen int64
	if rg, ok := w.(resetGenerationer); ok {
		myGen = rg.LoadResetGeneration()
	}

	// LLM retry: accumulate turn text and error data for retry detection.
	var turnText strings.Builder
	var lastError *events.ErrorData
	var pendingError *events.Envelope // buffered error event; suppressed if retry triggers

	// Turn timeout: kill worker if a single turn exceeds the configured duration.
	// Timer resets on every received event; stops on done.
	var turnTimerFired atomic.Bool
	var turnTimer *time.Timer
	if b.turnTimeout > 0 {
		turnTimer = time.AfterFunc(b.turnTimeout, func() {
			if !turnTimerFired.CompareAndSwap(false, true) {
				return
			}
			b.log.Warn("bridge: turn timeout exceeded, terminating worker",
				"session_id", sessionID, "worker_type", workerType, "turn_timeout", b.turnTimeout)
			timeoutEvt := events.NewEnvelope(aep.NewID(), sessionID,
				b.hub.NextSeq(sessionID), events.Error, events.ErrorData{
					Code:    "TURN_TIMEOUT",
					Message: fmt.Sprintf("Turn exceeded %v time limit and was terminated.", b.turnTimeout),
				})
			_ = b.hub.SendToSession(context.Background(), timeoutEvt)
			_ = w.Terminate(context.Background())
		})
		defer turnTimer.Stop()
	}

	for env := range w.Conn().Recv() {
		if env.Event.Type == events.Error {
			b.log.Warn("bridge: received error from worker", "session_id", sessionID, "worker_type", workerType, "data", env.Event.Data)
			// Capture last error for retry detection.
			if ed, ok := env.Event.Data.(events.ErrorData); ok {
				lastError = &ed
			}
			// When retry is enabled, buffer the error event instead of forwarding
			// immediately. If the subsequent Done triggers a retry, the error is
			// suppressed (user sees the notify message instead of raw LLM error).
			// If no retry triggers, the error is forwarded after Done.
			if b.retryCtrl != nil {
				cloned := events.Clone(env)
				cloned.SessionID = sessionID
				pendingError = cloned
				continue
			}
		} else if b.log.Enabled(context.Background(), slog.LevelDebug) {
			b.log.Debug("bridge: received event from worker", "session_id", sessionID, "worker_type", workerType, "event_type", env.Event.Type)
		}
		// Capture and persist worker-internal session ID on first event
		if firstEvent {
			b.persistWorkerSessionID(w, sessionID)
			firstEvent = false
		}

		// Turn timeout: reset timer on every received event (turn is still alive).
		// Skip if timer already fired (worker is being terminated).
		if turnTimer != nil && !turnTimerFired.Load() {
			turnTimer.Reset(b.turnTimeout)
		}
		// If turn timer fired, drain remaining events without processing.
		if turnTimerFired.Load() {
			continue
		}

		// Make a defensive copy before mutating SessionID to avoid a data race
		// with Hub.Run which reads env during JSON encoding (hub mutates Seq).
		env = events.Clone(env)
		env.SessionID = sessionID
		// Seq is assigned by hub.SendToSession via SeqGen (seq=0 triggers auto-assignment).

		// LLM retry: accumulate text from streaming output.
		if env.Event.Type == events.MessageDelta || env.Event.Type == events.Message {
			if content := extractMessageContent(env); content != "" {
				turnText.WriteString(content)
			}
		}

		// Stats accumulation: track tool calls and merge per-turn stats on done.
		switch env.Event.Type {
		case events.ToolCall:
			acc := b.getOrInitAccum(sessionID)
			acc.ToolCallCount++
		case events.Done:
			if turnTimer != nil {
				turnTimer.Stop()
			}
			acc := b.getOrInitAccum(sessionID)
			if dd, ok := env.Event.Data.(events.DoneData); ok {
				acc.mergePerTurnStats(dd)
			}
			acc.TurnCount++
			b.injectSessionStats(env, acc)
			if b.log.Enabled(context.Background(), slog.LevelDebug) {
				b.log.Debug("bridge: turn completed",
					"session_id", sessionID, "worker_type", workerType, "turn", acc.TurnCount,
					"duration", time.Since(startTime).Round(time.Millisecond),
					"text_len", turnText.Len(), "tools", acc.ToolCallCount)
			}
		}

		// UI Reconciliation (Fallback full message if silent dropped)
		if env.Event.Type == events.Done {
			doneReceived = true
			if b.hub.GetAndClearDropped(sessionID) {
				b.log.Warn("bridge: handling dropped deltas before done", "session_id", sessionID, "worker_type", workerType)

				if dataMap, ok := env.Event.Data.(map[string]any); ok {
					if stats, ok := dataMap["stats"].(map[string]any); ok {
						stats["dropped"] = true
					} else {
						dataMap["stats"] = map[string]any{"dropped": true}
					}
				} else if doneData, ok := env.Event.Data.(events.DoneData); ok {
					doneData.Dropped = true
					env.Event.Data = doneData
				} else if doneDataPtr, ok := env.Event.Data.(*events.DoneData); ok {
					doneDataPtr.Dropped = true
					env.Event.Data = doneDataPtr
				}
			}
		}

		if err := b.hub.SendToSession(context.Background(), env); err != nil {
			b.log.Warn("bridge: forward event failed", "err", err, "session_id", sessionID, "worker_type", workerType, "event_type", env.Event.Type)
		}

		// Flush buffered error on non-Done events (no retry decision possible yet).
		if pendingError != nil && env.Event.Type != events.Done {
			if err := b.hub.SendToSession(context.Background(), pendingError); err != nil {
				b.log.Warn("bridge: forward buffered error failed", "err", err, "session_id", sessionID, "worker_type", workerType)
			}
			pendingError = nil
		}

		// EVT-004: append to MessageStore on done events (end of each turn).
		if b.msgStore != nil && env.Event.Type == events.Done {
			payload, _ := aep.EncodeJSON(env)
			if err := b.msgStore.Append(context.Background(), env.SessionID, env.Seq, string(env.Event.Type), payload); err != nil {
				b.log.Warn("bridge: msgstore append", "err", err, "session_id", sessionID)
			}
		}

		// LLM retry: check after Done is forwarded and persisted.
		if env.Event.Type == events.Done && b.retryCtrl != nil {
			if shouldRetry, attempt := b.retryCtrl.ShouldRetry(sessionID, turnText.String(), lastError); shouldRetry {
				// Suppress buffered error — user sees the notify message instead of raw LLM error.
				pendingError = nil
				b.autoRetry(context.Background(), w, sessionID, attempt)
				turnText.Reset()
				lastError = nil
				continue
			}
			// No retry — flush buffered error event to client.
			if pendingError != nil {
				if err := b.hub.SendToSession(context.Background(), pendingError); err != nil {
					b.log.Warn("bridge: forward buffered error failed", "err", err, "session_id", sessionID, "worker_type", workerType)
				}
				pendingError = nil
			}
			b.retryCtrl.RecordSuccess(sessionID)
			turnText.Reset()
			lastError = nil
		}
	}

	// Check reset generation: if a reset happened while this goroutine was
	// running, the generation counter will differ from our captured value.
	// This is race-free because the counter is monotonic — OLD forwardEvents
	// always sees a different generation than what it captured at start.
	if rg, ok := w.(resetGenerationer); ok && rg.LoadResetGeneration() != myGen {
		b.log.Info("bridge: worker reset, old forwardEvents exiting", "session_id", sessionID, "worker_type", workerType, "my_gen", myGen, "cur_gen", rg.LoadResetGeneration())
		return
	}

	// AEP-020: Worker.Recv() closed — get exit code to determine crash vs normal exit.
	// Wrap Wait() with a timeout to avoid blocking the goroutine if the process
	// is stuck in an unreported state (e.g. zombie or kernel-level hang).
	// Use a longer timeout during shutdown to allow graceful termination.
	waitTimeout := 2 * time.Second
	if b.closed.Load() {
		waitTimeout = 10 * time.Second
	}
	var exitCode int
	ch := make(chan struct{})
	go func() {
		exitCode, _ = w.Wait()
		close(ch)
	}()
	select {
	case <-ch:
		// Wait() returned; check exit code.
	case <-time.After(waitTimeout):
		// Force-kill to ensure the Wait() goroutine completes and doesn't leak.
		b.log.Warn("bridge: Wait() timed out, force-killing", "session_id", sessionID, "worker_type", workerType)
		_ = w.Kill()
		<-ch // drain the goroutine
	}

	// Resume retry: if the resumed worker crashed quickly (within 15s), it likely
	// failed to restore its conversation state. Retry resume once (limited by
	// retryDepth) to recover, then fall back to fresh start if retry also fails.
	// Skip during shutdown to avoid spawning workers that will be immediately killed.
	fallbackAttempted := !b.closed.Load() && exitCode != 0 && opts.resumed && opts.retryDepth < 2 && time.Since(startTime) < 15*time.Second
	if fallbackAttempted {
		// Extract last input from dead worker's conn for re-delivery after fresh start.
		var lastInput string
		if conn := w.Conn(); conn != nil {
			if ir, ok := conn.(worker.InputRecoverer); ok {
				lastInput = sanitizeLastInput(ir.LastInput())
			}
		}
		if b.attemptResumeFallback(fallbackParams{
			sessionID:  sessionID,
			workDir:    opts.workDir,
			exitCode:   exitCode,
			retryDepth: opts.retryDepth,
			workerType: workerType,
			lastInput:  lastInput,
		}) {
			return // new forwardEvents goroutine took over
		}
		// Fallback already cleaned up (DetachWorker + Transition); skip redundant cleanup below.
	}

	// During shutdown, skip crash detection — workers are SIGTERM'd by design.
	// Just clean up without sending crash done events or incrementing crash metrics.
	if b.closed.Load() {
		b.cleanupCrashedWorker(sessionID)
		return
	}

	// If session is already TERMINATED (e.g., handler client_kill), the handler
	// already sends error + done events. Skip sending redundant crash/synthetic
	// done — only detach and clean up.
	if b.sm != nil {
		si, smErr := b.sm.Get(sessionID)
		if smErr == nil && si.State == events.StateTerminated {
			b.log.Debug("bridge: session already terminated, skipping done for handler-killed worker", "session_id", sessionID, "worker_type", workerType)
			if !fallbackAttempted {
				b.cleanupCrashedWorker(sessionID)
			}
			return
		}
	}

	if exitCode != 0 {
		acc := b.getOrInitAccum(sessionID)
		b.log.Warn("bridge: worker exited with non-zero code, sending crash done",
			"session_id", sessionID, "worker_type", workerType, "exit_code", exitCode,
			"duration", time.Since(startTime).Round(time.Millisecond), "turn_count", acc.TurnCount)
		metrics.WorkerCrashesTotal.WithLabelValues(string(workerType), fmt.Sprintf("%d", exitCode)).Inc()
		crashDone := events.NewEnvelope(aep.NewID(), sessionID, b.hub.NextSeq(sessionID), events.Done, events.DoneData{
			Success: false,
			Stats:   map[string]any{"crash_exit_code": exitCode},
		})
		_ = b.hub.SendToSession(context.Background(), crashDone)
	} else if !doneReceived {
		// Worker exited without sending a done event (e.g., ResetContext consumed
		// the exit code). Send a synthetic done so platform connections clean up
		// typing indicators, streaming cards, and tool reactions.
		b.log.Debug("bridge: sending synthetic done for platform cleanup", "session_id", sessionID, "worker_type", workerType)
		syntheticDone := events.NewEnvelope(aep.NewID(), sessionID, b.hub.NextSeq(sessionID), events.Done, events.DoneData{
			Success: false,
			Stats:   map[string]any{"synthetic": true},
		})
		_ = b.hub.SendToSession(context.Background(), syntheticDone)
	}

	// Clean up: detach the dead worker and transition session to TERMINATED
	// so the next message triggers orphan resume instead of silently dropping input.
	// Skip when attemptResumeFallback already performed cleanup.
	if !fallbackAttempted {
		b.cleanupCrashedWorker(sessionID)
	}
}

// fallbackParams carries the context needed by attemptResumeFallback.
type fallbackParams struct {
	sessionID  string
	workDir    string
	exitCode   int
	retryDepth int
	workerType worker.WorkerType
	lastInput  string
}

// attemptResumeFallback handles a crashed resumed worker with a two-step strategy:
//  1. retryDepth < 1: Retry resume once to preserve conversation history (transient failures).
//  2. retryDepth >= 1: Fall back to fresh start — conversation data is permanently lost.
//
// Returns true if a new forwardEvents goroutine took over.
func (b *Bridge) attemptResumeFallback(p fallbackParams) bool {
	b.log.Warn("bridge: worker crashed shortly after resume",
		"session_id", p.sessionID, "worker_type", p.workerType, "exit_code", p.exitCode, "retry_depth", p.retryDepth)

	// Clean up the crashed worker first.
	b.cleanupCrashedWorker(p.sessionID)

	// Step 1: Retry resume once for transient failures (e.g., file lock, timing).
	if p.retryDepth == 0 {
		if err := b.resumeWithOpts(context.Background(), p.sessionID, p.workDir, forwardOpts{resumed: true, workDir: p.workDir, retryDepth: p.retryDepth + 1}); err != nil {
			b.log.Error("bridge: resume retry failed synchronously, falling back to fresh start", "session_id", p.sessionID, "worker_type", p.workerType, "err", err)
			// Synchronous failure — fall through to fresh start below.
		} else {
			b.log.Info("bridge: resume retry succeeded", "session_id", p.sessionID, "worker_type", p.workerType)
			warnEvt := events.NewEnvelope(aep.NewID(), p.sessionID, b.hub.NextSeq(p.sessionID), events.Error, events.ErrorData{
				Code:    events.ErrCodeResumeRetry,
				Message: fmt.Sprintf("Worker crashed after resume (exit %d), retried resume to preserve conversation.", p.exitCode),
			})
			_ = b.hub.SendToSession(context.Background(), warnEvt)
			return true
		}
	}

	// Step 2: Resume retry also failed or retryDepth exhausted — start fresh worker.
	// Conversation data is permanently lost (e.g., "No conversation found").
	b.log.Info("bridge: starting fresh worker after failed resume", "session_id", p.sessionID, "worker_type", p.workerType)

	si, err := b.sm.Get(p.sessionID)
	if err != nil {
		b.log.Error("bridge: session not found for fresh start fallback", "session_id", p.sessionID, "err", err)
		return false
	}

	w, err := b.wf.NewWorker(si.WorkerType)
	if err != nil {
		b.log.Error("bridge: create worker for fresh start", "session_id", p.sessionID, "err", err)
		return false
	}
	if noopw, ok := w.(*noop.Worker); ok {
		noopw.SetConn(noop.NewConn(si.ID, si.UserID))
	}

	if err := b.sm.AttachWorker(p.sessionID, w); err != nil {
		b.log.Error("bridge: attach worker for fresh start", "session_id", p.sessionID, "err", err)
		return false
	}

	if err := b.sm.Transition(context.Background(), p.sessionID, events.StateRunning); err != nil {
		b.log.Warn("bridge: transition to running for fresh start", "session_id", p.sessionID, "err", err)
	}

	workerInfo := worker.SessionInfo{
		SessionID:       si.ID,
		UserID:          si.UserID,
		AllowedTools:    si.AllowedTools,
		WorkerSessionID: si.WorkerSessionID,
		ProjectDir:      p.workDir,
	}
	b.injectAgentConfig(&workerInfo, si.Platform)
	if err := w.Start(context.Background(), workerInfo); err != nil {
		b.sm.DetachWorker(p.sessionID)
		b.log.Error("bridge: fresh worker start failed", "session_id", p.sessionID, "err", err)
		return false
	}

	b.fwdWg.Add(1)
	go func() {
		defer b.fwdWg.Done()
		b.forwardEvents(w, p.sessionID, forwardOpts{})
	}()

	// Re-deliver the original input that was lost when the first worker crashed.
	if p.lastInput != "" {
		b.log.Info("bridge: re-delivering input to fresh worker", "session_id", p.sessionID, "content_len", len(p.lastInput))
		if err := w.Input(context.Background(), p.lastInput, nil); err != nil {
			b.log.Warn("bridge: input re-delivery failed", "session_id", p.sessionID, "err", err)
		}
	}

	b.log.Info("bridge: fresh worker started after resume failure", "session_id", p.sessionID, "worker_type", p.workerType)
	warnEvt := events.NewEnvelope(aep.NewID(), p.sessionID, b.hub.NextSeq(p.sessionID), events.Error, events.ErrorData{
		Code:    events.ErrCodeResumeRetry,
		Message: fmt.Sprintf("Conversation data lost (exit %d), started fresh session.", p.exitCode),
	})
	_ = b.hub.SendToSession(context.Background(), warnEvt)
	return true
}

// cleanupCrashedWorker detaches the dead worker and transitions the session to TERMINATED
// so the next message triggers orphan resume instead of silently dropping input.
func (b *Bridge) cleanupCrashedWorker(sessionID string) {
	acc := b.getOrInitAccum(sessionID)
	b.log.Debug("bridge: cleaning up crashed worker", "session_id", sessionID, "turn_count", acc.TurnCount)
	b.deleteAccum(sessionID)
	if b.sm == nil {
		return
	}
	b.sm.DetachWorker(sessionID)
	if err := b.sm.Transition(context.Background(), sessionID, events.StateTerminated); err != nil {
		b.log.Debug("bridge: transition to terminated after worker exit", "session_id", sessionID, "err", err)
	}
}

// StartPlatformSession creates a session for a platform message if it doesn't already exist.
// Implements messaging.SessionStarter. Idempotent: returns nil if session exists with a live worker.
//
// Decision logic (state-based with Resume→Start fallback):
//  1. No DB record → Create + Start (--session-id)
//  2. Worker alive → Reuse (forward message)
//  3. No worker, state=CREATED → Start (--session-id)
//  4. No worker, state=RUNNING/IDLE/TERMINATED → Resume (--resume)
//     If Resume fails (files gone/corrupted), fall back to Start (--session-id)
func (b *Bridge) StartPlatformSession(ctx context.Context, sessionID, ownerID, workerType, workDir, platform string, platformKey map[string]string) error {
	b.log.Debug("bridge: StartPlatformSession called", "session_id", sessionID, "owner_id", ownerID, "worker_type", workerType, "work_dir", workDir, "platform", platform, "platform_key", platformKey)
	si, err := b.sm.Get(sessionID)
	if err == nil {
		if w := b.sm.GetWorker(sessionID); w != nil {
			return nil
		}
		// Orphan: session record exists but worker is gone.
		if si.State == events.StateCreated {
			b.log.Info("bridge: orphan platform session unstarted, starting fresh", "session_id", sessionID)
			return b.startOrResumeOnInUse(ctx, sessionID, ownerID, worker.WorkerType(workerType), workDir, platform, platformKey)
		}
		// RUNNING/IDLE/TERMINATED — try Resume to preserve conversation history.
		// If Resume fails (session files deleted or corrupted), fall back to Start.
		b.log.Info("bridge: orphan platform session, resuming", "session_id", sessionID, "state", si.State)
		if err := b.ResumeSession(ctx, sessionID, workDir); err != nil {
			b.log.Warn("bridge: resume failed, falling back to new session",
				"session_id", sessionID, "state", si.State, "err", err)
			return b.startOrResumeOnInUse(ctx, sessionID, ownerID, worker.WorkerType(workerType), workDir, platform, platformKey)
		}
		return nil
	}

	wt := worker.WorkerType(workerType)
	if wt == "" {
		return fmt.Errorf("bridge: no worker_type configured for platform session %s", sessionID)
	}

	return b.startOrResumeOnInUse(ctx, sessionID, ownerID, wt, workDir, platform, platformKey)
}

// startOrResumeOnInUse attempts StartSession; if the worker reports its session
// files are already in use (leftover from a crashed session), falls back to
// ResumeSession to recover the existing conversation history.
func (b *Bridge) startOrResumeOnInUse(ctx context.Context, sessionID, ownerID string, wt worker.WorkerType, workDir, platform string, platformKey map[string]string) error {
	if err := b.StartSession(ctx, sessionID, ownerID, "", wt, nil, workDir, platform, platformKey); err != nil {
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

// isWorkerInUseError checks if the worker rejected the session because its
// session files already exist on disk (e.g. from a mid-start crash).
func isWorkerInUseError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "already in use")
}

// Shutdown signals the bridge that the gateway is shutting down.
// It sets the closed flag so forwardEvents goroutines skip crash detection,
// then waits for all forwardEvents goroutines to complete.
func (b *Bridge) Shutdown() {
	b.closed.Store(true)
	b.fwdWg.Wait()
}

// CancelRetry cancels any pending auto-retry for a session.
// Called by handler when a user sends a new input.
func (b *Bridge) CancelRetry(sessionID string) {
	b.retryCancelMu.Lock()
	defer b.retryCancelMu.Unlock()
	if ch, ok := b.retryCancel[sessionID]; ok {
		close(ch)
		delete(b.retryCancel, sessionID)
	}
}

// injectAgentConfig loads agent config files and injects the unified system
// prompt into session info. A no-op when config dir is empty or agent config
// is not configured.
func (b *Bridge) injectAgentConfig(info *worker.SessionInfo, platform string) {
	if b.agentConfigDir == "" {
		return
	}
	configs, err := agentconfig.Load(b.agentConfigDir, platform)
	if err != nil {
		b.log.Warn("bridge: agent config load failed", "dir", b.agentConfigDir, "err", err)
		return
	}
	if configs.IsEmpty() {
		return
	}

	if prompt := agentconfig.BuildSystemPrompt(configs); prompt != "" {
		info.SystemPrompt = prompt
	}
}

// autoRetry performs exponential backoff then sends the retry input to the worker.
func (b *Bridge) autoRetry(ctx context.Context, w worker.Worker, sessionID string, attempt int) {
	delay := b.retryCtrl.Delay(attempt)

	// Notify user if enabled.
	if b.retryCtrl.ShouldNotify() {
		msg := b.retryCtrl.NotifyMessage(attempt)
		notifyEnv := buildNotifyEnvelope(sessionID, msg, b.hub.NextSeq(sessionID))
		_ = b.hub.SendToSession(ctx, notifyEnv)
	}

	// Register cancel channel.
	cancelCh := make(chan struct{})
	b.retryCancelMu.Lock()
	b.retryCancel[sessionID] = cancelCh
	b.retryCancelMu.Unlock()

	// Wait with backoff, respecting cancellation.
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-cancelCh:
		b.log.Info("bridge: auto-retry cancelled by user input", "session_id", sessionID)
		return
	case <-timer.C:
	}

	// Send retry input to worker.
	b.log.Info("bridge: auto-retry sending input", "session_id", sessionID, "attempt", attempt)
	if err := w.Input(ctx, b.retryCtrl.RetryInput(), nil); err != nil {
		b.log.Warn("bridge: auto-retry input failed", "session_id", sessionID, "err", err)
	}

	// Clean up cancel channel.
	b.retryCancelMu.Lock()
	delete(b.retryCancel, sessionID)
	b.retryCancelMu.Unlock()
}

// extractMessageContent extracts text content from a message or message_delta event.
func extractMessageContent(env *events.Envelope) string {
	switch env.Event.Type {
	case events.Message, events.MessageDelta:
		if m, ok := env.Event.Data.(map[string]any); ok {
			if content, ok := m["content"].(string); ok {
				return content
			}
		}
	}
	return ""
}

// buildNotifyEnvelope creates a synthetic Message event for user notifications.
func buildNotifyEnvelope(sessionID, msg string, seq int64) *events.Envelope {
	return events.NewEnvelope(aep.NewID(), sessionID, seq, events.Message, map[string]any{"content": msg})
}

// getOrInitAccum returns the session accumulator, creating one if needed.
func (b *Bridge) getOrInitAccum(sessionID string) *sessionAccumulator {
	b.accumMu.Lock()
	defer b.accumMu.Unlock()
	if acc, ok := b.accum[sessionID]; ok {
		return acc
	}
	acc := &sessionAccumulator{StartedAt: time.Now()}
	b.accum[sessionID] = acc
	return acc
}

// deleteAccum removes the accumulator for a session (called on cleanup).
func (b *Bridge) deleteAccum(sessionID string) {
	b.accumMu.Lock()
	delete(b.accum, sessionID)
	b.accumMu.Unlock()
}

// injectSessionStats merges the accumulator snapshot into DoneData.Stats["_session"].
func (b *Bridge) injectSessionStats(env *events.Envelope, acc *sessionAccumulator) {
	dd, ok := env.Event.Data.(events.DoneData)
	if !ok {
		return
	}
	if dd.Stats == nil {
		dd.Stats = make(map[string]any)
	}
	dd.Stats["_session"] = acc.snapshot()
	env.Event.Data = dd
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
