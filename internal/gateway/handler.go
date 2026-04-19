package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hotplex/hotplex-worker/internal/config"
	"github.com/hotplex/hotplex-worker/internal/metrics"
	"github.com/hotplex/hotplex-worker/internal/security"
	"github.com/hotplex/hotplex-worker/internal/session"
	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// ─── Message Handler ─────────────────────────────────────────────────────────

// Handler processes incoming messages from a client connection.
// It coordinates between the hub, session manager, and pool.
type Handler struct {
	log          *slog.Logger
	cfg          *config.Config
	hub          *Hub
	sm           *session.Manager
	jwtValidator *security.JWTValidator
}

// NewHandler creates a new message handler.
func NewHandler(log *slog.Logger, cfg *config.Config, hub *Hub, sm *session.Manager, jwtValidator *security.JWTValidator) *Handler {
	return &Handler{
		log:          log,
		cfg:          cfg,
		hub:          hub,
		sm:           sm,
		jwtValidator: jwtValidator,
	}
}

// Handle processes an incoming envelope from a client.
func (h *Handler) Handle(ctx context.Context, env *events.Envelope) error {
	h.log.Debug("gateway: Handle called", "event_type", env.Event.Type, "session_id", env.SessionID, "seq", env.Seq)
	switch env.Event.Type {
	case events.Input:
		return h.handleInput(ctx, env)
	case events.Ping:
		return h.handlePing(ctx, env)
	case events.Control:
		return h.handleControl(ctx, env)
	// AEP-011 / AEP-012: pass-through events from worker to all session clients.
	case events.Reasoning, events.Step, events.PermissionRequest, events.PermissionResponse,
		events.QuestionRequest, events.QuestionResponse,
		events.ElicitationRequest, events.ElicitationResponse,
		events.Message, events.MessageStart, events.MessageEnd:
		return h.passthroughToSession(ctx, env)
	default:
		return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "unknown event type: %s", env.Event.Type)
	}
}

func (h *Handler) handleInput(ctx context.Context, env *events.Envelope) error {
	h.log.Debug("gateway: handleInput called", "session_id", env.SessionID, "seq", env.Seq)

	data, ok := env.Event.Data.(map[string]any)
	if !ok {
		h.log.Warn("gateway: handleInput malformed data", "session_id", env.SessionID)
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "malformed input data")
	}

	content, _ := data["content"].(string)
	h.log.Debug("gateway: handleInput content received", "session_id", env.SessionID, "content_len", len(content))

	// Check SESSION_BUSY: session must be active.
	si, err := h.sm.Get(env.SessionID)
	if err != nil {
		h.log.Warn("gateway: handleInput session not found", "session_id", env.SessionID, "err", err)
		return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
	}

	h.log.Debug("gateway: handleInput session state", "session_id", env.SessionID, "state", si.State)

	if !si.State.IsActive() {
		h.log.Warn("gateway: handleInput session not active", "session_id", env.SessionID, "state", si.State)
		return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session not active: %s", si.State)
	}

	// Atomic transition + input. Only needed for IDLE → RUNNING (not CREATED → RUNNING,
	// which is handled by Bridge.StartSession in performInit). This covers the resume case.
	if si.State == events.StateIdle {
		if err := h.sm.TransitionWithInput(ctx, env.SessionID, events.StateRunning, content, nil); err != nil {
			h.log.Warn("gateway: handleInput transition failed", "session_id", env.SessionID, "err", err)
			return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session busy: %v", err)
		}
	}

	// Deliver to worker.
	w := h.sm.GetWorker(env.SessionID)
	if w != nil {
		h.log.Debug("gateway: delivering input to worker", "session_id", env.SessionID, "content_preview", content)
		if err := w.Input(ctx, content, nil); err != nil {
			h.log.Warn("gateway: worker input", "err", err, "session_id", env.SessionID)
		} else {
			h.log.Info("gateway: input delivered to worker", "session_id", env.SessionID)
		}
	} else {
		h.log.Error("gateway: handleInput no worker found", "session_id", env.SessionID)
	}

	return nil
}

func (h *Handler) handlePing(ctx context.Context, env *events.Envelope) error {
	// Include current session state in pong (per AEP spec §11.4).
	si, err := h.sm.Get(env.SessionID)
	state := "unknown"
	if err == nil {
		state = string(si.State)
	}

	reply := events.NewEnvelope(
		aep.NewID(),
		env.SessionID,
		h.hub.NextSeq(env.SessionID),
		events.Pong,
		map[string]any{"state": state},
	)
	return h.hub.SendToSession(ctx, reply)
}

var passthroughMetricLabel = map[events.Kind]string{
	events.Reasoning:           "reasoning",
	events.Step:                "step",
	events.PermissionRequest:   "permission_request",
	events.PermissionResponse:  "permission_response",
	events.QuestionRequest:     "question_request",
	events.QuestionResponse:    "question_response",
	events.ElicitationRequest:  "elicitation_request",
	events.ElicitationResponse: "elicitation_response",
	events.Message:             "message",
	events.MessageStart:        "message.start",
	events.MessageEnd:          "message.end",
}

func (h *Handler) passthroughToSession(ctx context.Context, env *events.Envelope) error {
	if label, ok := passthroughMetricLabel[env.Event.Type]; ok {
		metrics.GatewayEventsTotal.WithLabelValues(label, "s2c").Inc()
	}
	return h.hub.SendToSession(ctx, env)
}

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
		// Send error + done to client.
		errEnv := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.Error, events.ErrorData{
			Code:    events.ErrCodeSessionTerminated,
			Message: "session terminated by client",
		})
		doneEnv := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.Done, events.DoneData{
			Success: false,
		})
		_ = h.hub.SendToSession(ctx, errEnv)
		_ = h.hub.SendToSession(ctx, doneEnv)
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

func (h *Handler) sendErrorf(ctx context.Context, env *events.Envelope, code events.ErrorCode, format string, args ...any) error {
	err := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.Error, events.ErrorData{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	})
	_ = h.hub.SendToSession(ctx, err) // best-effort; always return the error
	return fmt.Errorf("%s: %s", code, fmt.Sprintf(format, args...))
}

// validateOwner checks ownership and returns the session in one call.
// This avoids the double-fetch that calling ValidateOwnership then Get separately incurs.
func (h *Handler) validateOwner(_ context.Context, env *events.Envelope) (*session.SessionInfo, error) {
	si, err := h.sm.Get(env.SessionID)
	if err != nil {
		return nil, err
	}
	if si.UserID != env.OwnerID {
		return nil, fmt.Errorf("%w: owner mismatch", session.ErrOwnershipMismatch)
	}
	return si, nil
}

func (h *Handler) handleReset(ctx context.Context, env *events.Envelope) error {
	si, err := h.validateOwner(ctx, env)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
		}
		return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
	}
	if !si.State.IsActive() {
		return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "reset not allowed in state: %s", si.State)
	}

	if err := h.sm.ClearContext(ctx, env.SessionID); err != nil {
		h.log.Warn("gateway: reset clear context failed", "session_id", env.SessionID, "err", err)
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "clear context failed: %v", err)
	}

	w := h.sm.GetWorker(env.SessionID)
	if w != nil {
		if err := w.ResetContext(ctx); err != nil {
			h.log.Warn("gateway: worker reset context failed", "session_id", env.SessionID, "err", err)
			return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "worker reset failed: %v", err)
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
	_ = h.hub.SendToSession(ctx, stateEvt)

	h.log.Info("gateway: session reset", "session_id", env.SessionID)
	return nil
}

func (h *Handler) handleGC(ctx context.Context, env *events.Envelope) error {
	si, err := h.validateOwner(ctx, env)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
		}
		return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
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

	if err := h.sm.TransitionWithReason(ctx, env.SessionID, events.StateTerminated, "gc"); err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "gc transition failed: %v", err)
	}

	stateEvt := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.State, events.StateData{
		State:   events.StateTerminated,
		Message: "session_archived",
	})
	_ = h.hub.SendToSession(ctx, stateEvt)

	h.log.Info("gateway: session gc'd", "session_id", env.SessionID)
	return nil
}

// ─── Bridge ─────────────────────────────────────────────────────────────────

// SessionManager abstracts the session.Manager methods used by Bridge.
// It allows Bridge to be tested without a real Manager instance.
type SessionManager interface {
	CreateWithBot(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, platform string, platformKey map[string]string) (*session.SessionInfo, error)
	AttachWorker(id string, w worker.Worker) error
	DetachWorker(id string)
	Transition(ctx context.Context, id string, to events.SessionState) error
	Get(id string) (*session.SessionInfo, error)
	GetWorker(id string) worker.Worker
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, limit, offset int) ([]*session.SessionInfo, error)
	UpdateWorkerSessionID(ctx context.Context, id, workerSessionID string) error
}

// WorkerFactory creates worker instances. Production code uses defaultWorkerFactory.
type WorkerFactory interface {
	NewWorker(t worker.WorkerType) (worker.Worker, error)
}

type defaultWorkerFactory struct{}

func (defaultWorkerFactory) NewWorker(t worker.WorkerType) (worker.Worker, error) {
	return worker.NewWorker(t)
}
