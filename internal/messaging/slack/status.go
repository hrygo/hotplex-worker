package slack

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
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

// toolStatusFormatter produces a human-readable status string from tool input.
type toolStatusFormatter func(input map[string]any) string

// toolStatusFormatters maps tool names to specialized status formatters.
// Unregistered tools fall through to the generic key=value format.
var toolStatusFormatters = map[string]toolStatusFormatter{
	"TodoWrite":    formatTodoWriteStatus,
	"Read":         formatFileToolStatus("📖 Reading", "file_path"),
	"Edit":         formatFileToolStatus("✏️ Editing", "file_path"),
	"Write":        formatFileToolStatus("📝 Writing", "file_path"),
	"NotebookEdit": formatFileToolStatus("📓 Editing", "notebook_path"),
	"Bash":         formatBashStatus,
	"Grep":         formatGrepStatus,
	"Glob":         formatGlobStatus,
	"Agent":        formatAgentStatus,
	"WebSearch":    formatSimpleToolStatus("🌐 Searching", "query"),
	"WebFetch":     formatSimpleToolStatus("🌐 Fetching", "url"),
	"LSP":          formatLSPStatus,
}

// statusTextLimit is the max rune length for tool status text.
const statusTextLimit = 80

// toolNameFromEnvelope extracts the tool name from a ToolCall envelope.
func toolNameFromEnvelope(env *events.Envelope) string {
	if env.Event.Data == nil {
		return ""
	}
	if data, ok := env.Event.Data.(*events.ToolCallData); ok {
		return data.Name
	}
	if m, ok := env.Event.Data.(map[string]any); ok {
		if n, ok := m["name"].(string); ok {
			return n
		}
	}
	return ""
}

// extractToolCallStatus formats tool call info into a human-readable status string.
// Registered tools get specialized formatters; others use generic "Name(key=val)" format.
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

	if fn, ok := toolStatusFormatters[name]; ok && input != nil {
		return truncateWithSuffix(fn(input), statusTextLimit)
	}

	if len(input) == 0 {
		return truncateWithSuffix(name, statusTextLimit)
	}

	parts := make([]string, 0, len(input))
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, k+"="+truncateWithSuffix(shortenPaths(fmt.Sprintf("%v", input[k])), 30))
	}
	body := strings.Join(parts, ", ")
	return truncateWithSuffix(name+"("+body+")", statusTextLimit)
}

// --- Tool-specific formatters ---

// formatTodoWriteStatus shows task progress from TodoWrite input.
// Prioritizes the in_progress task; falls back to summary stats.
func formatTodoWriteStatus(input map[string]any) string {
	raw, ok := input["todos"]
	if !ok {
		return "📋 Updating tasks..."
	}

	type todoItem struct {
		content    string
		activeForm string
		status     string
	}

	var todos []todoItem
	if v, ok := raw.([]any); ok {
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t := todoItem{}
			if c, ok := m["content"].(string); ok {
				t.content = c
			}
			if a, ok := m["activeForm"].(string); ok {
				t.activeForm = a
			}
			if s, ok := m["status"].(string); ok {
				t.status = s
			}
			todos = append(todos, t)
		}
	}

	if len(todos) == 0 {
		return "📋 Updating tasks..."
	}

	var inProgress []string
	var completed, pending int
	for _, t := range todos {
		switch t.status {
		case "completed":
			completed++
		case "in_progress":
			label := t.activeForm
			if label == "" {
				label = t.content
			}
			if label != "" {
				inProgress = append(inProgress, label)
			}
		default:
			pending++
		}
	}

	if len(inProgress) > 0 {
		return "📋 " + inProgress[0]
	}

	return fmt.Sprintf("📋 %d tasks (%d done · %d pending)", len(todos), completed, pending)
}

// formatFileToolStatus returns a formatter for file-based tools (Read/Edit/Write).
func formatFileToolStatus(prefix, key string) toolStatusFormatter {
	return func(input map[string]any) string {
		path, _ := input[key].(string)
		if path == "" {
			return prefix + "..."
		}
		return prefix + " " + extractFileName(shortenPaths(path))
	}
}

// formatSimpleToolStatus returns a formatter that shows prefix + value of one key.
func formatSimpleToolStatus(prefix, key string) toolStatusFormatter {
	return func(input map[string]any) string {
		val, _ := input[key].(string)
		if val == "" {
			return prefix + "..."
		}
		return prefix + " " + val
	}
}

// formatBashStatus shows the command being executed.
func formatBashStatus(input map[string]any) string {
	cmd, _ := input["command"].(string)
	if cmd == "" {
		return "⏳ Running command..."
	}
	return "⏳ " + shortenPaths(cmd)
}

// formatGrepStatus shows the search pattern.
func formatGrepStatus(input map[string]any) string {
	pattern, _ := input["pattern"].(string)
	if pattern == "" {
		return "🔍 Searching..."
	}
	path, _ := input["path"].(string)
	if path != "" {
		return "🔍 " + pattern + " in " + extractFileName(shortenPaths(path))
	}
	return "🔍 " + pattern
}

// formatGlobStatus shows the glob pattern.
func formatGlobStatus(input map[string]any) string {
	pattern, _ := input["pattern"].(string)
	if pattern == "" {
		return "📂 Finding files..."
	}
	return "📂 " + pattern
}

// formatAgentStatus shows the agent description or type.
func formatAgentStatus(input map[string]any) string {
	desc, _ := input["description"].(string)
	if desc != "" {
		return "🤖 " + desc
	}
	subagent, _ := input["subagent_type"].(string)
	if subagent != "" {
		return "🤖 " + subagent
	}
	return "🤖 Spawning agent..."
}

// formatLSPStatus shows the LSP operation and target.
func formatLSPStatus(input map[string]any) string {
	op, _ := input["operation"].(string)
	filePath, _ := input["filePath"].(string)
	name := lspOpLabel(op)
	if filePath != "" {
		return name + " " + extractFileName(shortenPaths(filePath))
	}
	return name
}

// lspOpLabel maps LSP operations to human-readable labels.
func lspOpLabel(op string) string {
	switch op {
	case "hover":
		return "🔎 Hover"
	case "goToDefinition":
		return "🔎 Go to def"
	case "findReferences":
		return "🔎 Find refs"
	case "documentSymbol":
		return "🔎 Symbols"
	case "workspaceSymbol":
		return "🔎 Workspace search"
	case "goToImplementation":
		return "🔎 Go to impl"
	case "prepareCallHierarchy":
		return "🔎 Call hierarchy"
	case "incomingCalls":
		return "🔎 Incoming calls"
	case "outgoingCalls":
		return "🔎 Outgoing calls"
	default:
		return "🔎 LSP"
	}
}

// extractFileName returns the last path component from a file path.
func extractFileName(path string) string {
	if path == "" {
		return ""
	}
	// Handle both / and \ separators for robustness
	path = strings.ReplaceAll(path, `\`, "/")
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return path
}

// extractToolResultStatus formats tool result preview truncated to statusTextLimit chars.
func extractToolResultStatus(env *events.Envelope) string {
	if env.Event.Data == nil {
		return "Tool completed"
	}

	if data, ok := env.Event.Data.(*events.ToolResultData); ok {
		if data.Error != "" {
			return truncateWithSuffix(shortenPaths("Error: "+data.Error), statusTextLimit)
		}
		if data.Output != nil {
			return truncateWithSuffix(shortenPaths(limitedSprintf(data.Output, 200)), statusTextLimit)
		}
		return "Tool completed"
	}

	if m, ok := env.Event.Data.(map[string]any); ok {
		if errStr, ok := m["error"].(string); ok && errStr != "" {
			return truncateWithSuffix(shortenPaths("Error: "+errStr), statusTextLimit)
		}
		if output, ok := m["output"]; ok && output != nil {
			return truncateWithSuffix(shortenPaths(limitedSprintf(output, 200)), statusTextLimit)
		}
	}

	return "Tool completed"
}

// limitedSprintf converts v to string, capping output to maxBytes to avoid
// allocating arbitrarily large strings from tool output before truncation.
func limitedSprintf(v any, maxBytes int) string {
	if s, ok := v.(string); ok {
		if len(s) > maxBytes {
			return s[:maxBytes]
		}
		return s
	}
	s := fmt.Sprintf("%v", v)
	if len(s) > maxBytes {
		return s[:maxBytes]
	}
	return s
}

// shortenPaths replaces workDir with "$WK" then homeDir with "~" in s.
var (
	homeDir string
	workDir string
)

func init() {
	if dir, err := os.UserHomeDir(); err == nil {
		homeDir = dir
	}
}

// SetWorkDir sets the workdir used for $WK substitution in status text.
func SetWorkDir(dir string) {
	workDir = dir
}

func shortenPaths(s string) string {
	if workDir != "" {
		s = strings.ReplaceAll(s, workDir, "$WK")
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
		a.log.Warn("slack: Assistant API no longer available, switching to emoji fallback",
			"err", err)
		a.isAssistantCapable.Store(false)
		if a.statusMgr != nil {
			a.statusMgr.SetEmojiOnly(true)
		}
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
