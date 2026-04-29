package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// DefaultWorkerWorkDir is the fallback working directory when workDir is not configured.
var DefaultWorkerWorkDir = filepath.Join(config.TempBaseDir(), "workspace")

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
		log:        log.With("component", "messaging_bridge", "platform", string(platform)),
		platform:   platform,
		hub:        hub,
		sm:         sm,
		handler:    handler,
		starter:    starter,
		workerType: workerType,
		workDir:    workDir,
	}
}

// WorkDir returns the configured worker working directory.
func (b *Bridge) WorkDir() string { return b.workDir }

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
			b.log.Warn("messaging bridge: session start failed",
				"session_id", env.SessionID, "worker_type", b.workerType, "err", err)
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

	platformKey := b.platform.ExtractPlatformKeys(md)

	if len(platformKey) == 0 {
		return platform, nil
	}
	return platform, platformKey
}
