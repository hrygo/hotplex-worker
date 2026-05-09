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

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/messaging/stt"
	"github.com/hrygo/hotplex/pkg/events"
)

func init() {
	messaging.Register(messaging.PlatformFeishu, func(log *slog.Logger) messaging.PlatformAdapterInterface {
		return &Adapter{
			BaseAdapter: messaging.BaseAdapter[*FeishuConn]{
				PlatformAdapter: messaging.PlatformAdapter{Log: log.With("channel", string(messaging.PlatformFeishu))},
			},
		}
	})
}

type Adapter struct {
	messaging.BaseAdapter[*FeishuConn]

	appID              string
	appSecret          string
	wsClient           *ws.Client
	larkClient         *lark.Client
	botOpenID          string
	transcriber        Transcriber
	turnSummaryEnabled bool
	ttsPipeline        *TTSPipeline
	botName            string

	mu          sync.RWMutex
	chatQueue   *ChatQueue
	rateLimiter *FeishuRateLimiter
}

func (a *Adapter) Platform() messaging.PlatformType { return messaging.PlatformFeishu }

var _ messaging.PlatformAdapterInterface = (*Adapter)(nil)

func (a *Adapter) GetBotID() string { return a.botOpenID }

func (a *Adapter) ConfigureWith(config messaging.AdapterConfig) error {
	// Call base to set hub/sm/handler/bridge.
	_ = a.PlatformAdapter.ConfigureWith(config)

	// Shared adapter state (gate, backoff delays).
	a.ConfigureShared(config)

	// Feishu-specific: credentials.
	a.appID = config.ExtrasString("app_id")
	a.appSecret = config.ExtrasString("app_secret")

	// Platform-specific extras.
	if t, ok := config.Extras["transcriber"].(Transcriber); ok && t != nil {
		a.transcriber = t
	}
	if v, ok := config.Extras["turn_summary_enabled"].(bool); ok {
		a.turnSummaryEnabled = v
	}
	if p, ok := config.Extras["tts_pipeline"].(*TTSPipeline); ok && p != nil {
		a.ttsPipeline = p
	}

	return nil
}

func (a *Adapter) Start(ctx context.Context) error {
	if !a.StartGuard() {
		a.Log.Warn("feishu: adapter already started, skipping")
		return nil
	}
	if a.appID == "" || a.appSecret == "" {
		return fmt.Errorf("feishu: appID and appSecret required")
	}

	a.InitSharedState()
	a.InitConnPool(func(key string) *FeishuConn {
		parts := strings.SplitN(key, "#", 2)
		threadKey := ""
		if len(parts) > 1 {
			threadKey = parts[1]
		}
		return NewFeishuConn(a, parts[0], threadKey, a.Bridge().WorkDir())
	})
	a.chatQueue = NewChatQueue(a.Log)
	a.rateLimiter = NewFeishuRateLimiter()
	a.rateLimiter.Start()

	a.larkClient = lark.NewClient(a.appID, a.appSecret,
		lark.WithLogger(SlogLogger{Logger: a.Log}),
	)

	if err := a.fetchBotInfo(ctx); err != nil {
		return fmt.Errorf("feishu: failed to resolve bot identity: %w", err)
	}

	a.Log.Info("feishu: starting WebSocket connection")
	go a.runWebSocket(ctx)

	return nil
}

func (a *Adapter) newEventHandler() *dispatcher.EventDispatcher {
	return dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) (err error) {
			defer func() {
				if r := recover(); r != nil {
					a.Log.Error("feishu: panic in message handler", "panic", r, "stack", string(debug.Stack()))
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
	defer func() {
		if r := recover(); r != nil {
			a.Log.Error("feishu: panic in runWebSocket", "panic", r, "stack", string(debug.Stack()))
		}
	}()
	baseDelay := a.BackoffBaseDelay
	if baseDelay <= 0 {
		baseDelay = 2 * time.Second
	}
	maxDelay := a.BackoffMaxDelay
	if maxDelay <= 0 {
		maxDelay = 60 * time.Second
	}
	backoff := messaging.NewReconnectBackoff(baseDelay, maxDelay)

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
			ws.WithLogger(SlogLogger{Logger: a.Log}),
		)
		a.mu.Lock()
		a.wsClient = client
		a.mu.Unlock()

		a.Log.Info("feishu: starting WebSocket connection", "attempt", attempt)

		if err := client.Start(ctx); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.Next()):
				a.Log.Warn("feishu: WebSocket disconnected, reconnecting...",
					"err", err, "attempt", attempt)
				attempt++
				continue
			}
		}

		backoff.Reset()
		attempt = 1
		a.Log.Info("feishu: WebSocket closed cleanly, reconnecting...")
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff.Next()):
		}
	}
}

func (a *Adapter) fetchBotInfo(ctx context.Context) error {
	botCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := a.larkClient.Get(botCtx, "/open-apis/bot/v3/info", nil, "tenant_access_token")
	if err != nil {
		return fmt.Errorf("bot info API: %w", err)
	}

	body := resp.RawBody
	if len(body) == 0 {
		return fmt.Errorf("bot info API: empty response body")
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Bot  struct {
			OpenID  string `json:"open_id"`
			AppName string `json:"app_name"`
		} `json:"bot"`
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
	if result.Bot.AppName != "" {
		a.botName = result.Bot.AppName
	} else {
		a.botName = "HotPlex"
	}
	a.Log.Info("feishu: bot identity resolved", "open_id", a.botOpenID, "name", a.botName)
	return nil
}

// resolveBotName returns the bot display name (set during Start by fetchBotInfo).
// Falls back to "HotPlex" if the name was not resolved.
func (a *Adapter) resolveBotName() string {
	if a.botName == "" {
		return "HotPlex"
	}
	return a.botName
}

// processMediaAttachments downloads media files and runs STT on audio,
// returning file paths, transcriptions to be appended to the message text,
// and whether any audio was successfully transcribed (for TTS trigger).
func (a *Adapter) processMediaAttachments(ctx context.Context, medias []*MediaInfo) (paths, transcriptions []string, hasAudioTranscription bool) {
	for _, m := range medias {
		// Audio + STT: try transcription, conditionally skip disk write.
		if m.Type == "audio" && a.transcriber != nil {
			data, ext, fetchErr := a.fetchMediaBytes(ctx, m)
			if fetchErr != nil {
				a.Log.Warn("feishu: audio fetch failed", "key", m.Key, "err", fetchErr)
				continue
			}
			transcription, sttErr := a.transcriber.Transcribe(ctx, data)
			if sttErr == nil && transcription != "" {
				transcriptions = append(transcriptions, transcription)
				hasAudioTranscription = true
				// Pure cloud STT: skip disk write entirely.
				if !a.transcriber.RequiresDisk() {
					continue
				}
			} else if sttErr != nil {
				a.Log.Warn("feishu: stt failed, saving audio to disk", "err", sttErr)
			}
			// Local/fallback mode or STT failure: save to disk for the worker.
			path, saveErr := a.saveMediaBytes(data, m, ext)
			if saveErr != nil {
				a.Log.Warn("feishu: audio save failed", "err", saveErr)
				continue
			}
			paths = append(paths, path)
			continue
		}
		// Non-audio or no STT: download to disk.
		path, err := a.downloadMedia(ctx, m)
		if err != nil {
			a.Log.Warn("feishu: media download failed", "type", m.Type, "key", m.Key, "err", err)
			continue
		}
		if path != "" {
			paths = append(paths, path)
		}
	}
	return
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
	dedup := a.Dedup
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

	var hasVoice bool
	if len(medias) > 0 {
		var paths, transcriptions []string
		paths, transcriptions, hasVoice = a.processMediaAttachments(ctx, medias)
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
	if a.Gate != nil {
		if allowed, reason := a.Gate.Check(chatType == "p2p", userID, botMentioned); !allowed {
			a.Log.Debug("feishu: gate rejected", "reason", reason, "chat", chatID, "user", userID)
			return nil
		}
	}

	// Step 8: Abort fast-path.
	if messaging.IsAbortCommand(text) {
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
		cmd := messaging.DetectCommand(text)
		switch cmd.Action {
		case messaging.CmdHelp:
			_ = a.replyMessage(qtx, messageID, messaging.HelpText(), false)
			return nil
		case messaging.CmdControl:
			a.handleTextControlCommand(qtx, chatID, userID, threadKey, messageID, cmd.Control)
			return nil
		case messaging.CmdWorker:
			a.handleTextWorkerCommand(qtx, chatID, chatType, userID, threadKey, messageID, replyToMsgID, cmd.Worker)
			return nil
		}

		a.Log.Debug("feishu: handling message",
			"chat_type", chatType,
			"chat", chatID,
			"user", userID,
			"thread_key", threadKey,
			"text_len", len(text),
		)

		return a.handleTextMessage(qtx, messageID, chatID, chatType, userID, text, threadKey, replyToMsgID, hasVoice)
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

func (a *Adapter) handleTextMessage(ctx context.Context, platformMsgID, channelID, chatType, userID, text, threadKey, replyToMsgID string, voiceTriggered bool) error {
	if a.Bridge() == nil {
		return nil
	}

	conn := a.GetOrCreateConn(channelID, threadKey)

	if voiceTriggered {
		conn.voiceTriggered.Store(true)
	}

	envelope := a.Bridge().MakeFeishuEnvelope(channelID, threadKey, userID, text, conn.WorkDir(), a.botOpenID)
	if envelope == nil {
		return fmt.Errorf("feishu: failed to build envelope")
	}

	if md, ok := envelope.Event.Data.(map[string]any); ok {
		md["platform_msg_id"] = platformMsgID
		md["reply_to_msg_id"] = replyToMsgID
	}

	// Check if this text is a response to a pending interaction.
	if a.checkPendingInteraction(ctx, text, userID, conn) {
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
			a.Log.Debug("feishu: typing indicator failed (non-fatal)", "err", err)
		}
	}

	// Prepare streaming controller (card is lazily created on first content).
	if a.larkClient != nil && a.rateLimiter != nil {
		turnNum, model, branch, workDir := conn.turnHeaderMeta()
		ctrl := NewStreamingCardController(a.larkClient, a.rateLimiter, a.Log, a.resolveBotName(), turnNum+1, model, branch, workDir)
		conn.EnableStreaming(ctrl)
	}

	err := a.Bridge().Handle(ctx, envelope, conn)
	if err != nil && conn != nil {
		notifyErr := a.sendTextMessage(context.Background(), channelID,
			"抱歉，处理您的请求时遇到问题，请稍后重试。")
		if notifyErr != nil {
			a.Log.Warn("feishu: failed to send error notification",
				"chat", channelID, "original_err", err, "notify_err", notifyErr)
		}
	}
	return err
}

func (a *Adapter) HandleTextMessage(ctx context.Context, platformMsgID, channelID, teamID, threadTS, userID, text string) error {
	return a.handleTextMessage(ctx, platformMsgID, channelID, "p2p", userID, text, "", "", false)
}

func (a *Adapter) GetOrCreateConn(chatID, threadKey string) *FeishuConn {
	return a.BaseAdapter.GetOrCreateConn(chatID, threadKey)
}

func (a *Adapter) Close(ctx context.Context) error {
	if a.Log != nil {
		a.Log.Info("feishu: adapter closing")
	}

	// Shut down persistent STT subprocess if present.
	if closer, ok := a.transcriber.(stt.Closer); ok {
		if err := closer.Close(ctx); err != nil {
			a.Log.Warn("feishu: transcriber close", "err", err)
		}
	}

	// Close chat queue to drain all worker goroutines.
	if a.chatQueue != nil {
		a.chatQueue.Close()
	}

	// Drain conn pool — ConnPool manages its own lock, no deadlock with FeishuConn.Close().
	conns := a.DrainConns()

	a.mu.Lock()
	a.CloseSharedState()
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

	mu                sync.RWMutex
	chatType          string
	replyToMsgID      string
	platformMsgID     string
	sessionID         string
	streamCtrl        *StreamingCardController
	typingRid         string
	toolRid           string
	toolEmoji         string    // current timeline emoji, for dedup
	startedAt         time.Time // when the user sent the current message
	workDir           string    // current workDir identity for session key derivation
	turnCount         int       // cached from last Done event, 0 = first turn
	lastModel         string    // cached from last TurnSummaryData
	lastBranch        string    // cached from last TurnSummaryData
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
		c.adapter.Log.Debug("feishu: tool reaction failed (non-fatal)", "err", err)
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

	switch env.Event.Type {
	case events.Done:
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

		// Extract content before closing — Content() may return empty after Close().
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
				// Detach from request lifecycle but preserve trace context.
				ttsCtx, ttsCancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
				go func() {
					defer ttsCancel()
					c.adapter.ttsPipeline.Process(ttsCtx, fullText, chatID, replyID)
				}()
			}
			c.voiceTriggered.Store(false)
		}

		return closeErr
	case events.Error:
		streamCtrl := c.clearActiveIndicators(ctx)
		c.adapter.Interactions.CancelAll(env.SessionID)
		if streamCtrl != nil && streamCtrl.IsCreated() {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := streamCtrl.Close(closeCtx); err != nil {
				c.adapter.Log.Warn("feishu: failed to close streaming card on error", "err", err)
			}
			closeCancel()
		}
		// Extract error text and send as simple message
		if errMsg := messaging.ExtractErrorMessage(env); errMsg != "" {
			c.mu.RLock()
			platformMsgID := c.platformMsgID
			c.mu.RUnlock()
			if platformMsgID != "" {
				_ = c.adapter.replyMessage(ctx, platformMsgID, errMsg, false)
			}
		}
		return nil
	case events.ToolCall, events.ToolResult:
		c.mu.RLock()
		elapsed := time.Since(c.startedAt)
		c.mu.RUnlock()
		c.cycleReaction(ctx, timelineEmoji(elapsed))
		return nil
	case events.PermissionRequest:
		streamCtrl := c.clearActiveIndicators(ctx)
		if streamCtrl != nil && streamCtrl.IsCreated() {
			_ = streamCtrl.Close(ctx)
		}
		rid := c.setProcessingReaction(ctx)
		pErr := c.sendPermissionRequest(ctx, env)
		c.clearProcessingReaction(ctx, rid)
		return pErr
	case events.QuestionRequest:
		streamCtrl := c.clearActiveIndicators(ctx)
		if streamCtrl != nil && streamCtrl.IsCreated() {
			_ = streamCtrl.Close(ctx)
		}
		rid := c.setProcessingReaction(ctx)
		qErr := c.sendQuestionRequest(ctx, env)
		c.clearProcessingReaction(ctx, rid)
		return qErr
	case events.ElicitationRequest:
		streamCtrl := c.clearActiveIndicators(ctx)
		if streamCtrl != nil && streamCtrl.IsCreated() {
			_ = streamCtrl.Close(ctx)
		}
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
	case events.Message:
		// Handler/bridge-originated standalone messages (cd confirmation,
		// command feedback, help text, retry notifications). Workers send
		// message.delta for streaming content, not message, so these are
		// never duplicates of streamed output.
		var content string
		if msgData, ok := env.Event.Data.(events.MessageData); ok {
			content = msgData.Content
		} else if m, ok := env.Event.Data.(map[string]any); ok {
			content, _ = m["content"].(string)
		}
		if content == "" {
			return nil
		}
		content = OptimizeMarkdownStyle(SanitizeForCard(messaging.SanitizeText(content)))
		c.mu.RLock()
		replyToMsgID := c.replyToMsgID
		chatID := c.chatID
		c.mu.RUnlock()
		if replyToMsgID != "" {
			return c.adapter.replyMessage(ctx, replyToMsgID, content, false)
		}
		return c.adapter.sendTextMessage(ctx, chatID, content)
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
	if toolRid != "" && c.adapter.larkClient != nil {
		_ = c.adapter.removeReaction(context.Background(), platformMsgID, toolRid)
	}
	c.adapter.DeleteConn(c.chatID, c.threadKey)
	return nil
}

// clearActiveIndicators removes typing indicators and tool reactions,
// returning the stream controller for caller cleanup.
func (c *FeishuConn) clearActiveIndicators(ctx context.Context) *StreamingCardController {
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
	return streamCtrl
}

// writeContent delivers text content via streaming card or static fallback.
func (c *FeishuConn) writeContent(ctx context.Context, env *events.Envelope, text string) error {
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
		oldMsgID := streamCtrl.MsgID()
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		go func() {
			defer closeCancel()
			_ = streamCtrl.Close(closeCtx)
		}()
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
	c.mu.RLock()
	replyToMsgID := c.replyToMsgID
	chatID := c.chatID
	c.mu.RUnlock()
	var err error
	if replyToMsgID != "" {
		err = c.adapter.doReplyCard(sendCtx, replyToMsgID, cardJSON, false)
	} else {
		err = c.adapter.sendCardMessage(sendCtx, chatID, cardJSON)
	}
	if err != nil {
		c.adapter.Log.Warn("turn summary card send failed", "err", err)
		return
	}
	c.lastSummarySentMs.Store(time.Now().UnixMilli())
}

func (c *FeishuConn) sendContextUsage(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractContextUsageData(env)
	if err != nil {
		return nil
	}

	text := messaging.FormatCanonicalText(d)

	c.mu.RLock()
	chatID := c.chatID
	replyToMsgID := c.replyToMsgID
	c.mu.RUnlock()

	if replyToMsgID != "" {
		return c.adapter.replyMessage(ctx, replyToMsgID, text, false)
	}
	return c.adapter.sendTextMessage(ctx, chatID, text)
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
	conn := a.GetOrCreateConn(chatID, threadKey)
	envelope := a.Bridge().MakeFeishuEnvelope(chatID, threadKey, userID, "", conn.WorkDir(), a.botOpenID)
	if envelope == nil {
		a.Log.Warn("feishu: text control command failed to derive session", "action", result.Label)
		return
	}

	ctrlEnv := messaging.BuildControlEnvelope(result, envelope.SessionID, userID)

	// CD sends progress feedback before execution; other actions send completion feedback after.
	if result.Action == events.ControlActionCD {
		if platformMsgID != "" {
			_ = a.replyMessage(ctx, platformMsgID, controlFeedbackMessageCN(result.Action), false)
		} else {
			_ = a.sendTextMessage(ctx, chatID, controlFeedbackMessageCN(result.Action))
		}
	}

	if err := a.Bridge().Handle(ctx, ctrlEnv, conn); err != nil {
		a.Log.Warn("feishu: text control command failed", "action", result.Label, "err", err)
		// Provide user-friendly error message with details
		errMsg := fmt.Sprintf("❌ 执行 %s 失败：%s", result.Label, formatSecurityError(err))
		if replyErr := a.replyMessage(ctx, platformMsgID, errMsg, false); replyErr != nil {
			a.Log.Error("feishu: failed to send error message", "action", result.Label, "user", userID, "err", replyErr)
		}
		return
	}

	a.Log.Info("feishu: text control command sent", "action", result.Label, "user", userID, "session_id", envelope.SessionID)

	// After a successful CD, update conn's workDir so subsequent messages
	// derive the correct session ID for the target directory.
	if result.Action == events.ControlActionCD && result.Arg != "" {
		if expanded, err := config.ExpandAndAbs(result.Arg); err == nil {
			conn.SetWorkDir(expanded)
		}
	}

	// Reset/GC kills the worker without a guaranteed done event, so stale
	// pending interactions (permission/question/elicitation) may survive.
	// Cancel them now to prevent the next user message from being consumed
	// by checkPendingInteraction as a response to a dead interaction.
	if result.Action == events.ControlActionReset || result.Action == events.ControlActionGC {
		a.Interactions.CancelAll(envelope.SessionID)
		// Abort any active streaming card — GC/Reset kills the worker without a
		// done event, so the card would otherwise remain in streaming state.
		conn.mu.RLock()
		ctrl := conn.streamCtrl
		conn.mu.RUnlock()
		if ctrl != nil {
			_ = ctrl.Abort(ctx)
		}
		// Clear cached turn metadata so the next turn starts fresh.
		conn.mu.Lock()
		conn.turnCount = 0
		conn.lastModel = ""
		conn.lastBranch = ""
		conn.mu.Unlock()
	}

	// Completion feedback for non-CD actions (CD feedback was sent before execution).
	if result.Action != events.ControlActionCD {
		if platformMsgID != "" {
			_ = a.replyMessage(ctx, platformMsgID, controlFeedbackMessageCN(result.Action), false)
		} else {
			_ = a.sendTextMessage(ctx, chatID, controlFeedbackMessageCN(result.Action))
		}
	}
}

func (a *Adapter) handleTextWorkerCommand(ctx context.Context, chatID, chatType, userID, threadKey, platformMsgID, replyToMsgID string, result *messaging.WorkerCommandResult) {
	conn := a.GetOrCreateConn(chatID, threadKey)
	envelope := a.Bridge().MakeFeishuEnvelope(chatID, threadKey, userID, "", conn.WorkDir(), a.botOpenID)
	if envelope == nil {
		a.Log.Warn("feishu: worker command failed to derive session", "command", result.Label)
		return
	}

	cmdEnv := messaging.BuildWorkerCommandEnvelope(result, envelope.SessionID, userID)

	// Set conn fields for async response delivery.
	conn.mu.Lock()
	conn.platformMsgID = platformMsgID
	conn.replyToMsgID = replyToMsgID
	conn.chatType = chatType
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	if err := a.Bridge().Handle(ctx, cmdEnv, conn); err != nil {
		a.Log.Warn("feishu: worker command failed", "command", result.Label, "err", err)
		if platformMsgID != "" {
			_ = a.replyMessage(ctx, platformMsgID, fmt.Sprintf("❌ 执行 %s 失败。", result.Label), false)
		} else {
			_ = a.sendTextMessage(ctx, chatID, fmt.Sprintf("❌ 执行 %s 失败。", result.Label))
		}
		return
	}

	a.Log.Info("feishu: worker command sent", "command", result.Label, "user", userID, "session_id", envelope.SessionID)
}

func controlFeedbackMessageCN(action events.ControlAction) string {
	return messaging.ControlFeedbackMessage(action, messaging.ControlFeedbackCN, "✅ 已完成。")
}

func (a *Adapter) sendTextMessage(ctx context.Context, chatID, text string) error {
	if a.larkClient == nil {
		return fmt.Errorf("feishu: lark client not initialized")
	}

	cardJSON := buildCardContent(text, cardHeader{Title: a.resolveBotName()})
	preview := cardJSON
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	a.Log.Debug("feishu: sending card message", "chat", chatID, "content_len", len(cardJSON), "content_preview", preview)

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

	a.Log.Debug("feishu: message sent", "chat", chatID)
	return nil
}

//nolint:unparam // replyInThread reserved for future thread reply support
func (a *Adapter) replyMessage(ctx context.Context, messageID, content string, replyInThread bool) error {
	cardJSON := buildCardContent(content, cardHeader{Title: a.resolveBotName()})
	preview := cardJSON
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	a.Log.Debug("feishu: sending reply card", "msg_id", messageID, "content_len", len(cardJSON), "content_preview", preview)
	if err := a.doReplyCard(ctx, messageID, cardJSON, replyInThread); err != nil {
		return err
	}
	a.Log.Debug("feishu: reply message sent", "msg_id", messageID, "content_len", len(content))
	return nil
}

func (a *Adapter) doReplyCard(ctx context.Context, messageID, cardJSON string, replyInThread bool) error {
	if a.larkClient == nil {
		return fmt.Errorf("feishu: lark client not initialized")
	}
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
		return fmt.Errorf("feishu: reply card: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: reply card failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
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

	a.Log.Debug("feishu: media fetched", "type", media.Type, "key", media.Key, "size", len(data))
	return data, ext, nil
}

// saveMediaBytes writes media data to disk and returns the file path.
func (a *Adapter) saveMediaBytes(data []byte, media *MediaInfo, ext string) (string, error) {
	mediaDir := filepath.Join(config.TempBaseDir(), "media", media.Type+"s")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return "", fmt.Errorf("feishu: create media dir: %w", err)
	}

	filename := media.Key + ext
	if media.Name != "" {
		if base := filepath.Base(media.Name); base != "." && base != ".." && base != string(filepath.Separator) {
			filename = media.Key + "_" + base
		}
	}
	filePath := filepath.Join(mediaDir, filename)

	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return "", fmt.Errorf("feishu: write file: %w", err)
	}

	a.Log.Debug("feishu: media saved", "type", media.Type, "key", media.Key, "path", filePath)
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
func buildCardContent(text string, header cardHeader) string {
	return buildCard(header,
		map[string]any{"wide_screen_mode": true},
		[]map[string]any{{"tag": "markdown", "content": text}},
	)
}

// encodeCard serializes a CardKit v2 card to JSON with HTML escaping disabled.
func encodeCard(card map[string]any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(card)
	return strings.TrimRight(buf.String(), "\n")
}

// buildTurnSummaryCard builds a CardKit v2 card with column_set rows matching
// Slack's TableBlock layout: two columns per row (label | value).
func buildTurnSummaryCard(d messaging.TurnSummaryData, header cardHeader) string {
	fields := d.Fields()
	if len(fields) == 0 {
		return ""
	}

	// Skip fields already shown in header tags (Turn, Model, Dir, Branch).
	skipLabels := map[string]bool{"🔄 Turn": true, "🤖 Model": true, "📂 Dir": true, "🌿 Branch": true}
	var elements []map[string]any
	for _, f := range fields {
		if !skipLabels[f.Label] {
			elements = append(elements, tableRow(f.Label, f.Value))
		}
	}
	if len(elements) == 0 {
		return ""
	}

	return buildCard(header, map[string]any{"wide_screen_mode": true}, elements)
}

// tableRow creates a CardKit v2 column_set element with two weighted columns.
func tableRow(label, value string) map[string]any {
	return map[string]any{
		"tag": "column_set",
		"columns": []map[string]any{
			{
				"tag":      "column",
				"width":    "weighted",
				"weight":   1,
				"elements": []map[string]any{{"tag": "markdown", "content": "**" + label + "**"}},
			},
			{
				"tag":      "column",
				"width":    "weighted",
				"weight":   3,
				"elements": []map[string]any{{"tag": "markdown", "content": value}},
			},
		},
	}
}

// formatSecurityError converts technical security errors into user-friendly messages.
func formatSecurityError(err error) string {
	return messaging.FormatSecurityError(err, messaging.SecurityMessagesCN)
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
