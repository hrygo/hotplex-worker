package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// ConnFactory creates a PlatformConn for a given platform session.
// Each adapter registers its own factory during wiring.
type ConnFactory func(sessionID string) PlatformConn

// Bridge orchestrates platform messages and gateway sessions.
// It is the counterpart of gateway.Bridge for messaging platforms.
type Bridge struct {
	log         *slog.Logger
	platform    PlatformType
	hub         HubInterface
	sm          SessionManager
	handler     HandlerInterface
	starter     SessionStarter
	workerType  string
	workDir     string
	connFactory ConnFactory

	mu          sync.Mutex
	joined      map[string]bool // sessionID → already joined
}

// NewBridge creates a new platform bridge.
func NewBridge(log *slog.Logger, platform PlatformType, hub HubInterface,
	sm SessionManager, handler HandlerInterface, starter SessionStarter, workerType, workDir string,
) *Bridge {
	return &Bridge{
		log:        log,
		platform:   platform,
		hub:        hub,
		sm:         sm,
		handler:    handler,
		starter:    starter,
		workerType: workerType,
		workDir:    workDir,
		joined:     make(map[string]bool),
	}
}

// SetConnFactory registers the platform connection factory.
func (b *Bridge) SetConnFactory(f ConnFactory) { b.connFactory = f }

// Handle routes a platform message through the AEP handler.
// If no session exists yet, it auto-creates one using the configured worker type.
func (b *Bridge) Handle(ctx context.Context, env *events.Envelope) error {
	if env.OwnerID == "" {
		return fmt.Errorf("messaging bridge: OwnerID not set for platform message")
	}

	// Auto-create session if starter is available.
	if b.starter != nil {
		if err := b.starter.StartPlatformSession(ctx, env.SessionID, env.OwnerID, b.workerType, b.workDir); err != nil {
			b.log.Debug("messaging bridge: session start skipped or failed",
				"session_id", env.SessionID, "err", err)
		}
	}

	// Join platform conn so worker output is routed back to the platform.
	// Only join once per session to avoid duplicate entries in hub.
	if b.connFactory != nil && b.hub != nil {
		b.mu.Lock()
		if !b.joined[env.SessionID] {
			pc := b.connFactory(env.SessionID)
			if pc != nil {
				b.hub.JoinPlatformSession(env.SessionID, pc)
				b.joined[env.SessionID] = true
			}
		}
		b.mu.Unlock()
	}

	return b.handler.Handle(ctx, env)
}

// JoinSession subscribes a PlatformConn to a gateway session.
func (b *Bridge) JoinSession(sessionID string, pc PlatformConn) {
	b.hub.JoinPlatformSession(sessionID, pc)
}

// makeEnvelope creates an AEP input envelope with the given session ID and metadata.
func (b *Bridge) makeEnvelope(sessionID, ownerID, text string, metadata map[string]any) *events.Envelope {
	return &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		SessionID: sessionID,
		Event: events.Event{
			Type: events.Input,
			Data: map[string]any{
				"content":  strings.TrimSpace(text),
				"metadata": metadata,
			},
		},
		OwnerID: ownerID,
	}
}

// MakeSlackEnvelope converts a Slack message to an AEP input envelope.
// session ID format: slack:{team_id}:{channel_id}:{thread_ts}:{user_id}
func (b *Bridge) MakeSlackEnvelope(teamID, channelID, threadTS, userID, text string) *events.Envelope {
	sessionID := fmt.Sprintf("slack:%s:%s:%s:%s", teamID, channelID, threadTS, userID)
	return b.makeEnvelope(sessionID, userID, text, map[string]any{
		"platform":   "slack",
		"team_id":    teamID,
		"channel_id": channelID,
	})
}

// MakeFeishuEnvelope converts a Feishu message to an AEP input envelope.
// session ID format: feishu:{chat_id}:{thread_ts}:{user_id}
func (b *Bridge) MakeFeishuEnvelope(chatID, threadTS, userID, text string) *events.Envelope {
	sessionID := fmt.Sprintf("feishu:%s:%s:%s", chatID, threadTS, userID)
	return b.makeEnvelope(sessionID, userID, text, map[string]any{
		"platform": "feishu",
		"chat_id":  chatID,
	})
}
