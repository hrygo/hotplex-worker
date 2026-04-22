package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

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

const statusMinInterval = 3 * time.Second

// threadState tracks per-thread status for dedup and rate limiting.
type threadState struct {
	lastText string
	lastTime time.Time
}

// StatusManager manages AI status notifications with dedup + rate limiting + thread safety.
// When using emoji fallback (non-Assistant API workspaces), it tracks the last
// emoji added per thread so Clear() can remove it.
type StatusManager struct {
	adapter *Adapter
	logger  *slog.Logger
	mu      sync.Mutex
	// per-thread tracking for emoji fallback cleanup.
	// Key: "channelID:threadTS" → last emoji added via setStatusWithEmojiFallback.
	emojiState map[string]string
	// per-thread dedup + rate limiting.
	// Key: "channelID:threadTS" → last text and timestamp.
	threadState map[string]*threadState
}

// NewStatusManager creates a new status manager.
func NewStatusManager(adapter *Adapter, logger *slog.Logger) *StatusManager {
	return &StatusManager{
		adapter:     adapter,
		logger:      logger,
		emojiState:  make(map[string]string),
		threadState: make(map[string]*threadState),
	}
}

// Notify sends a status update. Skips if text is identical to the last sent
// value, or if less than statusMinInterval has elapsed since the last update.
func (m *StatusManager) Notify(ctx context.Context, channelID, threadTS string, status StatusType, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if text == "" {
		m.clearEmojiLocked(ctx, channelID, threadTS)
		return nil
	}

	key := channelID + ":" + threadTS
	if ts := m.threadState[key]; ts != nil {
		if ts.lastText == text {
			return nil
		}
		if time.Since(ts.lastTime) < statusMinInterval {
			return nil
		}
	}

	if m.threadState[key] == nil {
		m.threadState[key] = &threadState{}
	}
	m.threadState[key].lastText = text
	m.threadState[key].lastTime = time.Now()

	return m.adapter.SetStatus(ctx, channelID, threadTS, status, text)
}

// Clear removes any tracked status emoji and state for the thread.
func (m *StatusManager) Clear(ctx context.Context, channelID, threadTS string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearEmojiLocked(ctx, channelID, threadTS)
	delete(m.threadState, channelID+":"+threadTS)
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
		return StatusToolUse, extractToolCallStatus(env)
	case events.ToolResult:
		return StatusToolResult, extractToolResultStatus(env)
	case events.MessageDelta:
		return StatusAnswering, "Composing response..."
	default:
		return "", ""
	}
}

// extractToolCallStatus formats "ToolName(key=val, key=val)" truncated to 50 chars.
func extractToolCallStatus(env *events.Envelope) string {
	name := "tool"
	var input map[string]any

	if env.Event.Data == nil {
		return name
	}
	if data, ok := env.Event.Data.(*events.ToolCallData); ok {
		if data.Name != "" {
			name = data.Name
		}
		input = data.Input
	} else if m, ok := env.Event.Data.(map[string]any); ok {
		if n, ok := m["name"].(string); ok && n != "" {
			name = n
		}
		if inp, ok := m["input"].(map[string]any); ok {
			input = inp
		}
	}

	if len(input) == 0 {
		return truncateStatus(name, 50)
	}

	parts := make([]string, 0, len(input))
	for k, v := range input {
		parts = append(parts, k+"="+truncateValue(fmt.Sprintf("%v", v), 20))
	}
	body := strings.Join(parts, ", ")
	return truncateStatus(name+"("+body+")", 50)
}

// extractToolResultStatus formats tool result preview truncated to 50 chars.
func extractToolResultStatus(env *events.Envelope) string {
	if env.Event.Data == nil {
		return "Tool completed"
	}

	if data, ok := env.Event.Data.(*events.ToolResultData); ok {
		if data.Error != "" {
			return truncateStatus("Error: "+data.Error, 50)
		}
		if data.Output != nil {
			return truncateStatus(fmt.Sprintf("%v", data.Output), 50)
		}
		return "Tool completed"
	}

	if m, ok := env.Event.Data.(map[string]any); ok {
		if errStr, ok := m["error"].(string); ok && errStr != "" {
			return truncateStatus("Error: "+errStr, 50)
		}
		if output, ok := m["output"]; ok && output != nil {
			return truncateStatus(fmt.Sprintf("%v", output), 50)
		}
	}

	return "Tool completed"
}

// truncateStatus truncates s to at most max bytes, appending "..." if truncated.
func truncateStatus(s string, max int) string {
	if max <= 3 {
		return s
	}
	if len(s) <= max {
		return s
	}
	// Find the last rune boundary that fits within max-3 bytes.
	cut := max - 3
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// truncateValue truncates a single parameter value for display.
func truncateValue(s string, max int) string {
	return truncateStatus(s, max)
}

func isRuneStart(b byte) bool {
	// UTF-8 continuation bytes start with 10xxxxxx (0x80-0xBF).
	// A rune start byte is anything that is NOT a continuation byte.
	return b&0xC0 != 0x80
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
