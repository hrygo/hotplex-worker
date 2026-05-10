package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// handleControl processes client-originated control messages (terminate, delete).
// Server-originated control messages (reconnect, session_invalid, throttle) are
// sent via SendControlToSession.
func (h *Handler) handleControl(ctx context.Context, env *events.Envelope) error {
	var action string
	switch d := env.Event.Data.(type) {
	case events.ControlData:
		action = string(d.Action)
	case map[string]any:
		action, _ = d["action"].(string)
	default:
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "control: invalid data")
	}

	h.log.Info("gateway: control received", "action", action, "session_id", env.SessionID)

	switch events.ControlAction(action) {
	case events.ControlActionTerminate:
		// Ownership check: only the session owner can terminate.
		if err := h.sm.ValidateOwnership(ctx, env.SessionID, env.OwnerID, ""); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
		}
		// Transition to TERMINATED and kill the worker.
		if err := h.sm.TransitionWithReason(ctx, env.SessionID, events.StateTerminated, "client_kill"); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "terminate failed: %v", err)
		}
		_ = h.sendErrorf(ctx, env, events.ErrCodeSessionTerminated, "session terminated by client")
		return nil

	case events.ControlActionDelete:
		// Ownership check: only the session owner can delete.
		if err := h.sm.ValidateOwnership(ctx, env.SessionID, env.OwnerID, ""); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
		}
		// Delete the session (bypasses TERMINATED state per design §5).
		if err := h.sm.Delete(ctx, env.SessionID); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "delete failed: %v", err)
		}
		return nil

	case events.ControlActionReset:
		return h.handleReset(ctx, env)

	case events.ControlActionGC:
		return h.handleGC(ctx, env)

	case events.ControlActionCD:
		return h.handleCD(ctx, env)

	default:
		return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "unknown control action: %s", action)
	}
}

// SendControlToSession sends a server-originated control message to the client.
// Used for reconnect, session_invalid, and throttle notifications.
func (h *Handler) SendControlToSession(ctx context.Context, sessionID string, action events.ControlAction, reason string, details map[string]any) error {
	env := events.NewEnvelope(aep.NewID(), sessionID, h.hub.NextSeq(sessionID), events.Control, events.ControlData{
		Action:  action,
		Reason:  reason,
		Details: details,
	})
	env.Priority = events.PriorityControl // control messages bypass backpressure
	return h.hub.SendToSession(ctx, env)
}

// SendReconnect sends a reconnect control message to the client.
func (h *Handler) SendReconnect(ctx context.Context, sessionID, reason string, delayMs int) error {
	return h.SendControlToSession(ctx, sessionID, events.ControlActionReconnect, reason, map[string]any{
		"delay_ms": delayMs,
	})
}

// SendSessionInvalid sends a session_invalid control message to the client.
func (h *Handler) SendSessionInvalid(ctx context.Context, sessionID, reason string, recoverable bool) error {
	return h.SendControlToSession(ctx, sessionID, events.ControlActionSessionInvalid, reason, map[string]any{
		"recoverable": recoverable,
	})
}

// SendThrottle sends a throttle control message to the client.
func (h *Handler) SendThrottle(ctx context.Context, sessionID string, backoffMs, maxMessageRate int) error {
	return h.SendControlToSession(ctx, sessionID, events.ControlActionThrottle, "rate limit exceeded", map[string]any{
		"suggestion": map[string]any{
			"max_message_rate": maxMessageRate,
		},
		"backoff_ms":  backoffMs,
		"retry_after": backoffMs,
	})
}

func (h *Handler) handleReset(ctx context.Context, env *events.Envelope) error {
	si, err := h.requireActiveOwner(ctx, env)
	if err != nil {
		return err
	}
	if !si.State.IsActive() {
		return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "reset not allowed in state: %s", si.State)
	}

	// Delegate to Bridge for full lifecycle: intentional exit flag,
	// worker Terminate → delete files → Start, and new forwardEvents goroutine.
	if h.bridge != nil {
		if err := h.bridge.ResetSession(ctx, env.SessionID); err != nil {
			return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "reset failed: %v", err)
		}
	} else {
		// Test mode (no bridge): reset worker directly.
		w := h.sm.GetWorker(env.SessionID)
		if w != nil {
			if err := w.ResetContext(ctx); err != nil {
				return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "worker reset failed: %v", err)
			}
		}
	}

	// Idempotent: if already running, the state transition is a no-op.
	if si.State != events.StateRunning {
		if err := h.sm.TransitionWithReason(ctx, env.SessionID, events.StateRunning, "reset"); err != nil {
			return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "reset transition failed: %v", err)
		}
	}

	stateEvt := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.State, events.StateData{
		State:   events.StateRunning,
		Message: "context_reset",
	})
	if err := h.hub.SendToSession(ctx, stateEvt); err != nil {
		h.log.Warn("gateway: state notification send failed", "session_id", env.SessionID, "err", err)
	}

	h.log.Info("gateway: session reset", "session_id", env.SessionID)
	return nil
}

func (h *Handler) handleGC(ctx context.Context, env *events.Envelope) error {
	si, err := h.requireActiveOwner(ctx, env)
	if err != nil {
		return err
	}
	if si.State == events.StateTerminated {
		h.log.Info("gateway: gc idempotent (already terminated)", "session_id", env.SessionID)
		return nil
	}

	if w := h.sm.GetWorker(env.SessionID); w != nil {
		if err := w.Terminate(ctx); err != nil {
			h.log.Warn("gateway: gc worker terminate failed", "session_id", env.SessionID, "err", err)
		}
		h.sm.DetachWorker(env.SessionID)
	}

	// Re-read after worker cleanup to avoid stale-snapshot race with concurrent
	// cleanupCrashedWorker transitions.
	if fresh, err := h.sm.Get(ctx, env.SessionID); err == nil && fresh.State == events.StateTerminated {
		h.log.Info("gateway: gc idempotent (concurrently terminated)", "session_id", env.SessionID)
		return nil
	} else if err != nil {
		h.log.Debug("gateway: gc re-read failed, proceeding", "session_id", env.SessionID, "err", err)
	}

	if err := h.sm.TransitionWithReason(ctx, env.SessionID, events.StateTerminated, "gc"); err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "gc transition failed: %v", err)
	}

	stateEvt := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.State, events.StateData{
		State:   events.StateTerminated,
		Message: "session_archived",
	})
	if err := h.hub.SendToSession(ctx, stateEvt); err != nil {
		h.log.Warn("gateway: state notification send failed", "session_id", env.SessionID, "err", err)
	}

	h.log.Info("gateway: session gc'd", "session_id", env.SessionID)
	return nil
}

func (h *Handler) handleCD(ctx context.Context, env *events.Envelope) error {
	d, ok := events.DecodeAs[events.ControlData](env.Event.Data)
	var path string
	if ok && d.Details != nil {
		path, _ = d.Details["path"].(string)
	}

	// /cd with no arg: return current workDir.
	if path == "" {
		si, err := h.sm.Get(ctx, env.SessionID)
		if err != nil {
			return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
		}
		workDir := si.WorkDir
		msgEnv := events.NewEnvelope(
			aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID),
			events.Message, events.MessageData{Content: fmt.Sprintf("📂 当前工作目录: %s", workDir)},
		)
		return h.hub.SendToSession(ctx, msgEnv)
	}

	// Expand ~ and resolve to absolute path.
	expanded, err := config.ExpandAndAbs(path)
	if err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeConfigInvalid, "invalid path: %v", err)
	}
	path = expanded

	// Validate ownership.
	if _, err := h.requireActiveOwner(ctx, env); err != nil {
		return err
	}

	// Delegate to bridge.
	if h.bridge == nil {
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "cd not available")
	}

	result, err := h.bridge.SwitchWorkDir(ctx, env.SessionID, path)
	if err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "切换失败：%v", err)
	}

	// Send notification on the OLD session ID so the platform conn (still
	// registered with the old session) receives it. The next message will
	// derive the new session ID from the updated conn workDir.
	var msg string
	if result.Resumed {
		msg = fmt.Sprintf("📂 已切换到 %s（已恢复会话）", result.WorkDir)
	} else {
		msg = fmt.Sprintf("📂 已切换到 %s（新会话，上下文已重置）", result.WorkDir)
	}
	msgEnv := events.NewEnvelope(
		aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID),
		events.Message, events.MessageData{Content: msg},
	)
	if err := h.hub.SendToSession(ctx, msgEnv); err != nil {
		h.log.Warn("gateway: state notification send failed", "session_id", env.SessionID, "err", err)
	}

	_ = result // silence lint for now; result.WorkDir used above
	return nil
}
