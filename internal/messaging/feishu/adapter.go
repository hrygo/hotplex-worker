// Package feishu provides a Feishu (Lark) WebSocket platform adapter.
package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// Adapter implements messaging.PlatformAdapterInterface for Feishu WebSocket.
type Adapter struct {
	messaging.PlatformAdapter

	log        *slog.Logger
	appID      string
	appSecret  string
	wsClient   *ws.Client
	larkClient *lark.Client
	bridge     *messaging.Bridge

	mu          sync.RWMutex
	dedup       map[string]time.Time // messageID -> seenAt
	activeConns map[string]*FeishuConn
	dedupDone   chan struct{}
	dedupWg     sync.WaitGroup
}

func (a *Adapter) Platform() messaging.PlatformType { return messaging.PlatformFeishu }

// ExtractChatID parses the chat_id from a Feishu session ID.
// Format: feishu:{chat_id}:{thread_ts}:{user_id}
func ExtractChatID(sessionID string) string {
	parts := strings.SplitN(sessionID, ":", 4)
	if len(parts) < 4 || parts[0] != "feishu" {
		return ""
	}
	return parts[1]
}

// Configure sets credentials before Start.
func (a *Adapter) Configure(appID, appSecret string, bridge *messaging.Bridge) {
	a.appID = appID
	a.appSecret = appSecret
	a.bridge = bridge
}

// SetBridge stores the bridge for later use.
func (a *Adapter) SetBridge(b *messaging.Bridge) {
	a.bridge = b
}

func (a *Adapter) Start(ctx context.Context) error {
	if a.appID == "" || a.appSecret == "" {
		return fmt.Errorf("feishu: appID and appSecret required")
	}

	a.dedup = make(map[string]time.Time)
	a.activeConns = make(map[string]*FeishuConn)
	a.dedupDone = make(chan struct{})
	a.dedupWg.Add(1)
	go a.dedupCleanupLoop()

	// Build event handler — ws.Client dispatches as P2 protocol.
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

	a.log.Info("feishu: starting WebSocket connection")

	go func() {
		if err := a.wsClient.Start(ctx); err != nil {
			a.log.Error("feishu: WebSocket connection error", "error", err)
		}
	}()

	return nil
}

func (a *Adapter) handleMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event.Event == nil || event.Event.Message == nil {
		return nil
	}

	msg := event.Event.Message

	// Only handle text messages
	if msg.MessageType == nil || *msg.MessageType != "text" {
		return nil
	}

	// Extract message ID for dedup
	messageID := ptrStr(msg.MessageId)
	if messageID == "" {
		return nil
	}
	a.mu.Lock()
	if _, seen := a.dedup[messageID]; seen {
		a.mu.Unlock()
		return nil
	}
	a.dedup[messageID] = time.Now()
	a.mu.Unlock()

	// Parse content: {"text":"hello"}
	text := extractTextFromContent(ptrStr(msg.Content))
	if text == "" {
		return nil
	}

	chatID := ptrStr(msg.ChatId)
	userID := ""
	if event.Event.Sender != nil && event.Event.Sender.SenderId != nil {
		userID = ptrStr(event.Event.Sender.SenderId.OpenId)
	}

	a.log.Debug("feishu: handling message",
		"chat", chatID,
		"user", userID,
		"text_len", len(text),
	)

	return a.HandleTextMessage(ctx, messageID, chatID, userID, text)
}

func (a *Adapter) HandleTextMessage(ctx context.Context, platformMsgID, channelID, userID, text string) error {
	if a.bridge == nil {
		return nil
	}

	envelope := a.bridge.MakeFeishuEnvelope(channelID, "", userID, text)
	if envelope == nil {
		return fmt.Errorf("feishu: failed to build envelope")
	}

	return a.bridge.Handle(ctx, envelope)
}

// GetOrCreateConn returns or creates a FeishuConn for the given chat.
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

// Close gracefully terminates the platform connection.
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

// FeishuConn wraps the adapter with chat routing info to satisfy messaging.PlatformConn.
type FeishuConn struct {
	adapter *Adapter
	chatID  string
}

// NewFeishuConn creates a platform connection bound to a chat.
func NewFeishuConn(adapter *Adapter, chatID string) *FeishuConn {
	return &FeishuConn{adapter: adapter, chatID: chatID}
}

// WriteCtx sends an AEP envelope to the bound Feishu chat.
func (c *FeishuConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
	if env == nil {
		return fmt.Errorf("feishu: nil envelope")
	}

	text, ok := extractResponseText(env)
	if !ok {
		return nil
	}

	return c.adapter.sendTextMessage(ctx, c.chatID, text)
}

// Close removes the connection from the adapter's active sessions.
func (c *FeishuConn) Close() error {
	c.adapter.mu.Lock()
	defer c.adapter.mu.Unlock()
	delete(c.adapter.activeConns, c.chatID)
	return nil
}

var _ messaging.PlatformConn = (*FeishuConn)(nil)

// sendTextMessage sends a plain text message to a Feishu chat.
func (a *Adapter) sendTextMessage(ctx context.Context, chatID, text string) error {
	if a.larkClient == nil {
		return fmt.Errorf("feishu: lark client not initialized")
	}

	content := larkim.NewMessageTextBuilder().TextLine(text).Build()
	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType(larkim.MsgTypeText).
		Content(content).
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

// dedupCleanupLoop periodically removes expired entries from the dedup map.
func (a *Adapter) dedupCleanupLoop() {
	defer a.dedupWg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-a.dedupDone:
			return
		case <-ticker.C:
			a.mu.Lock()
			cutoff := time.Now().Add(-10 * time.Minute)
			for k, v := range a.dedup {
				if v.Before(cutoff) {
					delete(a.dedup, k)
				}
			}
			a.mu.Unlock()
		}
	}
}

// ptrStr safely dereferences a *string, returning "" for nil.
func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// textContent holds the JSON structure of a Feishu text message.
type textContent struct {
	Text string `json:"text"`
}

// extractTextFromContent parses the text field from Feishu message content JSON.
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
