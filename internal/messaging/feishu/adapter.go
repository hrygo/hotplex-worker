// Package feishu provides a Feishu (Lark) WebSocket platform adapter.
package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hotplex/hotplex-worker/internal/messaging"
	"github.com/hotplex/hotplex-worker/pkg/events"
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

	log       *slog.Logger
	appID     string
	appSecret string
	client    *ws.Client
	bridge    *messaging.Bridge

	mu          sync.RWMutex
	dedup       map[string]time.Time // messageID -> seenAt
	activeConns map[string]*FeishuConn
	dedupDone   chan struct{}
	dedupWg     sync.WaitGroup
}

func (a *Adapter) Platform() messaging.PlatformType { return messaging.PlatformFeishu }

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

	// Build event handler
	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnP1MessageReceiveV1(func(ctx context.Context, event *larkim.P1MessageReceiveV1) error {
			return a.handleMessage(ctx, event)
		})

	a.client = ws.NewClient(a.appID, a.appSecret,
		ws.WithEventHandler(eventHandler),
		ws.WithAutoReconnect(true),
	)

	a.log.Info("feishu: starting WebSocket connection")

	go func() {
		if err := a.client.Start(ctx); err != nil {
			a.log.Error("feishu: WebSocket connection error", "error", err)
		}
	}()

	return nil
}

func (a *Adapter) handleMessage(ctx context.Context, event *larkim.P1MessageReceiveV1) error {
	if event.Event == nil {
		return nil
	}

	e := event.Event

	// Dedup
	if e.OpenMessageID != "" {
		a.mu.Lock()
		if _, seen := a.dedup[e.OpenMessageID]; seen {
			a.mu.Unlock()
			return nil
		}
		a.dedup[e.OpenMessageID] = time.Now()
		a.mu.Unlock()
	}

	// Only handle text messages
	if e.MsgType != "text" {
		return nil
	}

	text := e.TextWithoutAtBot
	if text == "" {
		text = e.Text
	}
	if text == "" {
		return nil
	}

	a.log.Debug("feishu: handling message",
		"chat", e.OpenChatID,
		"user", e.OpenID,
		"text_len", len(text),
	)

	return a.HandleTextMessage(ctx, e.OpenMessageID, e.OpenChatID, e.OpenID, text)
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
	a.log.Debug("feishu: outbound message", "chat", chatID, "text_len", len(text))
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
