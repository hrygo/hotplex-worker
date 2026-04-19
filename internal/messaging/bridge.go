package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hotplex/hotplex-worker/internal/session"
	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// DefaultWorkerWorkDir is the fallback working directory when workDir is not configured.
const DefaultWorkerWorkDir = "/tmp/hotplex/workspace"

// Bridge orchestrates platform messages and gateway sessions.
// It is the counterpart of gateway.Bridge for messaging platforms.
type Bridge struct {
	log        *slog.Logger
	platform   PlatformType
	hub        HubInterface
	sm         SessionManager
	handler    HandlerInterface
	starter    SessionStarter
	workerType string
	workDir    string
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
	}
}

// Handle routes a platform message through the AEP handler.
// pc is the already-created PlatformConn for the platform session.
// It is registered with the hub so worker output is routed back to the platform.
// The caller is responsible for conn lifecycle (creation, field setup, reuse).
func (b *Bridge) Handle(ctx context.Context, env *events.Envelope, pc PlatformConn) error {
	if env.OwnerID == "" {
		return fmt.Errorf("messaging bridge: OwnerID not set for platform message")
	}

	// Auto-create session if starter is available.
	if b.starter != nil {
		// Extract platform key from envelope metadata for persistence.
		platform, platformKey := b.extractPlatformKey(env)
		if err := b.starter.StartPlatformSession(ctx, env.SessionID, env.OwnerID, b.workerType, b.workDir, platform, platformKey); err != nil {
			b.log.Debug("messaging bridge: session start skipped or failed",
				"session_id", env.SessionID, "err", err)
		}
	}

	// Register platform conn so worker output is routed back to the platform.
	if pc != nil && b.hub != nil {
		b.hub.JoinPlatformSession(env.SessionID, pc)
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
// session ID is derived via UUIDv5: ownerID + workerType + platform + teamID + channelID + threadTS + userID + workDir.
func (b *Bridge) MakeSlackEnvelope(teamID, channelID, threadTS, userID, text string) *events.Envelope {
	sessionID := session.DerivePlatformSessionKey(userID, worker.WorkerType(b.workerType), session.PlatformContext{
		Platform:  "slack",
		TeamID:    teamID,
		ChannelID: channelID,
		ThreadTS:  threadTS,
		UserID:    userID,
		WorkDir:   b.workDir,
	})
	return b.makeEnvelope(sessionID, userID, text, map[string]any{
		"platform":   string(PlatformSlack),
		"team_id":    teamID,
		"channel_id": channelID,
		"thread_ts":  threadTS,
		"user_id":    userID,
	})
}

// MakeFeishuEnvelope converts a Feishu message to an AEP input envelope.
// session ID is derived via UUIDv5: ownerID + workerType + platform + chatID + threadTS + userID + workDir.
func (b *Bridge) MakeFeishuEnvelope(chatID, threadTS, userID, text string) *events.Envelope {
	sessionID := session.DerivePlatformSessionKey(userID, worker.WorkerType(b.workerType), session.PlatformContext{
		Platform: "feishu",
		ChatID:   chatID,
		ThreadTS: threadTS,
		UserID:   userID,
		WorkDir:  b.workDir,
	})
	return b.makeEnvelope(sessionID, userID, text, map[string]any{
		"platform":  string(PlatformFeishu),
		"chat_id":   chatID,
		"thread_ts": threadTS,
		"user_id":   userID,
	})
}

// extractPlatformKey extracts the consistency-mapping inputs from the envelope metadata.
// Returns (platform, platformKey) suitable for session persistence.
func (b *Bridge) extractPlatformKey(env *events.Envelope) (string, map[string]string) {
	data, ok := env.Event.Data.(map[string]any)
	if !ok {
		return string(b.platform), nil
	}

	md, _ := data["metadata"].(map[string]any)
	if md == nil {
		return string(b.platform), nil
	}

	platform, _ := md["platform"].(string)
	if platform == "" {
		platform = string(b.platform)
	}

	platformKey := make(map[string]string)
	switch b.platform {
	case PlatformFeishu:
		if v, ok := md["chat_id"].(string); ok && v != "" {
			platformKey["chat_id"] = v
		}
		if v, ok := md["thread_ts"].(string); ok {
			platformKey["thread_ts"] = v
		}
		if v, ok := md["user_id"].(string); ok && v != "" {
			platformKey["user_id"] = v
		}
	case PlatformSlack:
		if v, ok := md["team_id"].(string); ok && v != "" {
			platformKey["team_id"] = v
		}
		if v, ok := md["channel_id"].(string); ok && v != "" {
			platformKey["channel_id"] = v
		}
		if v, ok := md["thread_ts"].(string); ok {
			platformKey["thread_ts"] = v
		}
		if v, ok := md["user_id"].(string); ok && v != "" {
			platformKey["user_id"] = v
		}
	}

	if len(platformKey) == 0 {
		return platform, nil
	}
	return platform, platformKey
}
