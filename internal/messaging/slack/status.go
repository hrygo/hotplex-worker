package slack

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/slack-go/slack"

	"github.com/hrygo/hotplex/pkg/events"
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

const (
	statusMinInterval = 3 * time.Second
	threadStateTTL    = 1 * time.Hour
)

// threadState tracks per-thread status for dedup and rate limiting.
type threadState struct {
	lastText string
	lastTime time.Time
	lastTool string
}

// StatusManager manages AI status notifications with dedup + rate limiting + thread safety.
// When using emoji fallback (non-Assistant API workspaces), it tracks the last
// emoji added per thread so Clear() can remove it.
type StatusManager struct {
	adapter *Adapter
	logger  *slog.Logger
	mu      sync.Mutex
	// emojiOnly is set once during probe: true means workspace has no Assistant API,
	// so Notify/Clear go directly to emoji reactions without checking isAssistantCapable.
	emojiOnly   atomic.Bool
	emojiState  map[string]string
	threadState map[string]*threadState
	// unregLogged tracks tool names already logged as unregistered (once-per-name dedup).
	unregLogged sync.Map
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

// SetEmojiOnly switches StatusManager to emoji-only mode.
// Called once after probe determines the workspace lacks Assistant API.
func (m *StatusManager) SetEmojiOnly(emojiOnly bool) {
	m.emojiOnly.Store(emojiOnly)
}

// LogOnceUnregistered logs a debug message the first time an unregistered tool name is seen.
func (m *StatusManager) LogOnceUnregistered(name string) {
	if _, loaded := m.unregLogged.LoadOrStore(name, true); loaded {
		return
	}
	m.logger.Debug("slack: unregistered tool in status formatter, consider adding to toolStatusFormatters", "tool", name)
}

// Notify sends a status update. Skips if text is identical to the last sent
// value, or if less than statusMinInterval has elapsed since the last update.
func (m *StatusManager) Notify(ctx context.Context, channelID, threadTS string, status StatusType, text string) error {
	key := channelID + ":" + threadTS

	m.mu.Lock()
	m.evictStaleStates()

	if text == "" {
		m.clearEmojiLocked(ctx, channelID, threadTS)
		delete(m.threadState, key)
		m.mu.Unlock()
		return nil
	}
	if ts := m.threadState[key]; ts != nil {
		if ts.lastText == text {
			m.mu.Unlock()
			return nil
		}
		if time.Since(ts.lastTime) < statusMinInterval {
			m.mu.Unlock()
			return nil
		}
	}

	if m.threadState[key] == nil {
		m.threadState[key] = &threadState{}
	}
	m.threadState[key].lastText = text
	m.threadState[key].lastTime = time.Now()
	m.mu.Unlock()

	// Fast path: workspace has no Assistant API → manage emoji directly.
	if m.emojiOnly.Load() {
		return m.setEmoji(ctx, channelID, threadTS, status)
	}

	if m.adapter != nil {
		return m.adapter.SetStatus(ctx, channelID, threadTS, status, text)
	}
	return nil
}

// Clear removes any tracked status emoji and state for the thread.
func (m *StatusManager) Clear(ctx context.Context, channelID, threadTS string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearEmojiLocked(ctx, channelID, threadTS)
	delete(m.threadState, channelID+":"+threadTS)
}

// SetLastTool records the tool name for the given thread so the next ToolResult can use it.
func (m *StatusManager) SetLastTool(channelID, threadTS, name string) {
	key := channelID + ":" + threadTS
	m.mu.Lock()
	if m.threadState[key] == nil {
		m.threadState[key] = &threadState{}
	}
	m.threadState[key].lastTool = name
	m.mu.Unlock()
}

// LastTool returns the most recently recorded tool name for the given thread.
func (m *StatusManager) LastTool(channelID, threadTS string) string {
	key := channelID + ":" + threadTS
	m.mu.Lock()
	defer m.mu.Unlock()
	if ts := m.threadState[key]; ts != nil {
		return ts.lastTool
	}
	return ""
}

// evictStaleStates removes threadState entries older than threadStateTTL.
// Caller must hold m.mu.
func (m *StatusManager) evictStaleStates() {
	if len(m.threadState) < 10 {
		return
	}
	now := time.Now()
	for k, ts := range m.threadState {
		if now.Sub(ts.lastTime) > threadStateTTL {
			delete(m.threadState, k)
		}
	}
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

// setEmoji manages emoji reactions directly (fast path for free workspaces).
// Removes previous emoji if different, then adds the new one.
// Thread-safety for same channel:thread relies on SlackConn.handlerMu serialization upstream.
func (m *StatusManager) setEmoji(ctx context.Context, channelID, threadTS string, status StatusType) error {
	emoji, ok := StatusEmojiMap[status]
	if !ok || emoji == "" || threadTS == "" {
		return nil
	}

	m.mu.Lock()
	key := channelID + ":" + threadTS
	prevEmoji := m.emojiState[key]
	m.mu.Unlock()

	if m.adapter != nil && m.adapter.client != nil {
		if prevEmoji != "" && prevEmoji != emoji {
			_ = m.adapter.client.RemoveReactionContext(ctx, prevEmoji, slack.ItemRef{
				Channel:   channelID,
				Timestamp: threadTS,
			})
		}
		err := m.adapter.client.AddReactionContext(ctx, emoji, slack.ItemRef{
			Channel:   channelID,
			Timestamp: threadTS,
		})
		if err == nil {
			m.mu.Lock()
			m.emojiState[key] = emoji
			m.mu.Unlock()
		}
		return err
	}
	return nil
}

// extractCallNameInput extracts (name, input) from a tool_call envelope.
func extractCallNameInput(env *events.Envelope) (string, map[string]any) {
	if env.Event.Data == nil {
		return "tool", nil
	}
	if data, ok := env.Event.Data.(*events.ToolCallData); ok {
		return data.Name, data.Input
	}
	if m, ok := env.Event.Data.(map[string]any); ok {
		name, _ := m["name"].(string)
		input, _ := m["input"].(map[string]any)
		return name, input
	}
	return "tool", nil
}

// extractResultFields extracts (output, errMsg) from a tool_result envelope.
func extractResultFields(env *events.Envelope) (any, string) {
	if env.Event.Data == nil {
		return nil, ""
	}
	if data, ok := env.Event.Data.(*events.ToolResultData); ok {
		return data.Output, data.Error
	}
	if m, ok := env.Event.Data.(map[string]any); ok {
		errMsg, _ := m["error"].(string)
		return m["output"], errMsg
	}
	return nil, ""
}

// statusTextLimit is the max rune length for tool status text.
const statusTextLimit = 80

// shortenPaths replaces workDir with "$WK" then homeDir with "~" in s.
var (
	homeDir   string
	workDir   string
	workDirMu sync.RWMutex
)

func init() {
	if dir, err := os.UserHomeDir(); err == nil {
		homeDir = dir
	}
}

// SetWorkDir sets the workdir used for $WK substitution in status text.
func SetWorkDir(dir string) {
	workDirMu.Lock()
	workDir = dir
	workDirMu.Unlock()
}

func shortenPaths(s string) string {
	workDirMu.RLock()
	wd := workDir
	workDirMu.RUnlock()
	if wd != "" {
		s = strings.ReplaceAll(s, wd, "$WK")
	}
	if homeDir != "" {
		s = strings.ReplaceAll(s, homeDir, "~")
	}
	return s
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
		a.Log.Warn("slack: Assistant API no longer available, switching to emoji fallback",
			"err", err)
		a.isAssistantCapable.Store(false)
		if a.statusMgr != nil {
			a.statusMgr.SetEmojiOnly(true)
		}
	} else {
		a.Log.Debug("slack: Assistant API call failed, trying emoji fallback",
			"err", err)
	}
}

func (a *Adapter) setStatusWithEmojiFallback(ctx context.Context, channelID, threadTS string, status StatusType) error {
	emoji, ok := StatusEmojiMap[status]
	if !ok || emoji == "" || threadTS == "" {
		return nil
	}

	// Remove previous status emoji before adding new one.
	a.statusMgr.mu.Lock()
	key := channelID + ":" + threadTS
	prevEmoji := a.statusMgr.emojiState[key]
	a.statusMgr.mu.Unlock()

	if prevEmoji != "" && prevEmoji != emoji && a.client != nil {
		_ = a.client.RemoveReactionContext(ctx, prevEmoji, slack.ItemRef{
			Channel:   channelID,
			Timestamp: threadTS,
		})
	}

	err := a.client.AddReactionContext(ctx, emoji, slack.ItemRef{
		Channel:   channelID,
		Timestamp: threadTS,
	})
	if err == nil {
		a.statusMgr.mu.Lock()
		a.statusMgr.emojiState[key] = emoji
		a.statusMgr.mu.Unlock()
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
			a.Log.Warn("slack: Assistant API not available (free workspace?), falling back to emoji reactions",
				"err", err)
			return false
		}
		// Transient or benign error (e.g. channel_not_found from empty params): treat as capable
		a.Log.Info("slack: Assistant API probe skipped (benign error), assuming capable",
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
