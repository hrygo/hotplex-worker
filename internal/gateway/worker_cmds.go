package gateway

import (
	"context"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

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

	si, err := h.requireActiveOwner(ctx, env)
	if err != nil {
		return err
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

	cr, ok := w.(worker.ControlRequester)
	if !ok {
		return h.sendErrorf(ctx, env, events.ErrCodeNotSupported, "worker type does not support control requests")
	}

	// Control requests need an independent timeout so they don't block for
	// the full caller context (e.g. Feishu chatQueue's 10-minute deadline).
	ctrlCtx, ctrlCancel := context.WithTimeout(ctx, 60*time.Second)
	defer ctrlCancel()

	switch cmd {
	case events.StdioSkills:
		return h.handleSkillsList(ctx, env, args)

	case events.StdioContextUsage:
		resp, err := cr.SendControlRequest(ctrlCtx, "get_context_usage", nil)
		if err != nil {
			code := classifyWorkerError(err)
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
		resp, err := cr.SendControlRequest(ctrlCtx, "mcp_status", nil)
		if err != nil {
			code := classifyWorkerError(err)
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
		_, err := cr.SendControlRequest(ctrlCtx, "set_model", map[string]any{"model": modelName})
		if err != nil {
			code := classifyWorkerError(err)
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
		_, err := cr.SendControlRequest(ctrlCtx, "set_permission_mode", map[string]any{"mode": mode})
		if err != nil {
			code := classifyWorkerError(err)
			return h.sendErrorf(ctx, env, code, "set permission: %v", err)
		}

	default:
		return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "unknown worker command: %s", cmd)
	}
	return nil
}

func (h *Handler) handlePassthroughCommand(ctx context.Context, env *events.Envelope, w worker.Worker, cmd events.WorkerStdioCommand, args string) error {
	if commander, ok := w.(worker.WorkerCommander); ok {
		switch cmd {
		case events.StdioCompact:
			if err := commander.Compact(ctx, nil); err != nil {
				return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "compact: %v", err)
			}
			h.sendCommandFeedback(ctx, env.SessionID, "✅ 对话历史已压缩")
			return nil
		case events.StdioClear:
			if err := commander.Clear(ctx); err != nil {
				return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "clear: %v", err)
			}
			h.sendCommandFeedback(ctx, env.SessionID, "✅ 会话已清空，新会话已创建")
			return nil
		case events.StdioRewind:
			if err := commander.Rewind(ctx, ""); err != nil {
				return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "rewind: %v", err)
			}
			h.sendCommandFeedback(ctx, env.SessionID, "✅ 已回退到上一轮对话")
			return nil
		case events.StdioEffort, events.StdioCommit:
			return h.sendErrorf(ctx, env, events.ErrCodeNotSupported, "%s not supported by this worker type", cmd)
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

func (h *Handler) sendCommandFeedback(ctx context.Context, sessionID, msg string) {
	env := events.NewEnvelope(
		aep.NewID(), sessionID, h.hub.NextSeq(sessionID),
		events.Message, events.MessageData{Content: msg},
	)
	_ = h.hub.SendToSession(ctx, env)
}

func (h *Handler) handleSkillsList(ctx context.Context, env *events.Envelope, filter string) error {
	if h.skillsLocator == nil {
		return h.sendErrorf(ctx, env, events.ErrCodeNotSupported, "skills listing not available")
	}

	si, err := h.sm.Get(ctx, env.SessionID)
	if err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
	}

	allSkills, err := h.skillsLocator.List(ctx, "", si.WorkDir)
	if err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "skills: %v", err)
	}

	filter = strings.TrimSpace(filter)
	if filter != "" {
		filtered := make([]skills.Skill, 0, len(allSkills))
		lower := strings.ToLower(filter)
		for _, s := range allSkills {
			if strings.Contains(strings.ToLower(s.Name), lower) ||
				strings.Contains(strings.ToLower(s.Description), lower) {
				filtered = append(filtered, s)
			}
		}
		allSkills = filtered
	}

	entries := make([]events.SkillEntry, len(allSkills))
	for i, s := range allSkills {
		entries[i] = events.SkillEntry{
			Name:        s.Name,
			Description: s.Description,
			Source:      s.Source,
		}
	}

	data := events.SkillsListData{Skills: entries, Total: len(entries), Filter: filter}
	respEnv := events.NewEnvelope(
		aep.NewID(), env.SessionID,
		h.hub.NextSeq(env.SessionID),
		events.SkillsList, data,
	)
	return h.hub.SendToSession(ctx, respEnv)
}
