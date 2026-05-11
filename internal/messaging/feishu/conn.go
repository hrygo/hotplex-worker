package feishu

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/pkg/events"
)

type FeishuConn struct {
	adapter   *Adapter
	chatID    string
	threadKey string

	mu                sync.RWMutex
	chatType          string
	replyToMsgID      string
	platformMsgID     string
	sessionID         string
	streamCtrl        *StreamingCardController
	typingRid         string
	thinkingRid       string // THINKING reaction from silence timer
	silenceTimer      *time.Timer
	workDir           string // current workDir identity for session key derivation
	turnCount         int    // cached from last Done event, 0 = first turn
	lastModel         string // cached from last TurnSummaryData
	lastBranch        string // cached from last TurnSummaryData
	lastSummarySentMs atomic.Int64
	voiceTriggered    atomic.Bool
}

func NewFeishuConn(adapter *Adapter, chatID, threadKey, workDir string) *FeishuConn {
	return &FeishuConn{adapter: adapter, chatID: chatID, threadKey: threadKey, workDir: workDir}
}

func (c *FeishuConn) WorkDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.workDir
}

func (c *FeishuConn) SetWorkDir(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workDir = dir
}

func (c *FeishuConn) cacheTurnMeta(d messaging.TurnSummaryData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d.TurnCount > 0 {
		c.turnCount = d.TurnCount
	}
	if d.ModelName != "" {
		c.lastModel = d.ModelName
	}
	if d.GitBranch != "" {
		c.lastBranch = d.GitBranch
	}
}

// turnHeaderMeta returns cached turn metadata for card header construction.
// When branch is unknown, resolves it from the workDir via git and caches it.
func (c *FeishuConn) turnHeaderMeta() (turnNum int, model, branch, workDir string) {
	c.mu.RLock()
	tn, m, br, wd := c.turnCount, c.lastModel, c.lastBranch, c.workDir
	c.mu.RUnlock()

	// Lazy-resolve branch from workDir on first turn or after /new reset.
	if br == "" && wd != "" {
		if resolved := messaging.GitBranchOf(wd); resolved != "" {
			c.mu.Lock()
			// Double-check: another goroutine may have set it via cacheTurnMeta.
			if c.lastBranch == "" {
				c.lastBranch = resolved
			}
			br = c.lastBranch
			c.mu.Unlock()
		}
	}

	return tn, m, br, wd
}

func (c *FeishuConn) EnableStreaming(ctrl *StreamingCardController) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streamCtrl = ctrl
}

// GetStreamCtrl returns the current streaming controller (exported for use by handleTextMessage).
func (c *FeishuConn) GetStreamCtrl() *StreamingCardController {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.streamCtrl
}

func (c *FeishuConn) getStreamCtrl() *StreamingCardController {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.streamCtrl
}

// resetStreamCtrl replaces a closed streaming controller with a fresh one
// so subsequent events can create a new card via lazy-init.
func (c *FeishuConn) resetStreamCtrl() {
	newCtrl := NewStreamingCardController(
		c.adapter.larkClient, c.adapter.rateLimiter, c.adapter.Log,
		c.adapter.resolveBotName(), c.turnCount, c.lastModel, c.lastBranch, c.workDir,
	)
	c.mu.Lock()
	c.streamCtrl = newCtrl
	c.mu.Unlock()
}

func (c *FeishuConn) SetTypingReactionID(rid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.typingRid = rid
}

// setProcessingReaction adds a "THINKING" reaction to the user's message.
// Returns the reaction ID for later cleanup. Non-fatal on failure.
func (c *FeishuConn) setProcessingReaction(ctx context.Context) string {
	c.mu.RLock()
	msgID := c.platformMsgID
	c.mu.RUnlock()
	if msgID == "" {
		return ""
	}
	rid, err := c.adapter.addReaction(ctx, msgID, "THINKING")
	if err != nil {
		c.adapter.Log.Debug("feishu: processing reaction failed (non-fatal)", "err", err)
	}
	return rid
}

// clearProcessingReaction removes a previously set processing reaction.
func (c *FeishuConn) clearProcessingReaction(ctx context.Context, rid string) {
	if rid == "" {
		return
	}
	c.mu.RLock()
	msgID := c.platformMsgID
	c.mu.RUnlock()
	if msgID == "" {
		return
	}
	_ = c.adapter.removeReaction(ctx, msgID, rid)
}

func (c *FeishuConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
	if env == nil {
		return fmt.Errorf("feishu: nil envelope")
	}

	if env.SessionID != "" {
		c.mu.Lock()
		c.sessionID = env.SessionID
		c.mu.Unlock()
	}

	switch env.Event.Type {
	case events.Done:
		return c.handleDone(ctx, env)
	case events.Error:
		return c.handleError(ctx, env)
	case events.ToolCall:
		return c.handleToolCall(ctx, env)
	case events.ToolResult:
		return c.handleToolResult(ctx, env)
	case events.PermissionRequest:
		return c.handleInteraction(ctx, env, c.sendPermissionRequest)
	case events.QuestionRequest:
		return c.handleInteraction(ctx, env, c.sendQuestionRequest)
	case events.ElicitationRequest:
		return c.handleInteraction(ctx, env, c.sendElicitationRequest)
	case events.ContextUsage:
		return c.handleStatusEvent(ctx, env, c.sendContextUsage)
	case events.MCPStatus:
		return c.handleStatusEvent(ctx, env, c.sendMCPStatus)
	case events.SkillsList:
		return c.handleStatusEvent(ctx, env, c.sendSkillsList)
	case events.Message:
		return c.handleMessageEvent(ctx, env)
	}

	text, ok := messaging.ExtractResponseText(env)
	if !ok {
		return nil
	}
	if env.Event.Type == events.MessageDelta && text != "" {
		text += "\n\n"
	}
	text = StripInvalidImageKeys(text)
	return c.writeContent(ctx, env, text)
}

func (c *FeishuConn) handleDone(ctx context.Context, env *events.Envelope) error {
	streamCtrl := c.clearActiveIndicators(ctx)
	c.adapter.Interactions.CancelAll(env.SessionID)

	d := messaging.ExtractTurnSummary(env)
	c.cacheTurnMeta(d)
	c.adapter.Log.Info("turn summary",
		"turn_count", d.TurnCount,
		"duration_ms", d.TurnDurationMs,
		"model", d.ModelName,
		"tool_calls", d.ToolCallCount,
		"input_tok", d.TotalInputTok,
	)
	if c.adapter.turnSummaryEnabled {
		go c.sendTurnSummaryCard(d)
	}

	var fullText string
	if streamCtrl != nil && streamCtrl.IsCreated() {
		fullText = streamCtrl.Content()
	}

	var closeErr error
	if streamCtrl != nil && streamCtrl.IsCreated() {
		streamCtrl.SetCloseMeta(d)
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		closeErr = streamCtrl.Close(closeCtx)
		closeCancel()
	}

	ttsOK := c.adapter.ttsPipeline != nil
	voiceOK := c.voiceTriggered.Load()
	c.adapter.Log.Debug("feishu: tts check",
		"tts_pipeline", ttsOK,
		"voice_triggered", voiceOK,
		"full_text_len", len(fullText),
	)
	if ttsOK && voiceOK {
		if fullText != "" {
			c.mu.RLock()
			chatID := c.chatID
			replyID := c.replyToMsgID
			c.mu.RUnlock()
			ttsCtx, ttsCancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
			go func() {
				defer ttsCancel()
				c.adapter.ttsPipeline.Process(ttsCtx, fullText, chatID, replyID)
			}()
		}
		c.voiceTriggered.Store(false)
	}
	return closeErr
}

func (c *FeishuConn) handleError(ctx context.Context, env *events.Envelope) error {
	streamCtrl := c.clearActiveIndicators(ctx)
	c.adapter.Interactions.CancelAll(env.SessionID)
	if streamCtrl != nil && streamCtrl.IsCreated() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := streamCtrl.Close(closeCtx); err != nil {
			c.adapter.Log.Warn("feishu: failed to close streaming card on error", "err", err)
		}
		closeCancel()
	}
	if errMsg := messaging.ExtractErrorMessage(env); errMsg != "" {
		c.mu.RLock()
		platformMsgID := c.platformMsgID
		c.mu.RUnlock()
		if platformMsgID != "" {
			_ = c.adapter.replyMessage(ctx, platformMsgID, errMsg, false)
		}
	}
	return nil
}

func (c *FeishuConn) handleToolCall(_ context.Context, env *events.Envelope) error {
	c.resetSilenceTimer()
	if ctrl := c.getStreamCtrl(); ctrl != nil {
		if id, name, input := extractToolCallData(env); id != "" {
			ctrl.WriteToolCall(id, name, input)
		}
	}
	return nil
}

func (c *FeishuConn) handleToolResult(_ context.Context, env *events.Envelope) error {
	c.resetSilenceTimer()
	if ctrl := c.getStreamCtrl(); ctrl != nil {
		if id, output, errMsg := extractToolResultData(env); id != "" {
			ctrl.WriteToolResult(id, output, errMsg)
		}
	}
	return nil
}

// interactionHandler is a function that sends an interaction request to the user.
type interactionHandler func(ctx context.Context, env *events.Envelope) error

func (c *FeishuConn) handleInteraction(ctx context.Context, env *events.Envelope, send interactionHandler) error {
	streamCtrl := c.clearActiveIndicators(ctx)
	if streamCtrl != nil && streamCtrl.IsCreated() {
		_ = streamCtrl.Close(ctx)
		c.resetStreamCtrl()
	}
	rid := c.setProcessingReaction(ctx)
	err := send(ctx, env)
	c.clearProcessingReaction(ctx, rid)
	return err
}

// statusHandler is a function that sends a status/event display to the user.
type statusHandler func(ctx context.Context, env *events.Envelope) error

func (c *FeishuConn) handleStatusEvent(ctx context.Context, env *events.Envelope, send statusHandler) error {
	rid := c.setProcessingReaction(ctx)
	err := send(ctx, env)
	c.clearProcessingReaction(ctx, rid)
	return err
}

func (c *FeishuConn) handleMessageEvent(ctx context.Context, env *events.Envelope) error {
	var content string
	if msgData, ok := env.Event.Data.(events.MessageData); ok {
		content = msgData.Content
	} else if m, ok := env.Event.Data.(map[string]any); ok {
		content, _ = m["content"].(string)
	}
	if content == "" {
		return nil
	}
	return c.sendOrReply(ctx, OptimizeMarkdownStyle(SanitizeForCard(messaging.SanitizeText(content))))
}

func (c *FeishuConn) Close() error {
	c.mu.Lock()
	streamCtrl := c.streamCtrl
	typingRid := c.typingRid
	thinkingRid := c.thinkingRid
	platformMsgID := c.platformMsgID
	c.streamCtrl = nil
	c.typingRid = ""
	c.thinkingRid = ""
	if c.silenceTimer != nil {
		c.silenceTimer.Stop()
		c.silenceTimer = nil
	}
	c.mu.Unlock()

	// Best-effort cleanup: try Close() for proper final flush.
	// Falls back silently if already in a terminal phase (Completed/Aborted).
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if streamCtrl != nil {
		_ = streamCtrl.Close(closeCtx)
	}
	if typingRid != "" && c.adapter.larkClient != nil {
		_ = c.adapter.RemoveTypingIndicator(context.Background(), platformMsgID, typingRid)
	}
	if thinkingRid != "" && c.adapter.larkClient != nil {
		_ = c.adapter.removeReaction(context.Background(), platformMsgID, thinkingRid)
	}
	c.adapter.DeleteConn(c.chatID, c.threadKey)
	return nil
}

// clearActiveIndicators removes typing and thinking indicators,
// returning the stream controller for caller cleanup.
func (c *FeishuConn) clearActiveIndicators(ctx context.Context) *StreamingCardController {
	c.mu.Lock()
	streamCtrl := c.streamCtrl
	c.streamCtrl = nil
	typingRid := c.typingRid
	thinkingRid := c.thinkingRid
	platformMsgID := c.platformMsgID
	c.typingRid = ""
	c.thinkingRid = ""
	if c.silenceTimer != nil {
		c.silenceTimer.Stop()
		c.silenceTimer = nil
	}
	c.mu.Unlock()

	if typingRid != "" {
		_ = c.adapter.RemoveTypingIndicator(ctx, platformMsgID, typingRid)
	}
	if thinkingRid != "" {
		_ = c.adapter.removeReaction(ctx, platformMsgID, thinkingRid)
	}
	return streamCtrl
}

// removeTypingReaction removes the Typing and THINKING reactions from the user's message.
// Called when streaming content first becomes visible — no longer need silence feedback.
func (c *FeishuConn) removeTypingReaction(ctx context.Context) {
	c.mu.Lock()
	rid := c.typingRid
	thRid := c.thinkingRid
	msgID := c.platformMsgID
	c.typingRid = ""
	c.thinkingRid = ""
	c.mu.Unlock()
	if rid != "" && msgID != "" {
		_ = c.adapter.removeReaction(ctx, msgID, rid)
	}
	if thRid != "" && msgID != "" {
		_ = c.adapter.removeReaction(ctx, msgID, thRid)
	}
}

// resetSilenceTimer resets the silence timer. Called on every worker event
// (ToolCall, ToolResult, MessageDelta) to delay the THINKING reaction.
// The timer fires only after continuous silence of silenceTimeout.
// Any existing THINKING reaction from a previous silence period is cleaned up.
func (c *FeishuConn) resetSilenceTimer() {
	c.mu.Lock()
	if c.silenceTimer != nil {
		c.silenceTimer.Stop()
	}
	var oldRid, oldMsgID string
	if c.thinkingRid != "" {
		oldRid = c.thinkingRid
		oldMsgID = c.platformMsgID
		c.thinkingRid = ""
	}
	adapter := c.adapter
	msgID := c.platformMsgID
	c.silenceTimer = time.AfterFunc(silenceTimeout, func() {
		if msgID == "" || adapter.larkClient == nil {
			return
		}
		adapter.Log.Debug("feishu: silence timeout, adding THINKING reaction", "msg", msgID)
		if rid, err := adapter.addReaction(context.Background(), msgID, "THINKING"); err == nil && rid != "" {
			c.mu.Lock()
			c.thinkingRid = rid
			c.mu.Unlock()
		}
	})
	c.mu.Unlock()

	// Remove previous THINKING reaction outside the lock.
	if oldRid != "" && oldMsgID != "" {
		_ = adapter.removeReaction(context.Background(), oldMsgID, oldRid)
	}
}

// writeContent delivers text content via streaming card or static fallback.
func (c *FeishuConn) writeContent(ctx context.Context, env *events.Envelope, text string) error {
	c.resetSilenceTimer()
	c.mu.Lock()
	chatID := c.chatID
	replyToMsgID := c.replyToMsgID
	streamCtrl := c.streamCtrl
	chatType := c.chatType
	c.mu.Unlock()

	c.adapter.Log.Debug("feishu: WriteCtx sending",
		"event_type", env.Event.Type,
		"chat", chatID,
		"reply_to", replyToMsgID,
		"text_len", len(text),
	)

	// TTL rotation: proactively replace expired streaming cards before
	// Feishu's 10-minute server limit kicks in.
	if streamCtrl != nil && streamCtrl.IsCreated() && streamCtrl.Expired() {
		// Mark all tools done so Close can flush done markers to the old card.
		streamCtrl.MarkAllToolsDone()
		oldMsgID := streamCtrl.MsgID()

		// Synchronous close: ensures old card is fully finalized before
		// creating the new one, avoiding API rate-limit collisions.
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := streamCtrl.Close(closeCtx); err != nil {
			c.adapter.Log.Warn("feishu: failed to close rotated card",
				"old_msg_id", oldMsgID, "err", err)
			metrics.StreamingCardRotationFailures.WithLabelValues("close_old").Inc()
		}
		closeCancel()

		metrics.StreamingCardRotationsTotal.Inc()
		c.adapter.Log.Info("feishu: streaming card rotated",
			"old_msg_id", oldMsgID)

		newCtrl := NewStreamingCardController(c.adapter.larkClient, c.adapter.rateLimiter, c.adapter.Log, c.adapter.resolveBotName(), c.turnCount+1, c.lastModel, c.lastBranch, c.workDir)
		c.mu.Lock()
		c.streamCtrl = newCtrl
		if oldMsgID != "" {
			c.replyToMsgID = oldMsgID
			replyToMsgID = oldMsgID
		}
		streamCtrl = newCtrl
		c.mu.Unlock()
	}

	if streamCtrl != nil {
		// Lazy-init: create card on first content arrival.
		if !streamCtrl.IsCreated() {
			if err := streamCtrl.EnsureCard(ctx, chatID, chatType, replyToMsgID, text); err != nil {
				c.adapter.Log.Warn("feishu: streaming card init failed, falling back to static", "err", err)
				c.mu.Lock()
				metrics.StreamingCardRotationFailures.WithLabelValues("ensure_card").Inc()
				c.streamCtrl = nil
				c.mu.Unlock()
			} else {
				return nil
			}
		} else {
			// Background flush loop handles delivery.
			if err := streamCtrl.Write(text); err != nil {
				// Streaming failed — flush buffered content and fall back to static delivery.
				c.adapter.Log.Warn("feishu: streaming write failed, falling back to static", "err", err)
				_ = streamCtrl.Close(context.Background())
				c.mu.Lock()
				c.streamCtrl = nil
				c.mu.Unlock()
			} else {
				c.removeTypingReaction(ctx)
			}
			return nil
		}
	}

	if replyToMsgID != "" {
		return c.adapter.replyMessage(ctx, replyToMsgID, OptimizeMarkdownStyle(SanitizeForCard(text)), false)
	}
	return c.adapter.sendTextMessage(ctx, chatID, OptimizeMarkdownStyle(SanitizeForCard(text)))
}

func (c *FeishuConn) sendTurnSummaryCard(d messaging.TurnSummaryData) {
	cardJSON := buildTurnSummaryCard(d, cardHeader{
		Title:    c.adapter.resolveBotName(),
		Template: headerBlue,
		Tags:     turnTags(d.TurnCount, d.ModelName, d.GitBranch, c.WorkDir()),
	})
	if cardJSON == "" {
		return
	}
	now := time.Now().UnixMilli()
	last := c.lastSummarySentMs.Load()
	if now-last < messaging.TurnSummaryCooldown.Milliseconds() {
		return
	}
	// CAS to win the race: only one goroutine proceeds past the cooldown.
	if !c.lastSummarySentMs.CompareAndSwap(last, now) {
		return
	}
	sendCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := c.sendOrReplyCard(sendCtx, cardJSON)
	if err != nil {
		c.adapter.Log.Warn("turn summary card send failed", "err", err)
		return
	}
	c.lastSummarySentMs.Store(time.Now().UnixMilli())
}

// sendOrReply sends text via reply (if in a thread) or direct message.
func (c *FeishuConn) sendOrReply(ctx context.Context, text string) error {
	c.mu.RLock()
	rid := c.replyToMsgID
	cid := c.chatID
	c.mu.RUnlock()
	if rid != "" {
		return c.adapter.replyMessage(ctx, rid, text, false)
	}
	return c.adapter.sendTextMessage(ctx, cid, text)
}

// sendOrReplyCard sends a card via reply (if in a thread) or direct message.
func (c *FeishuConn) sendOrReplyCard(ctx context.Context, cardJSON string) error {
	c.mu.RLock()
	rid := c.replyToMsgID
	cid := c.chatID
	c.mu.RUnlock()
	if rid != "" {
		return c.adapter.doReplyCard(ctx, rid, cardJSON, false)
	}
	return c.adapter.sendCardMessage(ctx, cid, cardJSON)
}

func (c *FeishuConn) sendContextUsage(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractContextUsageData(env)
	if err != nil {
		return nil
	}
	return c.sendOrReply(ctx, messaging.FormatCanonicalText(d))
}

func (c *FeishuConn) sendMCPStatus(ctx context.Context, env *events.Envelope) error {
	d, ok := messaging.ExtractMCPStatusData(env)
	if !ok {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("🔌 MCP Server Status")
	for _, s := range d.Servers {
		fmt.Fprintf(&sb, "\n%s %s — %s", messaging.MCPServerIcon(s.Status), s.Name, s.Status)
	}
	return c.sendOrReply(ctx, sb.String())
}

var _ messaging.PlatformConn = (*FeishuConn)(nil)

// handleTextControlCommand sends a control event derived from a text message
// through the bridge, then sends feedback via card message.
var _ messaging.PlatformConn = (*FeishuConn)(nil)
