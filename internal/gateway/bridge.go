package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hotplex/hotplex-worker/internal/metrics"
	"github.com/hotplex/hotplex-worker/internal/session"
	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/internal/worker/noop"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// Bridge connects the gateway to the session manager.
// It runs the read pump in a goroutine and proxies worker events to the hub.
type Bridge struct {
	log      *slog.Logger
	hub      *Hub
	sm       SessionManager
	msgStore session.MessageStore // EVT-004: optional; nil means event persistence disabled
	wf       WorkerFactory
}

// NewBridge creates a new bridge. msgStore may be nil (event persistence disabled).
func NewBridge(log *slog.Logger, hub *Hub, sm SessionManager, msgStore session.MessageStore) *Bridge {
	return &Bridge{
		log:      log,
		hub:      hub,
		sm:       sm,
		msgStore: msgStore,
		wf:       defaultWorkerFactory{},
	}
}

// SetWorkerFactory replaces the default worker factory. Used by tests to inject
// simulated workers without requiring external CLI binaries.
func (b *Bridge) SetWorkerFactory(wf WorkerFactory) {
	b.wf = wf
}

// StartSession creates a new session and starts a worker.
func (b *Bridge) StartSession(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, workDir string) error {
	// Create session in DB with bot_id and allowed_tools.
	si, err := b.sm.CreateWithBot(ctx, id, userID, botID, wt, allowedTools)
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
	if err := w.Start(ctx, workerInfo); err != nil {
		b.sm.DetachWorker(id)
		_ = b.sm.Delete(ctx, id)
		return fmt.Errorf("bridge: start worker: %w", err)
	}

	// Transition to RUNNING. (StateNotifier will emit state event automatically)
	if err := b.sm.Transition(ctx, id, events.StateRunning); err != nil {
		b.log.Warn("bridge: transition to running failed", "id", id, "err", err)
	}

	// Forward worker events to hub. Goroutine exits when conn.Recv() is closed
	// (happens when the worker is killed via poolMgr.Close).
	go b.forwardEvents(w, id)

	return nil
}

// ResumeSession reattaches to an existing session.
// workDir overrides the stored project directory (used by platform sessions that need a consistent workspace).
func (b *Bridge) ResumeSession(ctx context.Context, id, workDir string) error {
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
	return b.hub.SendToSession(ctx, stateEvt)
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
func (b *Bridge) forwardEvents(w worker.Worker, sessionID string) {
	b.log.Info("bridge: forwardEvents goroutine started", "session_id", sessionID)
	firstEvent := true
	for env := range w.Conn().Recv() {
		if env.Event.Type == events.Error {
			b.log.Warn("bridge: received error from worker", "session_id", sessionID, "data", env.Event.Data)
		} else {
			b.log.Debug("bridge: received event from worker", "session_id", sessionID, "event_type", env.Event.Type)
		}
		// Capture and persist worker-internal session ID on first event
		if firstEvent {
			b.persistWorkerSessionID(w, sessionID)
			firstEvent = false
		}
		// Make a defensive copy before mutating SessionID to avoid a data race
		// with Hub.Run which reads env during JSON encoding (hub mutates Seq).
		env = events.Clone(env)
		env.SessionID = sessionID
		// Seq is assigned by hub.SendToSession via SeqGen (seq=0 triggers auto-assignment).

		// UI Reconciliation (Fallback full message if silent dropped)
		if env.Event.Type == events.Done {
			if b.hub.GetAndClearDropped(sessionID) {
				b.log.Warn("gateway: handling dropped deltas before done", "session_id", sessionID)

				// Optional: Here we could inject a raw `message` pulling full state from Worker.
				// For now, we mutate the `done` event to pass the `dropped: true` flag inside `stats`.
				if dataMap, ok := env.Event.Data.(map[string]any); ok {
					if stats, ok := dataMap["stats"].(map[string]any); ok {
						stats["dropped"] = true
					} else {
						dataMap["stats"] = map[string]any{"dropped": true}
					}
					// Update with custom DoneData if needed
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
			b.log.Warn("bridge: forward event failed", "err", err, "session_id", sessionID)
		}

		// EVT-004: append to MessageStore on done events (end of each turn).
		// The Append call is async and non-blocking; failures are logged but do not
		// affect the event stream.
		if b.msgStore != nil && env.Event.Type == events.Done {
			payload, _ := aep.EncodeJSON(env)
			if err := b.msgStore.Append(context.Background(), env.SessionID, env.Seq, string(env.Event.Type), payload); err != nil {
				b.log.Warn("bridge: msgstore append", "err", err, "session_id", sessionID)
			}
		}
	}

	// AEP-020: Worker.Recv() closed — get exit code to determine crash vs normal exit.
	// Wrap Wait() with a 2s timeout to avoid blocking the goroutine if the process
	// is stuck in an unreported state (e.g. zombie or kernel-level hang).
	var exitCode int
	ch := make(chan struct{})
	go func() {
		exitCode, _ = w.Wait()
		close(ch)
	}()
	select {
	case <-ch:
		// Wait() returned; check exit code.
	case <-time.After(2 * time.Second):
		b.log.Warn("gateway: Wait() timed out, skipping crash detection", "session_id", sessionID)
	}
	if exitCode != 0 {
		b.log.Warn("gateway: worker exited with non-zero code, sending crash done", "session_id", sessionID, "exit_code", exitCode)
		metrics.WorkerCrashesTotal.WithLabelValues(string(w.Type()), fmt.Sprintf("%d", exitCode)).Inc()
		crashDone := events.NewEnvelope(aep.NewID(), sessionID, b.hub.NextSeq(sessionID), events.Done, events.DoneData{
			Success: false,
			Stats:   map[string]any{"crash_exit_code": exitCode},
		})
		_ = b.hub.SendToSession(context.Background(), crashDone)
	}
}

// StartPlatformSession creates a session for a platform message if it doesn't already exist.
// Implements messaging.SessionStarter. Idempotent: returns nil if session exists with a live worker.
// If the session exists but has no worker (orphan from a previous gateway restart), it resumes
// the existing session so the worker can restore its internal session state.
func (b *Bridge) StartPlatformSession(ctx context.Context, sessionID, ownerID, workerType, workDir string) error {
	b.log.Debug("bridge: StartPlatformSession called", "session_id", sessionID, "owner_id", ownerID, "worker_type", workerType, "work_dir", workDir)
	_, err := b.sm.Get(sessionID)
	if err == nil {
		if w := b.sm.GetWorker(sessionID); w != nil {
			return nil
		}
		// Orphan: session record exists but worker is gone (gateway restarted).
		// Resume the session so the worker restores its internal session state
		// (e.g., Claude Code's --resume flag, OpenCode CLI's session continuity).
		b.log.Info("gateway: orphan platform session, resuming", "session_id", sessionID)
		return b.ResumeSession(ctx, sessionID, workDir)
	}

	wt := worker.WorkerType(workerType)
	if wt == "" {
		return fmt.Errorf("gateway: no worker_type configured for platform session %s", sessionID)
	}

	return b.StartSession(ctx, sessionID, ownerID, "", wt, nil, workDir)
}
