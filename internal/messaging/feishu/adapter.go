// Package feishu provides a Feishu (Lark) WebSocket platform adapter.
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/messaging/stt"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

func init() {
	messaging.Register(messaging.PlatformFeishu, func(log *slog.Logger) messaging.PlatformAdapterInterface {
		return &Adapter{log: log}
	})
}

type Adapter struct {
	messaging.PlatformAdapter

	log         *slog.Logger
	appID       string
	appSecret   string
	wsClient    *ws.Client
	larkClient  *lark.Client
	bridge      *messaging.Bridge
	botOpenID   string
	transcriber Transcriber

	backoffBaseDelay time.Duration
	backoffMaxDelay  time.Duration

	mu           sync.RWMutex
	dedup        *Dedup
	activeConns  map[string]*FeishuConn
	gate         *Gate
	chatQueue    *ChatQueue
	interactions *messaging.InteractionManager
	rateLimiter  *FeishuRateLimiter
	dedupDone    chan struct{}
	dedupWg      sync.WaitGroup
	started      atomic.Bool
}

func (a *Adapter) Platform() messaging.PlatformType { return messaging.PlatformFeishu }

func (a *Adapter) Configure(appID, appSecret string, bridge *messaging.Bridge) {
	a.appID = appID
	a.appSecret = appSecret
	a.bridge = bridge
}

func (a *Adapter) SetBridge(b *messaging.Bridge) {
	a.bridge = b
}

func (a *Adapter) SetGate(gate *Gate) {
	a.gate = gate
}

func (a *Adapter) SetTranscriber(t Transcriber) {
	a.transcriber = t
}

func (a *Adapter) SetReconnectDelays(baseDelay, maxDelay time.Duration) {
	a.backoffBaseDelay = baseDelay
	a.backoffMaxDelay = maxDelay
}

func (a *Adapter) Start(ctx context.Context) error {
	if !a.started.CompareAndSwap(false, true) {
		a.log.Warn("feishu: adapter already started, skipping")
		return nil
	}
	if a.appID == "" || a.appSecret == "" {
		return fmt.Errorf("feishu: appID and appSecret required")
	}

	a.dedup = NewDedup(dedupDefaultMaxEntries, dedupDefaultTTL)
	a.activeConns = make(map[string]*FeishuConn)
	a.interactions = messaging.NewInteractionManager(a.log)
	a.chatQueue = NewChatQueue(a.log)
	a.rateLimiter = NewFeishuRateLimiter()
	a.rateLimiter.Start()
	a.dedupDone = make(chan struct{})
	a.dedupWg.Add(1)
	go a.dedupCleanupLoop()

	a.larkClient = lark.NewClient(a.appID, a.appSecret,
		lark.WithLogger(SlogLogger{Logger: a.log}),
	)

	if err := a.fetchBotOpenID(ctx); err != nil {
		a.log.Warn("feishu: failed to fetch bot open_id, mention detection disabled", "err", err)
	}

	a.log.Info("feishu: starting WebSocket connection")
	go a.runWebSocket(ctx)

	return nil
}

func (a *Adapter) newEventHandler() *dispatcher.EventDispatcher {
	return dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) (err error) {
			defer func() {
				if r := recover(); r != nil {
					a.log.Error("feishu: panic in message handler", "panic", r, "stack", string(debug.Stack()))
					err = fmt.Errorf("feishu handler panic: %v", r)
				}
			}()
			return a.handleMessage(ctx, event)
		}).
		OnP2MessageReadV1(func(_ context.Context, _ *larkim.P2MessageReadV1) error {
			return nil
		}).
		OnP2MessageReactionCreatedV1(func(_ context.Context, _ *larkim.P2MessageReactionCreatedV1) error {
			return nil
		}).
		OnP2MessageReactionDeletedV1(func(_ context.Context, _ *larkim.P2MessageReactionDeletedV1) error {
			return nil
		})
}

func (a *Adapter) runWebSocket(ctx context.Context) {
	baseDelay := a.backoffBaseDelay
	if baseDelay <= 0 {
		baseDelay = 2 * time.Second
	}
	maxDelay := a.backoffMaxDelay
	if maxDelay <= 0 {
		maxDelay = 60 * time.Second
	}
	backoff := newReconnectBackoff(baseDelay, maxDelay)

	attempt := 1
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		client := ws.NewClient(a.appID, a.appSecret,
			ws.WithEventHandler(a.newEventHandler()),
			ws.WithAutoReconnect(true),
			ws.WithLogger(SlogLogger{Logger: a.log}),
		)
		a.mu.Lock()
		a.wsClient = client
		a.mu.Unlock()

		a.log.Info("feishu: starting WebSocket connection", "attempt", attempt)

		if err := client.Start(ctx); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.Next()):
				a.log.Warn("feishu: WebSocket disconnected, reconnecting...",
					"err", err, "attempt", attempt)
				attempt++
				continue
			}
		}

		backoff.Reset()
		attempt = 1
		a.log.Info("feishu: WebSocket closed cleanly, reconnecting...")
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff.Next()):
		}
	}
}

func (a *Adapter) fetchBotOpenID(ctx context.Context) error {
	// Add a bounded timeout to prevent startup hanging on the bot info API.
	botCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := a.larkClient.Get(botCtx, "/open-apis/bot/v3/info", nil, "tenant_access_token")
	if err != nil {
		return fmt.Errorf("bot info API: %w", err)
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Bot  struct {
			OpenID string `json:"open_id"`
		} `json:"bot"`
	}

	body := resp.RawBody
	if len(body) == 0 {
		return fmt.Errorf("bot info API: empty response body")
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse bot info: %w", err)
	}
	if result.Code != 0 {
		return fmt.Errorf("bot info API error: code=%d msg=%s", result.Code, result.Msg)
	}
	if result.Bot.OpenID == "" {
		return fmt.Errorf("bot open_id is empty")
	}
	a.botOpenID = result.Bot.OpenID
	a.log.Info("feishu: bot identity resolved", "open_id", a.botOpenID)
	return nil
}

func (a *Adapter) handleMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event.Event == nil || event.Event.Message == nil {
		return nil
	}

	msg := event.Event.Message

	// Step 2: Bot self-message defense.
	if event.Event.Sender != nil {
		senderType := ptrStr(event.Event.Sender.SenderType)
		if senderType == "app" {
			return nil
		}
	}

	// Step 3: Message expiry check (30 minutes).
	if msg.CreateTime != nil && *msg.CreateTime != "" {
		createTimeMs, err := strconv.ParseInt(*msg.CreateTime, 10, 64)
		if err == nil && IsMessageExpired(createTimeMs) {
			return nil
		}
	}

	// Step 4: Dedup.
	messageID := ptrStr(msg.MessageId)
	if messageID == "" {
		return nil
	}
	a.mu.RLock()
	dedup := a.dedup
	a.mu.RUnlock()
	if dedup == nil {
		return nil // adapter is closing
	}
	if !dedup.TryRecord(messageID) {
		return nil
	}

	// Step 5: Message type conversion.
	msgType := ptrStr(msg.MessageType)
	text, ok, medias := ConvertMessage(msgType, ptrStr(msg.Content), msg.Mentions, a.botOpenID, messageID)
	if !ok || text == "" {
		return nil
	}
	text = messaging.SanitizeText(text)

	// Download media to local files and build structured prompt.
	if len(medias) > 0 {
		var paths []string
		var transcriptions []string
		for _, m := range medias {
			// Audio + STT: try transcription, conditionally skip disk write.
			if m.Type == "audio" && a.transcriber != nil {
				data, ext, fetchErr := a.fetchMediaBytes(ctx, m)
				if fetchErr != nil {
					a.log.Warn("feishu: audio fetch failed", "key", m.Key, "err", fetchErr)
					continue
				}
				transcription, sttErr := a.transcriber.Transcribe(ctx, data)
				if sttErr == nil && transcription != "" {
					transcriptions = append(transcriptions, transcription)
					// Pure cloud STT: skip disk write entirely.
					if !a.transcriber.RequiresDisk() {
						continue
					}
				} else if sttErr != nil {
					a.log.Warn("feishu: stt failed, saving audio to disk", "err", sttErr)
				}
				// Local/fallback mode or STT failure: save to disk for the worker.
				path, saveErr := a.saveMediaBytes(data, m, ext)
				if saveErr != nil {
					a.log.Warn("feishu: audio save failed", "err", saveErr)
					continue
				}
				paths = append(paths, path)
				continue
			}
			// Non-audio or no STT: download to disk.
			path, err := a.downloadMedia(ctx, m)
			if err != nil {
				a.log.Warn("feishu: media download failed", "type", m.Type, "key", m.Key, "err", err)
				continue
			}
			if path != "" {
				paths = append(paths, path)
			}
		}
		if len(paths) > 0 || len(transcriptions) > 0 {
			text = BuildMediaPrompt(text, paths, medias, transcriptions)
		}
	}

	// Step 6: @Mention resolution is done inside ConvertMessage for text/post types.

	// Extract routing info.
	chatType := ptrStr(msg.ChatType)
	chatID := ptrStr(msg.ChatId)
	rootID := ptrStr(msg.RootId)
	parentID := ptrStr(msg.ParentId)
	threadKey := rootID
	if threadKey == "" {
		threadKey = ptrStr(msg.ThreadId)
	}
	userID := ""
	if event.Event.Sender != nil && event.Event.Sender.SenderId != nil {
		userID = ptrStr(event.Event.Sender.SenderId.OpenId)
	}

	// Step 7: Access control.
	botMentioned := isBotMentioned(msg.Mentions, a.botOpenID)
	if a.gate != nil {
		result := a.gate.Check(chatType, userID, botMentioned)
		if !result.Allowed {
			a.log.Debug("feishu: gate rejected", "reason", result.Reason, "chat", chatID, "user", userID)
			return nil
		}
	}

	// Step 8: Abort fast-path.
	if IsAbortCommand(text) {
		a.chatQueue.Abort(chatID)
		return nil
	}

	// Step 9: All message processing (including control commands) goes through
	// chatQueue to serialize execution per chatID, preventing races between
	// reset's Terminate→Start and the next message's Input() call.
	replyToMsgID := parentID
	if replyToMsgID == "" {
		replyToMsgID = rootID
	}

	return a.chatQueue.Enqueue(chatID, func(qtx context.Context) error {
		// Help command - reply directly without involving the worker.
		if messaging.IsHelpCommand(text) {
			_ = a.replyMessage(qtx, messageID, messaging.HelpText(), false)
			return nil
		}

		// Control command detection (natural language + /command).
		if result := messaging.ParseControlCommand(text); result != nil {
			a.handleTextControlCommand(qtx, chatID, userID, threadKey, messageID, result)
			return nil
		}

		// Worker command detection (slash + $ natural language).
		// Only intercept structured commands (context, mcp, model, perm).
		// Passthrough commands (compact, clear, rewind, effort, commit)
		// fall through to normal input — they aren't supported in stream-json mode.
		if cmdResult := messaging.ParseWorkerCommand(text); cmdResult != nil && !cmdResult.Command.IsPassthrough() {
			a.handleTextWorkerCommand(qtx, chatID, chatType, userID, threadKey, messageID, replyToMsgID, cmdResult)
			return nil
		}

		a.log.Debug("feishu: handling message",
			"chat_type", chatType,
			"chat", chatID,
			"user", userID,
			"thread_key", threadKey,
			"text_len", len(text),
		)

		return a.handleTextMessage(qtx, messageID, chatID, chatType, userID, text, threadKey, replyToMsgID)
	})
}

func isBotMentioned(mentions []*larkim.MentionEvent, botOpenID string) bool {
	if botOpenID == "" {
		return false
	}
	for _, m := range mentions {
		if m.Id != nil && m.Id.OpenId != nil && *m.Id.OpenId == botOpenID {
			return true
		}
	}
	return false
}

func (a *Adapter) handleTextMessage(ctx context.Context, platformMsgID, channelID, chatType, userID, text, threadKey, replyToMsgID string) error {
	if a.bridge == nil {
		return nil
	}

	envelope := a.bridge.MakeFeishuEnvelope(channelID, threadKey, userID, text)
	if envelope == nil {
		return fmt.Errorf("feishu: failed to build envelope")
	}

	if md, ok := envelope.Event.Data.(map[string]any); ok {
		md["platform_msg_id"] = platformMsgID
		md["reply_to_msg_id"] = replyToMsgID
	}

	// Pre-create conn so its fields are ready before the bridge forwards to the handler.
	conn := a.GetOrCreateConn(channelID, threadKey)

	// Check if this text is a response to a pending interaction.
	if a.checkPendingInteraction(ctx, text, conn) {
		return nil // text consumed as interaction response
	}
	conn.mu.Lock()
	// Clean up stale reactions from previous message before switching platformMsgID.
	if conn.platformMsgID != "" && conn.platformMsgID != platformMsgID {
		if conn.toolRid != "" {
			_ = a.removeReaction(context.Background(), conn.platformMsgID, conn.toolRid)
			conn.toolRid = ""
		}
		if conn.typingRid != "" {
			_ = a.RemoveTypingIndicator(context.Background(), conn.platformMsgID, conn.typingRid)
			conn.typingRid = ""
		}
	}
	conn.replyToMsgID = replyToMsgID
	conn.platformMsgID = platformMsgID
	conn.chatType = chatType
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	// Typing indicator: add reaction to user's message (non-blocking, failure is non-fatal).
	if platformMsgID != "" {
		if rid, err := a.AddTypingIndicator(ctx, platformMsgID); err == nil && rid != "" {
			conn.SetTypingReactionID(rid)
		} else if err != nil {
			a.log.Debug("feishu: typing indicator failed (non-fatal)", "err", err)
		}
	}

	// Prepare streaming controller (card is lazily created on first content).
	if a.larkClient != nil && a.rateLimiter != nil {
		ctrl := NewStreamingCardController(a.larkClient, a.rateLimiter, a.log)
		conn.EnableStreaming(ctrl)
	}

	err := a.bridge.Handle(ctx, envelope, conn)
	if err != nil && conn != nil {
		notifyErr := a.sendTextMessage(context.Background(), channelID,
			"抱歉，处理您的请求时遇到问题，请稍后重试。")
		if notifyErr != nil {
			a.log.Warn("feishu: failed to send error notification",
				"chat", channelID, "original_err", err, "notify_err", notifyErr)
		}
	}
	return err
}

func (a *Adapter) HandleTextMessage(ctx context.Context, platformMsgID, channelID, teamID, threadTS, userID, text string) error {
	return a.handleTextMessage(ctx, platformMsgID, channelID, "p2p", userID, text, "", "")
}

func (a *Adapter) GetOrCreateConn(chatID, threadKey string) *FeishuConn {
	key := chatID + "#" + threadKey
	a.mu.Lock()
	defer a.mu.Unlock()

	if conn, ok := a.activeConns[key]; ok {
		return conn
	}

	conn := NewFeishuConn(a, chatID, threadKey)
	a.activeConns[key] = conn
	return conn
}

func (a *Adapter) Close(ctx context.Context) error {
	if a.log != nil {
		a.log.Info("feishu: adapter closing")
	}

	// Shut down persistent STT subprocess if present.
	if closer, ok := a.transcriber.(stt.Closer); ok {
		if err := closer.Close(ctx); err != nil {
			a.log.Warn("feishu: transcriber close", "err", err)
		}
	}

	// Close chat queue to drain all worker goroutines.
	if a.chatQueue != nil {
		a.chatQueue.Close()
	}

	a.mu.Lock()

	// Collect conns, clear map, then close outside lock to avoid deadlock.
	// FeishuConn.Close() acquires a.mu, so we must not hold it during conn.Close().
	var conns []*FeishuConn
	for _, conn := range a.activeConns {
		conns = append(conns, conn)
	}
	a.activeConns = nil
	a.dedup = nil
	close(a.dedupDone)
	a.dedupWg.Wait()
	if a.rateLimiter != nil {
		a.rateLimiter.Stop()
		a.rateLimiter = nil
	}
	a.mu.Unlock()

	// Close conns outside lock to prevent deadlock with FeishuConn.Close().
	for _, conn := range conns {
		_ = conn.Close()
	}

	return nil
}

type FeishuConn struct {
	adapter   *Adapter
	chatID    string
	threadKey string

	mu            sync.RWMutex
	chatType      string
	replyToMsgID  string
	platformMsgID string
	sessionID     string
	streamCtrl    *StreamingCardController
	typingRid     string
	toolRid       string
	toolEmoji     string    // current timeline emoji, for dedup
	startedAt     time.Time // when the user sent the current message
}

func NewFeishuConn(adapter *Adapter, chatID, threadKey string) *FeishuConn {
	return &FeishuConn{adapter: adapter, chatID: chatID, threadKey: threadKey}
}

func (c *FeishuConn) EnableStreaming(ctrl *StreamingCardController) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streamCtrl = ctrl
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
		c.adapter.log.Debug("feishu: processing reaction failed (non-fatal)", "err", err)
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

// cycleReaction replaces the current tool reaction with a new one.
// The typing indicator is kept alive throughout the session and only
// removed on done/close to prevent message flicker from repeated add/remove.
func (c *FeishuConn) cycleReaction(ctx context.Context, emoji string) {
	c.mu.Lock()
	toolRid := c.toolRid
	toolEmoji := c.toolEmoji
	platformMsgID := c.platformMsgID
	c.mu.Unlock()

	if platformMsgID == "" {
		return
	}

	// Dedup: skip API calls if the emoji hasn't changed.
	if toolEmoji == emoji {
		return
	}

	// Remove previous tool reaction only.
	if toolRid != "" {
		_ = c.adapter.removeReaction(ctx, platformMsgID, toolRid)
	}

	// Add new tool reaction.
	if rid, err := c.adapter.addReaction(ctx, platformMsgID, emoji); err == nil && rid != "" {
		c.mu.Lock()
		c.toolRid = rid
		c.toolEmoji = emoji
		c.mu.Unlock()
	} else if err != nil {
		c.adapter.log.Debug("feishu: tool reaction failed (non-fatal)", "err", err)
	}
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

	// Handle done event before extractResponseText (which returns false for done).
	if env.Event.Type == events.Done {
		c.mu.Lock()
		streamCtrl := c.streamCtrl
		typingRid := c.typingRid
		toolRid := c.toolRid
		platformMsgID := c.platformMsgID
		if typingRid != "" {
			_ = c.adapter.RemoveTypingIndicator(ctx, platformMsgID, typingRid)
			c.typingRid = ""
		}
		if toolRid != "" {
			_ = c.adapter.removeReaction(ctx, platformMsgID, toolRid)
			c.toolRid = ""
			c.toolEmoji = ""
		}
		c.mu.Unlock()

		if streamCtrl != nil && streamCtrl.IsCreated() {
			return streamCtrl.Close(ctx)
		}
		return nil
	}

	// Handle error events: clean up streaming card so it doesn't remain stale
	// (e.g., TURN_TIMEOUT sent by bridge.go timeout handler before worker exits).
	if env.Event.Type == events.Error {
		c.mu.Lock()
		streamCtrl := c.streamCtrl
		typingRid := c.typingRid
		toolRid := c.toolRid
		platformMsgID := c.platformMsgID
		if typingRid != "" {
			_ = c.adapter.RemoveTypingIndicator(ctx, platformMsgID, typingRid)
			c.typingRid = ""
		}
		if toolRid != "" {
			_ = c.adapter.removeReaction(ctx, platformMsgID, toolRid)
			c.toolRid = ""
			c.toolEmoji = ""
		}
		c.mu.Unlock()
		if streamCtrl != nil && streamCtrl.IsCreated() {
			_ = streamCtrl.Close(ctx)
		}
		// Don't return here — let it fall through to extractResponseText below.
	}

	// Handle tool_call: update reaction to timeline emoji.
	if env.Event.Type == events.ToolCall {
		c.mu.RLock()
		elapsed := time.Since(c.startedAt)
		c.mu.RUnlock()
		c.cycleReaction(ctx, timelineEmoji(elapsed))
		return nil
	}

	// Handle tool_result: update reaction to timeline emoji.
	if env.Event.Type == events.ToolResult {
		c.mu.RLock()
		elapsed := time.Since(c.startedAt)
		c.mu.RUnlock()
		c.cycleReaction(ctx, timelineEmoji(elapsed))
		return nil
	}

	// Handle interaction request events.
	switch env.Event.Type {
	case events.PermissionRequest:
		rid := c.setProcessingReaction(ctx)
		pErr := c.sendPermissionRequest(ctx, env)
		c.clearProcessingReaction(ctx, rid)
		return pErr
	case events.QuestionRequest:
		rid := c.setProcessingReaction(ctx)
		qErr := c.sendQuestionRequest(ctx, env)
		c.clearProcessingReaction(ctx, rid)
		return qErr
	case events.ElicitationRequest:
		rid := c.setProcessingReaction(ctx)
		eErr := c.sendElicitationRequest(ctx, env)
		c.clearProcessingReaction(ctx, rid)
		return eErr
	case events.ContextUsage:
		rid := c.setProcessingReaction(ctx)
		cuErr := c.sendContextUsage(ctx, env)
		c.clearProcessingReaction(ctx, rid)
		return cuErr
	case events.MCPStatus:
		rid := c.setProcessingReaction(ctx)
		mErr := c.sendMCPStatus(ctx, env)
		c.clearProcessingReaction(ctx, rid)
		return mErr
	case events.SkillsList:
		rid := c.setProcessingReaction(ctx)
		slErr := c.sendSkillsList(ctx, env)
		c.clearProcessingReaction(ctx, rid)
		return slErr
	}

	// Cancel pending interactions on done/error.
	if env.Event.Type == events.Done || env.Event.Type == events.Error {
		c.adapter.interactions.CancelAll(env.SessionID)
	}

	text, ok := extractResponseText(env)
	if !ok {
		return nil
	}
	if env.Event.Type == events.MessageDelta && text != "" {
		text += "\n\n"
	}
	text = StripInvalidImageKeys(text)

	c.mu.Lock()
	chatID := c.chatID
	replyToMsgID := c.replyToMsgID
	streamCtrl := c.streamCtrl
	chatType := c.chatType
	c.mu.Unlock()

	c.adapter.log.Debug("feishu: WriteCtx sending",
		"event_type", env.Event.Type,
		"chat", chatID,
		"reply_to", replyToMsgID,
		"text_len", len(text),
	)

	// TTL rotation: proactively replace expired streaming cards before
	// Feishu's 10-minute server limit kicks in.
	if streamCtrl != nil && streamCtrl.IsCreated() && streamCtrl.Expired() {
		oldMsgID := streamCtrl.MsgID()
		abortCtx, abortCancel := context.WithTimeout(context.Background(), 10*time.Second)
		go func() {
			defer abortCancel()
			_ = streamCtrl.Abort(abortCtx)
		}()
		c.adapter.log.Info("feishu: streaming card rotated",
			"old_msg_id", oldMsgID)

		newCtrl := NewStreamingCardController(c.adapter.larkClient, c.adapter.rateLimiter, c.adapter.log)
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
				c.adapter.log.Warn("feishu: streaming card init failed, falling back to static", "err", err)
				c.mu.Lock()
				c.streamCtrl = nil
				c.mu.Unlock()
			} else {
				return nil
			}
		} else {
			// Subsequent content: write + flush.
			if err := streamCtrl.Write(text); err != nil {
				// Streaming failed — close and fall back to static delivery.
				c.adapter.log.Warn("feishu: streaming write failed, falling back to static", "err", err)
				_ = streamCtrl.Abort(context.Background())
				c.mu.Lock()
				c.streamCtrl = nil
				c.mu.Unlock()
			} else {
				return streamCtrl.Flush(ctx)
			}
		}
	}

	if replyToMsgID != "" {
		return c.adapter.replyMessage(ctx, replyToMsgID, OptimizeMarkdownStyle(SanitizeForCard(text)), false)
	}
	return c.adapter.sendTextMessage(ctx, chatID, OptimizeMarkdownStyle(SanitizeForCard(text)))
}

func (c *FeishuConn) Close() error {
	c.mu.Lock()
	streamCtrl := c.streamCtrl
	typingRid := c.typingRid
	toolRid := c.toolRid
	platformMsgID := c.platformMsgID
	c.streamCtrl = nil
	c.typingRid = ""
	c.toolRid = ""
	c.mu.Unlock()

	// Best-effort cleanup: Abort uses Background since this Close path is
	// called during shutdown (no deadline needed for graceful teardown).
	if streamCtrl != nil {
		_ = streamCtrl.Abort(context.Background())
	}
	if typingRid != "" && c.adapter.larkClient != nil {
		_ = c.adapter.RemoveTypingIndicator(context.Background(), platformMsgID, typingRid)
	}
	if toolRid != "" && c.adapter.larkClient != nil {
		_ = c.adapter.removeReaction(context.Background(), platformMsgID, toolRid)
	}
	c.adapter.mu.Lock()
	delete(c.adapter.activeConns, c.chatID+"#"+c.threadKey)
	c.adapter.mu.Unlock()
	return nil
}

func (c *FeishuConn) sendContextUsage(ctx context.Context, env *events.Envelope) error {
	var d events.ContextUsageData
	switch v := env.Event.Data.(type) {
	case events.ContextUsageData:
		d = v
	case map[string]any:
		raw, _ := json.Marshal(v)
		_ = json.Unmarshal(raw, &d)
	default:
		return nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "📊 Context Usage — %d%% (%d / %d)", d.Percentage, d.TotalTokens, d.MaxTokens)
	if d.Model != "" {
		fmt.Fprintf(&sb, "\n🤖 Model: %s", d.Model)
	}
	if len(d.Categories) > 0 {
		var catParts []string
		for _, cat := range d.Categories {
			if d.Skills.Total > 0 && strings.EqualFold(cat.Name, "Skills") {
				continue
			}
			catParts = append(catParts, fmt.Sprintf("%s: %d", cat.Name, cat.Tokens))
		}
		if len(catParts) > 0 {
			sb.WriteString("\n📂 " + strings.Join(catParts, " · "))
		}
	}
	var extras []string
	if d.MemoryFiles > 0 {
		extras = append(extras, fmt.Sprintf("📁 %d memory files", d.MemoryFiles))
	}
	if d.MCPTools > 0 {
		extras = append(extras, fmt.Sprintf("🔧 %d MCP tools", d.MCPTools))
	}
	if d.Agents > 0 {
		extras = append(extras, fmt.Sprintf("🤖 %d agents", d.Agents))
	}
	if d.Skills.Total > 0 {
		skillsStr := fmt.Sprintf("⚡ %d skills (%d included, %d tokens)", d.Skills.Total, d.Skills.Included, d.Skills.Tokens)
		if len(d.Skills.Names) > 0 {
			skillsStr += "\n📜 " + strings.Join(d.Skills.Names, ", ")
		}
		extras = append(extras, skillsStr)
	}
	if len(extras) > 0 {
		sb.WriteString("\n" + strings.Join(extras, " · "))
	}

	c.mu.RLock()
	chatID := c.chatID
	replyToMsgID := c.replyToMsgID
	c.mu.RUnlock()

	if replyToMsgID != "" {
		return c.adapter.replyMessage(ctx, replyToMsgID, sb.String(), false)
	}
	return c.adapter.sendTextMessage(ctx, chatID, sb.String())
}

func (c *FeishuConn) sendMCPStatus(ctx context.Context, env *events.Envelope) error {
	var d events.MCPStatusData
	switch v := env.Event.Data.(type) {
	case events.MCPStatusData:
		d = v
	case map[string]any:
		raw, _ := json.Marshal(v)
		_ = json.Unmarshal(raw, &d)
	default:
		return nil
	}

	var sb strings.Builder
	sb.WriteString("🔌 MCP Server Status")
	for _, s := range d.Servers {
		icon := "✅"
		if s.Status != "connected" && s.Status != "ok" {
			icon = "❌"
		}
		fmt.Fprintf(&sb, "\n%s %s — %s", icon, s.Name, s.Status)
	}

	c.mu.RLock()
	chatID := c.chatID
	replyToMsgID := c.replyToMsgID
	c.mu.RUnlock()

	if replyToMsgID != "" {
		return c.adapter.replyMessage(ctx, replyToMsgID, sb.String(), false)
	}
	return c.adapter.sendTextMessage(ctx, chatID, sb.String())
}

var _ messaging.PlatformConn = (*FeishuConn)(nil)

// handleTextControlCommand sends a control event derived from a text message
// through the bridge, then sends feedback via card message.
func (a *Adapter) handleTextControlCommand(ctx context.Context, chatID, userID, threadKey, platformMsgID string, result *messaging.ControlCommandResult) {
	envelope := a.bridge.MakeFeishuEnvelope(chatID, threadKey, userID, "")
	if envelope == nil {
		a.log.Error("feishu: text control command failed to derive session", "action", result.Label)
		return
	}

	ctrlEnv := &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		SessionID: envelope.SessionID,
		Event: events.Event{
			Type: events.Control,
			Data: events.ControlData{Action: result.Action},
		},
		OwnerID: userID,
	}

	conn := a.GetOrCreateConn(chatID, threadKey)
	if err := a.bridge.Handle(ctx, ctrlEnv, conn); err != nil {
		a.log.Error("feishu: text control command failed", "action", result.Label, "err", err)
		_ = a.replyMessage(ctx, threadKey, fmt.Sprintf("❌ 执行 %s 失败。", result.Label), false)
		return
	}

	a.log.Info("feishu: text control command sent", "action", result.Label, "user", userID, "session_id", envelope.SessionID)

	// Reset/GC kills the worker without a guaranteed done event, so stale
	// pending interactions (permission/question/elicitation) may survive.
	// Cancel them now to prevent the next user message from being consumed
	// by checkPendingInteraction as a response to a dead interaction.
	if result.Action == events.ControlActionReset || result.Action == events.ControlActionGC {
		a.interactions.CancelAll(envelope.SessionID)
		// Abort any active streaming card — GC/Reset kills the worker without a
		// done event, so the card would otherwise remain in streaming state.
		conn.mu.RLock()
		ctrl := conn.streamCtrl
		conn.mu.RUnlock()
		if ctrl != nil {
			_ = ctrl.Abort(ctx)
		}
	}

	if platformMsgID != "" {
		_ = a.replyMessage(ctx, platformMsgID, controlFeedbackMessageCN(result.Action), false)
	} else {
		_ = a.sendTextMessage(ctx, chatID, controlFeedbackMessageCN(result.Action))
	}
}

func (a *Adapter) handleTextWorkerCommand(ctx context.Context, chatID, chatType, userID, threadKey, platformMsgID, replyToMsgID string, result *messaging.WorkerCommandResult) {
	envelope := a.bridge.MakeFeishuEnvelope(chatID, threadKey, userID, "")
	if envelope == nil {
		a.log.Error("feishu: worker command failed to derive session", "command", result.Label)
		return
	}

	cmdEnv := &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		SessionID: envelope.SessionID,
		Event: events.Event{
			Type: events.WorkerCmd,
			Data: events.WorkerCommandData{
				Command: result.Command,
				Args:    result.Args,
				Extra:   result.Extra,
			},
		},
		OwnerID: userID,
	}

	conn := a.GetOrCreateConn(chatID, threadKey)

	// Set conn fields for async response delivery.
	conn.mu.Lock()
	conn.platformMsgID = platformMsgID
	conn.replyToMsgID = replyToMsgID
	conn.chatType = chatType
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	if err := a.bridge.Handle(ctx, cmdEnv, conn); err != nil {
		a.log.Error("feishu: worker command failed", "command", result.Label, "err", err)
		if platformMsgID != "" {
			_ = a.replyMessage(ctx, platformMsgID, fmt.Sprintf("❌ 执行 %s 失败。", result.Label), false)
		} else {
			_ = a.sendTextMessage(ctx, chatID, fmt.Sprintf("❌ 执行 %s 失败。", result.Label))
		}
		return
	}

	a.log.Info("feishu: worker command sent", "command", result.Label, "user", userID, "session_id", envelope.SessionID)
}

func controlFeedbackMessageCN(action events.ControlAction) string {
	switch action {
	case events.ControlActionGC:
		return "✅ 会话已休眠，发消息即可恢复。"
	case events.ControlActionReset:
		return "✅ 上下文已重置。"
	default:
		return "✅ 已完成。"
	}
}

func (a *Adapter) sendTextMessage(ctx context.Context, chatID, text string) error {
	if a.larkClient == nil {
		return fmt.Errorf("feishu: lark client not initialized")
	}

	cardJSON := buildCardContent(text)
	preview := cardJSON
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	a.log.Debug("feishu: sending card message", "chat", chatID, "content_len", len(cardJSON), "content_preview", preview)

	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType(larkim.MsgTypeInteractive).
		Content(cardJSON).
		Build()

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(body).
		Build()

	resp, err := a.larkClient.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu: send message: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: send message failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	a.log.Debug("feishu: message sent", "chat", chatID)
	return nil
}

//nolint:unparam // replyInThread reserved for future thread reply support
func (a *Adapter) replyMessage(ctx context.Context, messageID, content string, replyInThread bool) error {
	if a.larkClient == nil {
		return fmt.Errorf("feishu: lark client not initialized")
	}

	cardJSON := buildCardContent(content)
	preview := cardJSON
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	a.log.Debug("feishu: sending reply card", "msg_id", messageID, "content_len", len(cardJSON), "content_preview", preview)
	body := larkim.NewReplyMessageReqBodyBuilder().
		MsgType(larkim.MsgTypeInteractive).
		Content(cardJSON).
		ReplyInThread(replyInThread).
		Build()

	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(body).
		Build()

	resp, err := a.larkClient.Im.V1.Message.Reply(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu: reply message: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: reply message failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	a.log.Debug("feishu: reply message sent", "msg_id", messageID, "content_len", len(content))
	return nil
}

const mediaMaxSize = 10 * 1024 * 1024 // 10 MB

// mediaTypeToResourceType maps our internal media types to Feishu resource types.
var mediaTypeToResourceType = map[string]string{
	"image":   "image",
	"file":    "file",
	"audio":   "file",
	"video":   "file",
	"sticker": "file",
}

// mediaExtByType provides fallback extensions when Content-Type is unavailable.
var mediaExtByType = map[string]string{
	"image":   ".jpg",
	"file":    "",
	"audio":   ".opus",
	"video":   ".mp4",
	"sticker": ".gif",
}

// fetchMediaBytes downloads media content to memory without writing to disk.
func (a *Adapter) fetchMediaBytes(ctx context.Context, media *MediaInfo) ([]byte, string, error) {
	if a.larkClient == nil || media == nil || media.MessageID == "" || media.Key == "" {
		return nil, "", fmt.Errorf("feishu: missing lark client, media, messageID, or key")
	}

	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(media.MessageID).
		FileKey(media.Key).
		Type(mediaTypeToResourceType[media.Type]).
		Build()

	// Add a 30-second timeout for the media download.
	downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := a.larkClient.Im.V1.MessageResource.Get(downloadCtx, req)
	if err != nil {
		return nil, "", fmt.Errorf("feishu: download %s: %w", media.Type, err)
	}
	if !resp.Success() {
		return nil, "", fmt.Errorf("feishu: download %s failed: code=%d msg=%s", media.Type, resp.Code, resp.Msg)
	}

	ext := mediaExtByType[media.Type]
	if resp.FileName != "" {
		ext = filepath.Ext(resp.FileName)
	} else if ct := resp.ApiResp.Header.Get("Content-Type"); ct != "" {
		ext = mimeExt(ct)
	}

	data, err := io.ReadAll(resp.File)
	if err != nil {
		return nil, "", fmt.Errorf("feishu: read file content: %w", err)
	}
	if len(data) > mediaMaxSize {
		return nil, "", fmt.Errorf("feishu: file too large: %d > %d bytes", len(data), mediaMaxSize)
	}

	a.log.Debug("feishu: media fetched", "type", media.Type, "key", media.Key, "size", len(data))
	return data, ext, nil
}

// saveMediaBytes writes media data to disk and returns the file path.
func (a *Adapter) saveMediaBytes(data []byte, media *MediaInfo, ext string) (string, error) {
	mediaDir := "/tmp/hotplex/media/" + media.Type + "s"
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return "", fmt.Errorf("feishu: create media dir: %w", err)
	}

	filename := media.Key + ext
	if media.Name != "" {
		filename = media.Key + "_" + media.Name
	}
	filePath := filepath.Join(mediaDir, filename)

	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return "", fmt.Errorf("feishu: write file: %w", err)
	}

	a.log.Debug("feishu: media saved", "type", media.Type, "key", media.Key, "path", filePath)
	return filePath, nil
}

// downloadMedia fetches media and writes to disk. Convenience wrapper.
func (a *Adapter) downloadMedia(ctx context.Context, media *MediaInfo) (string, error) {
	data, ext, err := a.fetchMediaBytes(ctx, media)
	if err != nil {
		return "", err
	}
	return a.saveMediaBytes(data, media, ext)
}

// mimeExt maps MIME type to common file extension.
func mimeExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "audio/opus":
		return ".opus"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	default:
		return ""
	}
}

func (a *Adapter) dedupCleanupLoop() {
	defer a.dedupWg.Done()
	ticker := time.NewTicker(dedupSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.dedupDone:
			return
		case <-ticker.C:
			a.dedup.Sweep()
		}
	}
}

func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

type textContent struct {
	Text string `json:"text"`
}

// buildCardContent builds a Feishu interactive card JSON using CardKit v2 format.
// schema:"2.0" is required for the "markdown" tag to work with full markdown rendering.
// Uses json.NewEncoder with SetEscapeHTML(false) to preserve HTML entities.
func buildCardContent(text string) string {
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{"wide_screen_mode": true},
		"body": map[string]any{
			"elements": []map[string]any{
				{"tag": "markdown", "content": text},
			},
		},
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(card)
	return strings.TrimRight(buf.String(), "\n")
}

func extractTextFromContent(content string) string {
	if content == "" {
		return ""
	}
	var tc textContent
	if err := json.Unmarshal([]byte(content), &tc); err != nil {
		return ""
	}
	return tc.Text
}
