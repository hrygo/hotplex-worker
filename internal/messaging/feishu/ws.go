package feishu

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/hrygo/hotplex/internal/messaging"
)

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
		}).
		OnP2ChatAccessEventBotP2pChatEnteredV1(func(ctx context.Context, event *larkim.P2ChatAccessEventBotP2pChatEnteredV1) (err error) {
			defer func() {
				if r := recover(); r != nil {
					a.Log.Error("feishu: panic in chat entered handler", "panic", r, "stack", string(debug.Stack()))
					err = fmt.Errorf("feishu chat entered panic: %v", r)
				}
			}()
			return a.handleChatEntered(ctx, event)
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
