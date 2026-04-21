package slack

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/slack-go/slack"

	"github.com/hotplex/hotplex-worker/pkg/events"
)

// StatusType represents the current AI processing phase.
type StatusType string

const (
	StatusInitializing StatusType = "initializing"
	StatusThinking     StatusType = "thinking"
	StatusToolUse      StatusType = "tool_use"
	StatusToolResult   StatusType = "tool_result"
	StatusAnswering    StatusType = "answering"
	StatusStepFinish   StatusType = "step_finish"
	StatusIdle         StatusType = "idle"
)

// StatusEmojiMap maps StatusType to Slack emoji name for fallback.
var StatusEmojiMap = map[StatusType]string{
	StatusInitializing: "hourglass_flowing_sand",
	StatusThinking:     "brain",
	StatusToolUse:      "gear",
	StatusToolResult:   "wrench",
	StatusAnswering:    "pencil",
	StatusStepFinish:   "white_check_mark",
	StatusIdle:         "white_circle",
}

// StatusTextMap maps StatusType to human-readable status text.
var StatusTextMap = map[StatusType]string{
	StatusInitializing: "Initializing...",
	StatusThinking:     "Thinking...",
	StatusToolResult:   "Tool completed",
	StatusAnswering:    "Composing response...",
	StatusStepFinish:   "Step complete",
}

// StatusManager manages AI status notifications with dedup + thread safety.
// When using emoji fallback (non-Assistant API workspaces), it tracks the last
// emoji added per thread so Clear() can remove it.
type StatusManager struct {
	adapter *Adapter
	logger  *slog.Logger
	mu      sync.Mutex
	// per-thread tracking for emoji fallback cleanup.
	// Key: "channelID:threadTS" → last emoji added via setStatusWithEmojiFallback.
	emojiState map[string]string
}

// NewStatusManager creates a new status manager.
func NewStatusManager(adapter *Adapter, logger *slog.Logger) *StatusManager {
	return &StatusManager{
		adapter:    adapter,
		logger:     logger,
		emojiState: make(map[string]string),
	}
}

// Notify sends a status update; skips if status unchanged for the same thread.
func (m *StatusManager) Notify(ctx context.Context, channelID, threadTS string, status StatusType, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if text == "" {
		m.clearEmojiLocked(ctx, channelID, threadTS)
		return nil
	}
	return m.adapter.SetStatus(ctx, channelID, threadTS, status, text)
}

// Clear removes any tracked status emoji for the thread.
func (m *StatusManager) Clear(ctx context.Context, channelID, threadTS string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearEmojiLocked(ctx, channelID, threadTS)
}

// clearEmojiLocked removes the tracked emoji reaction. Caller must hold m.mu.
func (m *StatusManager) clearEmojiLocked(ctx context.Context, channelID, threadTS string) {
	key := channelID + ":" + threadTS
	emoji := m.emojiState[key]
	if emoji == "" {
		return
	}
	delete(m.emojiState, key)

	if m.adapter != nil && m.adapter.client != nil && threadTS != "" {
		_ = m.adapter.client.RemoveReactionContext(ctx, emoji, slack.ItemRef{
			Channel:   channelID,
			Timestamp: threadTS,
		})
	}
}

// trackEmoji records the emoji added for a thread. Called by setStatusWithEmojiFallback.
func (m *StatusManager) trackEmoji(channelID, threadTS, emoji string) {
	key := channelID + ":" + threadTS
	m.emojiState[key] = emoji
}

// aepEventToStatus maps an AEP envelope to a status type and text.
func aepEventToStatus(env *events.Envelope) (StatusType, string) {
	switch env.Event.Type {
	case events.ToolCall:
		toolName := extractToolName(env)
		return StatusToolUse, "Using " + toolName + "..."
	case events.ToolResult:
		return StatusToolResult, "Tool completed"
	case events.MessageDelta:
		return StatusAnswering, "Composing response..."
	default:
		return "", ""
	}
}

// extractToolName extracts the tool name from an AEP ToolCall envelope.
func extractToolName(env *events.Envelope) string {
	if env.Event.Data == nil {
		return "tool"
	}
	if data, ok := env.Event.Data.(*events.ToolCallData); ok && data.Name != "" {
		return data.Name
	}
	if m, ok := env.Event.Data.(map[string]any); ok {
		if name, ok := m["name"].(string); ok {
			return name
		}
	}
	return "tool"
}

// SetAssistantStatus sets the native assistant status text via Slack API.
func (a *Adapter) SetAssistantStatus(ctx context.Context, channelID, threadTS, status string) error {
	if a.client == nil || threadTS == "" {
		return nil
	}

	params := slack.AssistantThreadsSetStatusParameters{
		ChannelID: channelID,
		ThreadTS:  threadTS,
		Status:    status,
	}

	return a.client.SetAssistantThreadsStatusContext(ctx, params)
}

// SetStatus sets the AI status. Uses Assistant API when available;
// reaction emoji is a fallback only when it is not. The two paths are
// mutually exclusive: one or the other, never both.
func (a *Adapter) SetStatus(ctx context.Context, channelID, threadTS string, status StatusType, text string) error {
	if a.client == nil {
		return nil
	}
	if text == "" {
		return a.ClearStatus(ctx, channelID, threadTS)
	}
	if a.isAssistantCapable.Load() {
		err := a.SetAssistantStatus(ctx, channelID, threadTS, text)
		if err == nil {
			return nil
		}
		a.handleCapabilityError(err)
	}
	return a.setStatusWithEmojiFallback(ctx, channelID, threadTS, status)
}

// ClearStatus clears the AI status. Uses Assistant API when capable,
// or removes the reaction emoji when it is not. The two paths are
// mutually exclusive to avoid unnecessary API calls.
func (a *Adapter) ClearStatus(ctx context.Context, channelID, threadTS string) error {
	if a.client == nil {
		return nil
	}
	if a.isAssistantCapable.Load() {
		err := a.SetAssistantStatus(ctx, channelID, threadTS, "")
		if err == nil {
			return nil
		}
		a.handleCapabilityError(err)
	}
	a.statusMgr.Clear(ctx, channelID, threadTS)
	return nil
}

func (a *Adapter) handleCapabilityError(err error) {
	if isAssistantCapabilityError(err) {
		a.log.Warn("slack: Assistant API no longer available, switching to emoji fallback",
			"err", err)
		a.isAssistantCapable.Store(false)
	} else {
		a.log.Debug("slack: Assistant API call failed, trying emoji fallback",
			"err", err)
	}
}

func (a *Adapter) setStatusWithEmojiFallback(ctx context.Context, channelID, threadTS string, status StatusType) error {
	emoji, ok := StatusEmojiMap[status]
	if !ok || emoji == "" || threadTS == "" {
		return nil
	}
	err := a.client.AddReactionContext(ctx, emoji, slack.ItemRef{
		Channel:   channelID,
		Timestamp: threadTS,
	})
	if err == nil {
		a.statusMgr.trackEmoji(channelID, threadTS, emoji)
	}
	return err
}

// ProbeAssistantCapability tests if the workspace supports the Assistant API.
func (a *Adapter) ProbeAssistantCapability(ctx context.Context) bool {
	if !a.assistantAPIEnabled() {
		return false
	}
	if a.client == nil {
		return false
	}
	params := slack.AssistantThreadsSetStatusParameters{Status: ""}
	err := a.client.SetAssistantThreadsStatusContext(ctx, params)
	if err != nil {
		if isAssistantCapabilityError(err) {
			a.log.Warn("slack: Assistant API not available (free workspace?), falling back to emoji reactions",
				"err", err)
			return false
		}
		// Transient or benign error (e.g. channel_not_found from empty params): treat as capable
		a.log.Info("slack: Assistant API probe skipped (benign error), assuming capable",
			"err", err)
		return true
	}
	return true
}

func (a *Adapter) assistantAPIEnabled() bool {
	return a.assistantEnabled == nil || *a.assistantEnabled
}

func isAssistantCapabilityError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "not_allowed") ||
		strings.Contains(errStr, "not_allowed_token_type")
}
