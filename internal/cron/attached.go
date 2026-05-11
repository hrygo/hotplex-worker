package cron

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/pkg/events"
)

// AttachedSessionRouter is the narrow interface for session callback execution.
// Implemented by an adapter in cmd/hotplex/ that bridges Bridge + SessionManager.
type AttachedSessionRouter interface {
	// GetSessionInfo returns session metadata for callback dispatch.
	GetSessionInfo(ctx context.Context, id string) (*session.SessionInfo, error)

	// ResumeAndInput resumes a dormant session and injects the callback prompt.
	ResumeAndInput(ctx context.Context, sessionID string, workDir string, prompt string, metadata map[string]any) error

	// InjectInput sends a prompt to an already-running session's worker.
	InjectInput(ctx context.Context, sessionID string, prompt string, metadata map[string]any) error
}

// AttachedSessionHandler dispatches callback prompts into existing sessions.
type AttachedSessionHandler struct {
	log    *slog.Logger
	router AttachedSessionRouter
}

// NewAttachedSessionHandler creates a new callback handler.
func NewAttachedSessionHandler(log *slog.Logger, router AttachedSessionRouter) *AttachedSessionHandler {
	return &AttachedSessionHandler{
		log:    log.With("component", "cron_attached"),
		router: router,
	}
}

// Execute dispatches a callback into the target session.
// Returns nil on successful injection (fire-and-forget), or an error if dispatch fails.
func (h *AttachedSessionHandler) Execute(ctx context.Context, job *CronJob) error {
	sid := job.Payload.TargetSessionID

	info, err := h.router.GetSessionInfo(ctx, sid)
	if err != nil {
		metrics.CronAttachedTotal.WithLabelValues("session_not_found").Inc()
		return fmt.Errorf("callback: session %s not found: %w", sid, err)
	}

	prompt := fmt.Sprintf("[cron:%s %s] %s\n%s",
		job.ID, job.Name, job.Payload.Message, time.Now().Format(time.RFC3339))
	metadata := map[string]any{
		"source":   "cron_attached",
		"cron_job": job.ID,
	}

	switch info.State {
	case events.StateRunning:
		if err := h.router.InjectInput(ctx, sid, prompt, metadata); err != nil {
			metrics.CronAttachedTotal.WithLabelValues("inject_failed").Inc()
			return fmt.Errorf("callback: inject into running session: %w", err)
		}
		h.log.Info("callback: injected into running session",
			"session_id", sid, "job_id", job.ID)

	case events.StateIdle, events.StateTerminated:
		if err := h.router.ResumeAndInput(ctx, sid, info.WorkDir, prompt, metadata); err != nil {
			metrics.CronAttachedTotal.WithLabelValues("resume_failed").Inc()
			return fmt.Errorf("callback: resume session %s: %w", sid, err)
		}
		h.log.Info("callback: resumed and injected",
			"session_id", sid, "job_id", job.ID, "from_state", info.State)

	case events.StateDeleted:
		metrics.CronAttachedTotal.WithLabelValues("session_not_found").Inc()
		return fmt.Errorf("callback: session %s is deleted, aborting", sid)

	case events.StateCreated:
		metrics.CronAttachedTotal.WithLabelValues("session_not_found").Inc()
		return fmt.Errorf("callback: session %s is in CREATED state (never started), aborting", sid)

	default:
		metrics.CronAttachedTotal.WithLabelValues("session_not_found").Inc()
		return fmt.Errorf("callback: session %s in unexpected state %s", sid, info.State)
	}

	metrics.CronAttachedTotal.WithLabelValues("success").Inc()
	return nil
}
