package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// Bridge orchestrates platform messages and gateway sessions.
// It is the counterpart of gateway.Bridge for messaging platforms.
type Bridge struct {
	log      *slog.Logger
	platform PlatformType
	hub      HubInterface
	sm       SessionManager
	handler  HandlerInterface
}

// NewBridge creates a new platform bridge.
func NewBridge(log *slog.Logger, platform PlatformType, hub HubInterface,
	sm SessionManager, handler HandlerInterface) *Bridge {
	return &Bridge{
		log:      log,
		platform: platform,
		hub:      hub,
		sm:       sm,
		handler:  handler,
	}
}

// Handle routes a platform message through the AEP handler.
// The OwnerID must be set (validated at SDK level by the adapter).
func (b *Bridge) Handle(ctx context.Context, env *events.Envelope) error {
	if env.OwnerID == "" {
		return fmt.Errorf("messaging bridge: OwnerID not set for platform message")
	}
	return b.handler.Handle(ctx, env)
}

// JoinSession subscribes a PlatformConn to a gateway session.
func (b *Bridge) JoinSession(sessionID string, pc PlatformConn) {
	b.hub.JoinPlatformSession(sessionID, pc)
}

// MakeSlackEnvelope converts a Slack message to an AEP input envelope.
// session ID format: slack:{team_id}:{channel_id}:{thread_ts}:{user_id}
func (b *Bridge) MakeSlackEnvelope(teamID, channelID, threadTS, userID, text string) *events.Envelope {
	sessionID := fmt.Sprintf("slack:%s:%s:%s:%s", teamID, channelID, threadTS, userID)
	return &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		Seq:       0,
		SessionID: sessionID,
		Timestamp: 0,
		Event: events.Event{
			Type: events.Input,
			Data: map[string]any{
				"content": strings.TrimSpace(text),
				"metadata": map[string]any{
					"platform":   "slack",
					"team_id":    teamID,
					"channel_id": channelID,
				},
			},
		},
		OwnerID: userID,
	}
}

// MakeFeishuEnvelope converts a Feishu message to an AEP input envelope.
// session ID format: feishu:{chat_id}:{thread_ts}:{user_id}
func (b *Bridge) MakeFeishuEnvelope(chatID, threadTS, userID, text string) *events.Envelope {
	sessionID := fmt.Sprintf("feishu:%s:%s:%s", chatID, threadTS, userID)
	return &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		Seq:       0,
		SessionID: sessionID,
		Timestamp: 0,
		Event: events.Event{
			Type: events.Input,
			Data: map[string]any{
				"content": strings.TrimSpace(text),
				"metadata": map[string]any{
					"platform": "feishu",
					"chat_id":  chatID,
				},
			},
		},
		OwnerID: userID,
	}
}
