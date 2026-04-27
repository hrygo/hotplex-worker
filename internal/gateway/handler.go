package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── Message Handler ─────────────────────────────────────────────────────────

// Handler processes incoming messages from a client connection.
// It coordinates between the hub, session manager, and pool.
type Handler struct {
	log          *slog.Logger
	hub          *Hub
	sm           *session.Manager
	jwtValidator *security.JWTValidator
	bridge       *Bridge // set via SetBridge; nil during tests
	convStore    session.ConversationStore
}

// NewHandler creates a new message handler.
func NewHandler(log *slog.Logger, hub *Hub, sm *session.Manager, jwtValidator *security.JWTValidator) *Handler {
	return &Handler{
		log:          log.With("component", "handler"),
		hub:          hub,
		sm:           sm,
		jwtValidator: jwtValidator,
	}
}

// SetBridge injects the Bridge for lifecycle operations (reset).
// Must be called after NewHandler and NewBridge.
func (h *Handler) SetBridge(b *Bridge) { h.bridge = b }

// SetConvStore injects the conversation store for turn-level persistence.
func (h *Handler) SetConvStore(cs session.ConversationStore) { h.convStore = cs }

// Handle processes an incoming envelope from a client.
func (h *Handler) Handle(ctx context.Context, env *events.Envelope) (err error) {
	defer func() {
		if r := recover(); r != nil {
			h.log.Error("gateway: panic in handler", "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	switch env.Event.Type {
	case events.Input:
		return h.handleInput(ctx, env)
	case events.Ping:
		return h.handlePing(ctx, env)
	case events.Control:
		return h.handleControl(ctx, env)
	case events.WorkerCmd:
		return h.handleWorkerCommand(ctx, env)
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
	// Cancel pending auto-retry if user sends new input during backoff.
	if h.bridge != nil {
		h.bridge.CancelRetry(env.SessionID)
	}

	data, ok := env.Event.Data.(map[string]any)
	if !ok {
		h.log.Warn("gateway: handleInput malformed data", "session_id", env.SessionID)
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "malformed input data")
	}

	content, _ := data["content"].(string)

	// --- Command detection (parity with Slack/Feishu adapters) ---

	// Help command: reply directly without involving the worker.
	if messaging.IsHelpCommand(content) {
		helpEnv := events.NewEnvelope(
			aep.NewID(), env.SessionID,
			h.hub.NextSeq(env.SessionID),
			events.Message, events.MessageData{Content: messaging.HelpText()},
		)
		return h.hub.SendToSession(ctx, helpEnv)
	}

	// Control command: convert to AEP control event and dispatch.
	if result := messaging.ParseControlCommand(content); result != nil {
		data := events.ControlData{Action: result.Action}
		if result.Arg != "" {
			data.Details = map[string]any{"path": result.Arg}
		}
		ctrlEnv := &events.Envelope{
			Version:   events.Version,
			ID:        aep.NewID(),
			SessionID: env.SessionID,
			Seq:       h.hub.NextSeq(env.SessionID),
			Event: events.Event{
				Type: events.Control,
				Data: data,
			},
			OwnerID: env.OwnerID,
		}
		return h.handleControl(ctx, ctrlEnv)
	}

	// Worker command: convert to AEP worker_cmd event and dispatch.
	if cmdResult := messaging.ParseWorkerCommand(content); cmdResult != nil {
		wcmdEnv := &events.Envelope{
			Version:   events.Version,
			ID:        aep.NewID(),
			SessionID: env.SessionID,
			Seq:       h.hub.NextSeq(env.SessionID),
			Event: events.Event{
				Type: events.WorkerCmd,
				Data: events.WorkerCommandData{
					Command: cmdResult.Command,
					Args:    cmdResult.Args,
					Extra:   cmdResult.Extra,
				},
			},
			OwnerID: env.OwnerID,
		}
		return h.handleWorkerCommand(ctx, wcmdEnv)
	}

	// --- End command detection ---

	// Check SESSION_BUSY: session must be active.
	si, err := h.sm.Get(env.SessionID)
	if err != nil {
		h.log.Warn("gateway: handleInput session not found", "session_id", env.SessionID, "err", err)
		return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
	}

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
		if h.log.Enabled(ctx, slog.LevelDebug) {
			runes := []rune(content)
			preview := string(runes)
			if len(runes) > 32 {
				preview = string(runes[:32]) + "..."
			}
			h.log.Debug("gateway: delivering input to worker", "session_id", env.SessionID, "content_len", len(content), "preview", preview)
		}
		if err := w.Input(ctx, content, nil); err != nil {
			h.log.Warn("gateway: worker input", "err", err, "session_id", env.SessionID)
			_ = h.sendErrorf(ctx, env, events.ErrCodeInternalError, "worker input failed: %v", err)
		} else {
			h.log.Debug("gateway: input delivered to worker", "session_id", env.SessionID)
			// Record user input to conversation store (best-effort).
			if h.convStore != nil {
				_ = h.convStore.Append(ctx, &session.ConversationRecord{
					SessionID: env.SessionID,
					Seq:       env.Seq,
					Role:      session.RoleUser,
					Content:   content,
					Platform:  si.Platform,
					UserID:    env.OwnerID,
				})
			}
		}
	} else {
		h.log.Warn("gateway: handleInput no worker found", "session_id", env.SessionID)
		_ = h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "no worker attached to session")
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

func (h *Handler) handleCD(ctx context.Context, env *events.Envelope) error {
	// Extract path from control data.
	var path string
	switch d := env.Event.Data.(type) {
	case events.ControlData:
		if d.Details != nil {
			path, _ = d.Details["path"].(string)
		}
	case map[string]any:
		path, _ = d["path"].(string)
	}

	// /cd with no arg: return current workDir.
	if path == "" {
		si, err := h.sm.Get(env.SessionID)
		if err != nil {
			return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
		}
		workDir := si.WorkDir
		msgEnv := events.NewEnvelope(
			aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID),
			events.Message, events.MessageData{Content: workDir},
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
	if _, err := h.validateOwner(ctx, env); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
		}
		return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
	}

	// Delegate to bridge.
	if h.bridge == nil {
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "cd not available")
	}
	result, err := h.bridge.SwitchWorkDir(ctx, env.SessionID, path)
	if err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "切换失败：%v", err)
	}

	msg := fmt.Sprintf("已切换到 %s（新会话 %s）", result.WorkDir, result.NewSessionID)
	msgEnv := events.NewEnvelope(
		aep.NewID(), result.NewSessionID, h.hub.NextSeq(result.NewSessionID),
		events.Message, events.MessageData{Content: msg},
	)
	_ = h.hub.SendToSession(ctx, msgEnv)

	return nil
}

// ControlRequester is implemented by workers that support structured control queries.
type ControlRequester interface {
	SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error)
}

// WorkerCommander is implemented by workers that support worker-level commands
// beyond the basic Input() passthrough.
type WorkerCommander interface {
	Compact(ctx context.Context, args map[string]any) error
	Clear(ctx context.Context) error
	Rewind(ctx context.Context, targetID string) error
}

func (h *Handler) handleWorkerCommand(ctx context.Context, env *events.Envelope) error {
	var cmd events.WorkerStdioCommand
	var args string
	var extra map[string]any

	switch d := env.Event.Data.(type) {
	case events.WorkerCommandData:
		cmd = d.Command
		args = d.Args
		extra = d.Extra
	case map[string]any:
		c, _ := d["command"].(string)
		cmd = events.WorkerStdioCommand(c)
		args, _ = d["args"].(string)
		if e, ok := d["extra"].(map[string]any); ok {
			extra = e
		}
	default:
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "worker_command: invalid data")
	}

	si, err := h.validateOwner(ctx, env)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
		}
		return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
	}
	if !si.State.IsActive() {
		return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "worker command requires active session, current: %s", si.State)
	}

	w := h.sm.GetWorker(env.SessionID)
	if w == nil {
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "no worker attached")
	}

	if cmd.IsPassthrough() {
		return h.handlePassthroughCommand(ctx, env, w, cmd, args)
	}

	cr, ok := w.(ControlRequester)
	if !ok {
		return h.sendErrorf(ctx, env, events.ErrCodeNotSupported, "worker type does not support control requests")
	}

	switch cmd {
	case events.StdioContextUsage, events.StdioSkills:
		resp, err := cr.SendControlRequest(ctx, "get_context_usage", nil)
		if err != nil {
			code := events.ErrCodeInternalError
			if strings.Contains(err.Error(), "not running") || strings.Contains(err.Error(), "closed") {
				code = events.ErrCodeSessionTerminated
			}
			return h.sendErrorf(ctx, env, code, "context query: %v", err)
		}
		data := events.MapContextUsageResponse(resp)
		respEnv := events.NewEnvelope(
			aep.NewID(), env.SessionID,
			h.hub.NextSeq(env.SessionID),
			events.ContextUsage, data,
		)
		return h.hub.SendToSession(ctx, respEnv)

	case events.StdioMCPStatus:
		resp, err := cr.SendControlRequest(ctx, "mcp_status", nil)
		if err != nil {
			code := events.ErrCodeInternalError
			if strings.Contains(err.Error(), "not running") || strings.Contains(err.Error(), "closed") {
				code = events.ErrCodeSessionTerminated
			}
			return h.sendErrorf(ctx, env, code, "mcp status: %v", err)
		}
		data := events.MapMCPStatusResponse(resp)
		respEnv := events.NewEnvelope(
			aep.NewID(), env.SessionID,
			h.hub.NextSeq(env.SessionID),
			events.MCPStatus, data,
		)
		return h.hub.SendToSession(ctx, respEnv)

	case events.StdioSetModel:
		modelName := args
		if modelName == "" {
			modelName, _ = extra["model"].(string)
		}
		if modelName == "" {
			return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "model name required")
		}
		_, err := cr.SendControlRequest(ctx, "set_model", map[string]any{"model": modelName})
		if err != nil {
			code := events.ErrCodeInternalError
			if strings.Contains(err.Error(), "not running") || strings.Contains(err.Error(), "closed") {
				code = events.ErrCodeSessionTerminated
			}
			return h.sendErrorf(ctx, env, code, "set model: %v", err)
		}

	case events.StdioSetPermMode:
		mode := args
		if mode == "" {
			mode, _ = extra["mode"].(string)
		}
		if mode == "" {
			return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "permission mode required")
		}
		_, err := cr.SendControlRequest(ctx, "set_permission_mode", map[string]any{"mode": mode})
		if err != nil {
			code := events.ErrCodeInternalError
			if strings.Contains(err.Error(), "not running") || strings.Contains(err.Error(), "closed") {
				code = events.ErrCodeSessionTerminated
			}
			return h.sendErrorf(ctx, env, code, "set permission: %v", err)
		}

	default:
		return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "unknown worker command: %s", cmd)
	}
	return nil
}

func (h *Handler) handlePassthroughCommand(ctx context.Context, env *events.Envelope, w worker.Worker, cmd events.WorkerStdioCommand, args string) error {
	if commander, ok := w.(WorkerCommander); ok {
		switch cmd {
		case events.StdioCompact:
			if err := commander.Compact(ctx, nil); err != nil {
				return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "compact: %v", err)
			}
			return nil
		case events.StdioClear:
			if err := commander.Clear(ctx); err != nil {
				return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "clear: %v", err)
			}
			return nil
		case events.StdioRewind:
			if err := commander.Rewind(ctx, ""); err != nil {
				return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "rewind: %v", err)
			}
			return nil
		case events.StdioEffort:
			return h.sendErrorf(ctx, env, events.ErrCodeNotSupported, "effort not supported by this worker type")
		}
	}

	content := "/" + string(cmd)
	if args != "" {
		content += " " + args
	}
	if err := w.Input(ctx, content, nil); err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "%s: %v", cmd, err)
	}
	return nil
}

// ─── Bridge ─────────────────────────────────────────────────────────────────

// SessionManager abstracts the session.Manager methods used by Bridge.
// It allows Bridge to be tested without a real Manager instance.
type SessionManager interface {
	CreateWithBot(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, platform string, platformKey map[string]string, workDir, title string) (*session.SessionInfo, error)
	AttachWorker(id string, w worker.Worker) error
	DetachWorker(id string)
	DetachWorkerIf(id string, expected worker.Worker) bool
	Transition(ctx context.Context, id string, to events.SessionState) error
	Get(id string) (*session.SessionInfo, error)
	GetWorker(id string) worker.Worker
	Delete(ctx context.Context, id string) error
	DeletePhysical(ctx context.Context, id string) error
	List(ctx context.Context, userID, platform string, limit, offset int) ([]*session.SessionInfo, error)
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
