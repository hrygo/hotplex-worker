package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/hotplex/hotplex-worker/internal/session"
	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// DefaultWorkerWorkDir is the fallback working directory when workDir is not configured.
const DefaultWorkerWorkDir = "/tmp/hotplex/workspace"

// ConnFactory creates a PlatformConn for a given session.
// It receives the platform-specific raw ID (e.g., chat_id for Feishu, channel_id for Slack)
// extracted from the envelope metadata, allowing it to create a properly configured
// connection even when the session ID is a UUID.
type ConnFactory func(sessionID, rawID string) PlatformConn

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

	mu     sync.Mutex
	joined map[string]bool // sessionID → already joined
}

// NewBridge creates a new platform bridge.
func NewBridge(log *slog.Logger, platform PlatformType, hub HubInterface,
	sm SessionManager, handler HandlerInterface, starter SessionStarter, workerType, workDir string,
) *Bridge {
	if workDir == "" {
		workDir = DefaultWorkerWorkDir
	}
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
// The factory receives the session ID and the platform-specific raw ID (e.g., chat_id
// for Feishu) extracted from the envelope metadata, so it can create a properly
// configured PlatformConn even when the session ID is a UUID.
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
			rawID := ""
			if md, ok := env.Event.Data.(map[string]any); ok {
				if id, ok := md["chat_id"].(string); ok {
					rawID = id
				} else if id, ok := md["channel_id"].(string); ok {
					rawID = id
				}
			}
			pc := b.connFactory(env.SessionID, rawID)
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
// session ID is derived via UUIDv5: ownerID + workerType + platform + teamID + channelID + threadTS + userID.
func (b *Bridge) MakeSlackEnvelope(teamID, channelID, threadTS, userID, text string) *events.Envelope {
	sessionID := session.DerivePlatformSessionKey(userID, worker.WorkerType(b.workerType), session.PlatformContext{
		Platform:  "slack",
		TeamID:    teamID,
		ChannelID: channelID,
		ThreadTS:  threadTS,
		UserID:    userID,
	})
	return b.makeEnvelope(sessionID, userID, text, map[string]any{
		"platform":   string(PlatformSlack),
		"team_id":    teamID,
		"channel_id": channelID,
	})
}

// MakeFeishuEnvelope converts a Feishu message to an AEP input envelope.
// session ID is derived via UUIDv5: ownerID + workerType + platform + chatID + threadTS + userID.
func (b *Bridge) MakeFeishuEnvelope(chatID, threadTS, userID, text string) *events.Envelope {
	sessionID := session.DerivePlatformSessionKey(userID, worker.WorkerType(b.workerType), session.PlatformContext{
		Platform: "feishu",
		ChatID:   chatID,
		ThreadTS: threadTS,
		UserID:   userID,
	})
	return b.makeEnvelope(sessionID, userID, text, map[string]any{
		"platform": string(PlatformFeishu),
		"chat_id":  chatID,
	})
}
