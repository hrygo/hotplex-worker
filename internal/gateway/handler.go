package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── Message Handler ─────────────────────────────────────────────────────────

// Handler processes incoming messages from a client connection.
// It coordinates between the hub, session manager, and pool.
type Handler struct {
	log           *slog.Logger
	hub           *Hub
	sm            SessionManager
	auth          *security.Authenticator
	jwtValidator  *security.JWTValidator
	bridge        *Bridge
	skillsLocator SkillsLocator
}

// SkillsLocator discovers skills from the filesystem.
type SkillsLocator interface {
	List(ctx context.Context, homeDir, workDir string) ([]skills.Skill, error)
	Close()
}

// NewHandler creates a new message handler.
func NewHandler(deps HandlerDeps) *Handler {
	return &Handler{
		log:           deps.Log.With("component", "handler"),
		hub:           deps.Hub,
		sm:            deps.SM,
		auth:          deps.Auth,
		jwtValidator:  deps.JWTValidator,
		bridge:        deps.Bridge,
		skillsLocator: deps.SkillsLocator,
	}
}

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

	// Interaction responses: deliver directly to worker, skipping command detection,
	// state transitions, and conversation store. Only route when metadata contains
	// a recognized interaction response key (not platform context metadata).
	if md, ok := data["metadata"].(map[string]any); ok {
		if md["permission_response"] != nil ||
			md["question_response"] != nil ||
			md["elicitation_response"] != nil {
			respType := "unknown"
			switch {
			case md["permission_response"] != nil:
				respType = "permission"
			case md["question_response"] != nil:
				respType = "question"
			case md["elicitation_response"] != nil:
				respType = "elicitation"
			}
			w := h.sm.GetWorker(env.SessionID)
			if w != nil {
				h.log.Info("gateway: routing interaction response",
					"type", respType,
					"session_id", env.SessionID)
				if err := w.Input(ctx, content, md); err != nil {
					h.log.Warn("gateway: worker interaction response failed",
						"err", err,
						"type", respType,
						"session_id", env.SessionID)
				} else if h.bridge != nil {
					h.bridge.CaptureInbound(env.SessionID, env.Seq, events.Input, env.Event.Data)
				}
			} else {
				h.log.Warn("gateway: interaction response dropped — no worker",
					"type", respType,
					"session_id", env.SessionID)
			}
			return nil
		}
	}

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
	si, err := h.sm.Get(ctx, env.SessionID)
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
			return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "worker input failed: %v", err)
		}
		h.log.Debug("gateway: input delivered to worker", "session_id", env.SessionID)
		// Capture inbound event for replay (best-effort).
		if h.bridge != nil {
			h.bridge.CaptureInbound(env.SessionID, env.Seq, events.Input, env.Event.Data)
		}
	} else {
		h.log.Warn("gateway: handleInput no worker found", "session_id", env.SessionID)
		return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "no worker attached to session")
	}

	return nil
}

func (h *Handler) handlePing(ctx context.Context, env *events.Envelope) error {
	// Include current session state in pong (per AEP spec §11.4).
	si, err := h.sm.Get(ctx, env.SessionID)
	state := "unknown"
	if err == nil {
		state = string(si.State)
	}

	reply := events.NewEnvelope(
		aep.NewID(),
		env.SessionID,
		0, // P2: pong should not consume seq
		events.Pong,
		map[string]any{"state": state},
	)
	if h.log.Enabled(ctx, slog.LevelDebug) {
		h.log.Debug("gateway: ping received, sending pong", "session_id", env.SessionID, "state", state)
	}
	err = h.hub.SendToSession(ctx, reply)
	if err != nil {
		h.log.Warn("gateway: pong send failed", "session_id", env.SessionID, "err", err)
	}
	return err
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

// validateOwner checks ownership and returns the session in one call.
// This avoids the double-fetch that calling ValidateOwnership then Get separately incurses.
func (h *Handler) validateOwner(ctx context.Context, env *events.Envelope) (*session.SessionInfo, error) {
	si, err := h.sm.Get(ctx, env.SessionID)
	if err != nil {
		return nil, err
	}
	if si.UserID != env.OwnerID {
		return nil, fmt.Errorf("%w: owner mismatch", session.ErrOwnershipMismatch)
	}
	return si, nil
}

// requireActiveOwner validates session ownership and returns the session info.
// On error it sends an appropriate error to the client and returns the error
// so the caller can simply do: si, err := h.requireActiveOwner(ctx, env); if err != nil { return err }
func (h *Handler) requireActiveOwner(ctx context.Context, env *events.Envelope) (*session.SessionInfo, error) {
	si, err := h.validateOwner(ctx, env)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return nil, h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
		}
		return nil, h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
	}
	return si, nil
}

// ─── Bridge ─────────────────────────────────────────────────────────────────

// SessionReader provides read-only session access.
type SessionReader interface {
	Get(ctx context.Context, id string) (*session.SessionInfo, error)
	GetWorker(id string) worker.Worker
}

// SessionLifecycle provides session creation and deletion.
type SessionLifecycle interface {
	CreateWithBot(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, platform string, platformKey map[string]string, workDir, title string) (*session.SessionInfo, error)
	Delete(ctx context.Context, id string) error
	DeletePhysical(ctx context.Context, id string) error
}

// SessionTransitioner provides state transition operations.
type SessionTransitioner interface {
	Transition(ctx context.Context, id string, to events.SessionState) error
	TransitionWithInput(ctx context.Context, id string, to events.SessionState, content string, metadata map[string]any) error
	TransitionWithReason(ctx context.Context, id string, to events.SessionState, termReason string) error
}

// SessionWorkerManager provides worker attachment and detachment.
type SessionWorkerManager interface {
	AttachWorker(id string, w worker.Worker) error
	DetachWorker(id string)
	DetachWorkerIf(id string, expected worker.Worker) bool
	UpdateWorkerSessionID(ctx context.Context, id, workerSessionID string) error
}

// SessionAdmin provides listing, ownership validation, and metadata mutations.
type SessionAdmin interface {
	List(ctx context.Context, userID, platform string, limit, offset int) ([]*session.SessionInfo, error)
	ValidateOwnership(ctx context.Context, sessionID, userID, adminUserID string) error
	UpdateWorkDir(ctx context.Context, id, workDir string) error
	ResetExpiry(ctx context.Context, id string) error
}

// SessionManager composes all session sub-interfaces for full management.
type SessionManager interface {
	SessionReader
	SessionLifecycle
	SessionTransitioner
	SessionWorkerManager
	SessionAdmin
}

// WorkerFactory creates worker instances. Production code uses defaultWorkerFactory.
type WorkerFactory interface {
	NewWorker(t worker.WorkerType) (worker.Worker, error)
}

type defaultWorkerFactory struct{}

func (defaultWorkerFactory) NewWorker(t worker.WorkerType) (worker.Worker, error) {
	return worker.NewWorker(t)
}
