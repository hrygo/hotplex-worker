// Package feishu provides a Feishu (Lark) WebSocket platform adapter.
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hotplex/hotplex-worker/internal/messaging"
	"github.com/hotplex/hotplex-worker/pkg/events"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"
)

func init() {
	messaging.Register(messaging.PlatformFeishu, func(log *slog.Logger) messaging.PlatformAdapterInterface {
		return &Adapter{log: log}
	})
}

type Adapter struct {
	messaging.PlatformAdapter

	log        *slog.Logger
	appID      string
	appSecret  string
	wsClient   *ws.Client
	larkClient *lark.Client
	bridge     *messaging.Bridge
	botOpenID  string

	mu          sync.RWMutex
	dedup       *Dedup
	activeConns map[string]*FeishuConn
	gate        *Gate
	chatQueue   *ChatQueue
	rateLimiter *FeishuRateLimiter
	dedupDone   chan struct{}
	dedupWg     sync.WaitGroup
}

func (a *Adapter) Platform() messaging.PlatformType { return messaging.PlatformFeishu }

func ExtractChatID(sessionID string) string {
	parts := strings.SplitN(sessionID, ":", 4)
	if len(parts) < 4 || parts[0] != "feishu" {
		return ""
	}
	return parts[1]
}

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

func (a *Adapter) Start(ctx context.Context) error {
	if a.appID == "" || a.appSecret == "" {
		return fmt.Errorf("feishu: appID and appSecret required")
	}

	a.dedup = NewDedup(dedupDefaultMaxEntries, dedupDefaultTTL)
	a.activeConns = make(map[string]*FeishuConn)
	a.chatQueue = NewChatQueue(a.log)
	a.rateLimiter = NewFeishuRateLimiter()
	a.dedupDone = make(chan struct{})
	a.dedupWg.Add(1)
	go a.dedupCleanupLoop()

	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			return a.handleMessage(ctx, event)
		}).
		OnP2MessageReadV1(func(_ context.Context, _ *larkim.P2MessageReadV1) error {
			return nil
		})

	a.wsClient = ws.NewClient(a.appID, a.appSecret,
		ws.WithEventHandler(eventHandler),
		ws.WithAutoReconnect(true),
	)
	a.larkClient = lark.NewClient(a.appID, a.appSecret)

	if err := a.fetchBotOpenID(ctx); err != nil {
		a.log.Warn("feishu: failed to fetch bot open_id, mention detection disabled", "error", err)
	}

	a.log.Info("feishu: starting WebSocket connection")

	go func() {
		if err := a.wsClient.Start(ctx); err != nil {
			a.log.Error("feishu: WebSocket connection error", "error", err)
		}
	}()

	return nil
}

func (a *Adapter) fetchBotOpenID(ctx context.Context) error {
	resp, err := a.larkClient.Get(ctx, "/open-apis/bot/v3/info", nil, "tenant_access_token")
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

func (a *Adapter) handleMessage(_ context.Context, event *larkim.P2MessageReceiveV1) error {
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
	if !a.dedup.TryRecord(messageID) {
		return nil
	}

	// Step 5: Message type conversion.
	msgType := ptrStr(msg.MessageType)
	text, ok := ConvertMessage(msgType, ptrStr(msg.Content), msg.Mentions, a.botOpenID)
	if !ok || text == "" {
		return nil
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

	replyToMsgID := parentID
	if replyToMsgID == "" {
		replyToMsgID = rootID
	}

	a.log.Debug("feishu: handling message",
		"chat_type", chatType,
		"chat", chatID,
		"user", userID,
		"thread_key", threadKey,
		"text_len", len(text),
	)

	return a.chatQueue.Enqueue(chatID, func(qtx context.Context) error {
		return a.handleTextMessage(qtx, messageID, chatID, userID, text, threadKey, replyToMsgID)
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

func (a *Adapter) handleTextMessage(ctx context.Context, platformMsgID, channelID, userID, text, threadKey, replyToMsgID string) error {
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

	// Pre-create conn and set reply target before bridge creates it via connFactory.
	conn := a.GetOrCreateConn(channelID)
	conn.replyToMsgID = replyToMsgID

	return a.bridge.Handle(ctx, envelope)
}

func (a *Adapter) HandleTextMessage(ctx context.Context, platformMsgID, channelID, userID, text string) error {
	return a.handleTextMessage(ctx, platformMsgID, channelID, userID, text, "", "")
}

func (a *Adapter) GetOrCreateConn(chatID string) *FeishuConn {
	a.mu.Lock()
	defer a.mu.Unlock()

	if conn, ok := a.activeConns[chatID]; ok {
		return conn
	}

	conn := NewFeishuConn(a, chatID)
	a.activeConns[chatID] = conn
	return conn
}

func (a *Adapter) Close(ctx context.Context) error {
	if a.log != nil {
		a.log.Info("feishu: adapter closing")
	}

	a.mu.Lock()
	for _, conn := range a.activeConns {
		_ = conn.Close()
	}
	a.activeConns = nil
	a.dedup = nil
	close(a.dedupDone)
	a.dedupWg.Wait()
	a.mu.Unlock()

	return nil
}

type FeishuConn struct {
	adapter      *Adapter
	chatID       string
	replyToMsgID string
	streamCtrl   *StreamingCardController
	typingRid    string
}

func NewFeishuConn(adapter *Adapter, chatID string) *FeishuConn {
	return &FeishuConn{adapter: adapter, chatID: chatID}
}

func (c *FeishuConn) EnableStreaming(ctrl *StreamingCardController) {
	c.streamCtrl = ctrl
}

func (c *FeishuConn) SetTypingReactionID(rid string) {
	c.typingRid = rid
}

func (c *FeishuConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
	if env == nil {
		return fmt.Errorf("feishu: nil envelope")
	}

	text, ok := extractResponseText(env)
	if !ok {
		return nil
	}

	c.adapter.log.Debug("feishu: WriteCtx sending",
		"event_type", env.Event.Type,
		"chat", c.chatID,
		"reply_to", c.replyToMsgID,
		"text_len", len(text),
	)

	if c.streamCtrl != nil {
		if env.Event.Type == events.Done {
			return c.streamCtrl.Close(ctx)
		}
		return c.streamCtrl.Write(text)
	}

	if c.replyToMsgID != "" {
		return c.adapter.replyMessage(ctx, c.replyToMsgID, text, false)
	}
	return c.adapter.sendTextMessage(ctx, c.chatID, text)
}

func (c *FeishuConn) Close() error {
	if c.streamCtrl != nil {
		_ = c.streamCtrl.Abort(context.Background())
	}
	if c.typingRid != "" && c.adapter.larkClient != nil {
		_ = c.adapter.RemoveTypingIndicator(context.Background(), "", c.typingRid)
	}
	c.adapter.mu.Lock()
	defer c.adapter.mu.Unlock()
	delete(c.adapter.activeConns, c.chatID)
	return nil
}

var _ messaging.PlatformConn = (*FeishuConn)(nil)

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
