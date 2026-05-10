package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/pkg/events"
)

// PlatformType identifies the messaging platform.
type PlatformType string

const (
	PlatformSlack  PlatformType = "slack"
	PlatformFeishu PlatformType = "feishu"
)

// ExtractPlatformKeys pulls platform-specific fields from generic metadata.
func (p PlatformType) ExtractPlatformKeys(md map[string]any) map[string]string {
	pk := make(map[string]string)
	switch p {
	case PlatformFeishu:
		if v, ok := md["chat_id"].(string); ok && v != "" {
			pk["chat_id"] = v
		}
		if v, ok := md["thread_ts"].(string); ok {
			pk["thread_ts"] = v
		}
		if v, ok := md["user_id"].(string); ok && v != "" {
			pk["user_id"] = v
		}
	case PlatformSlack:
		if v, ok := md["team_id"].(string); ok && v != "" {
			pk["team_id"] = v
		}
		if v, ok := md["channel_id"].(string); ok && v != "" {
			pk["channel_id"] = v
		}
		if v, ok := md["thread_ts"].(string); ok {
			pk["thread_ts"] = v
		}
		if v, ok := md["user_id"].(string); ok && v != "" {
			pk["user_id"] = v
		}
	}
	// bot_id is platform-agnostic — extracted for all platform types.
	if v, ok := md["bot_id"].(string); ok && v != "" {
		pk["bot_id"] = v
	}
	return pk
}

// PlatformAdapterInterface is the minimal interface that all platform adapters must implement.
type PlatformAdapterInterface interface {
	// Platform returns the platform type identifier.
	Platform() PlatformType

	// Start initiates the platform connection.
	// It must be non-blocking: long-running setup runs in background goroutines.
	Start(ctx context.Context) error

	// HandleTextMessage processes an incoming text message from the platform.
	// The adapter maps the platform message to an AEP Envelope and delegates to PlatformBridge.Handle.
	// teamID and threadTS are optional; adapters that don't use them should ignore them.
	HandleTextMessage(ctx context.Context, platformMsgID, channelID, teamID, threadTS, userID, text string) error

	// Close gracefully terminates the platform connection.
	Close(ctx context.Context) error

	// ConfigureWith applies a unified configuration to the adapter.
	ConfigureWith(config AdapterConfig) error

	// GetBotID returns the platform bot identity (Slack UserID, Feishu OpenID, etc.).
	GetBotID() string
}

// CronResultSender sends a cron job execution result to a platform target.
type CronResultSender interface {
	SendCronResult(ctx context.Context, text string, platformKey map[string]string) error
}

// AdapterBuilder creates a new adapter instance.
type AdapterBuilder func(log *slog.Logger) PlatformAdapterInterface

var (
	registryMu sync.RWMutex
	registry   = make(map[PlatformType]AdapterBuilder)
)

// Register records an adapter builder under its platform type.
func Register(pt PlatformType, builder AdapterBuilder) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if builder == nil {
		panic(fmt.Sprintf("messaging: nil builder for platform %q", pt))
	}
	if _, exists := registry[pt]; exists {
		panic(fmt.Sprintf("messaging: duplicate registration for platform %q", pt))
	}
	registry[pt] = builder
}

// New creates an adapter by type.
func New(pt PlatformType, log *slog.Logger) (PlatformAdapterInterface, error) {
	registryMu.RLock()
	b, ok := registry[pt]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("messaging: unknown platform %q", pt)
	}
	return b(log.With("platform", string(pt))), nil
}

// RegisteredTypes returns all registered platform types.
func RegisteredTypes() []PlatformType {
	registryMu.RLock()
	defer registryMu.RUnlock()
	types := make([]PlatformType, 0, len(registry))
	for pt := range registry {
		types = append(types, pt)
	}
	return types
}

// PlatformAdapter is the base type for all messaging platform adapters.
// Each adapter embeds this struct and implements Start, HandleTextMessage, and Close.
type PlatformAdapter struct {
	Log *slog.Logger

	hub     HubInterface
	sm      SessionManager
	handler HandlerInterface
	bridge  *Bridge

	// Shared adapter state (promoted from Slack/Feishu adapters).
	started          atomic.Bool
	closed           atomic.Bool
	Dedup            *Dedup
	Gate             *Gate
	Interactions     *InteractionManager
	BackoffBaseDelay time.Duration
	BackoffMaxDelay  time.Duration
}

// HubInterface is the subset of gateway.Hub methods needed by platform adapters.
type HubInterface interface {
	JoinPlatformSession(sessionID string, pc PlatformConn)
	NextSeq(sessionID string) int64
}

// HandlerInterface is the subset of gateway.Handler methods needed by platform adapters.
type HandlerInterface interface {
	Handle(ctx context.Context, env *events.Envelope) error
}

// SessionManager is an opaque interface for session management.
// Platform adapters don't call session creation directly; the bridge handles it.
type SessionManager any

// SessionStarter creates a new gateway session for a platform message.
// Implemented by gateway.Bridge and injected during wiring.
type SessionStarter interface {
	StartPlatformSession(ctx context.Context, sessionID, ownerID, workerType, workDir, platform string, platformKey map[string]string, botID string) error
}

// Bridge returns the messaging bridge.
func (a *PlatformAdapter) Bridge() *Bridge { return a.bridge }

// ConfigureWith sets the common adapter dependencies from config.
func (a *PlatformAdapter) ConfigureWith(config AdapterConfig) error {
	a.hub = config.Hub
	a.sm = config.SM
	a.handler = config.Handler
	a.bridge = config.Bridge
	return nil
}

// ConfigureShared extracts common adapter config fields (gate, backoff delays).
func (a *PlatformAdapter) ConfigureShared(config AdapterConfig) {
	if config.Gate != nil {
		a.Gate = config.Gate
	}
	if bd := config.ExtrasDuration("reconnect_base_delay"); bd > 0 {
		a.BackoffBaseDelay = bd
	}
	if md := config.ExtrasDuration("reconnect_max_delay"); md > 0 {
		a.BackoffMaxDelay = md
	}
}

// StartGuard atomically marks the adapter as started. Returns true on the
// first call, false on subsequent calls.
func (a *PlatformAdapter) StartGuard() bool { return a.started.CompareAndSwap(false, true) }

// MarkClosed marks the adapter as closed.
func (a *PlatformAdapter) MarkClosed() { a.closed.Store(true) }

// IsClosed reports whether the adapter has been closed.
func (a *PlatformAdapter) IsClosed() bool { return a.closed.Load() }

// InitSharedState creates the default Dedup (5000 entries, 12h TTL) and
// InteractionManager. Adapters with custom dedup params should overwrite
// a.Dedup after calling this.
func (a *PlatformAdapter) InitSharedState() {
	a.Dedup = NewDedup(0, 0)
	a.Dedup.StartCleanup()
	a.Interactions = NewInteractionManager(a.Log)
}

// CloseSharedState closes the shared dedup tracker. Safe to call multiple times.
func (a *PlatformAdapter) CloseSharedState() {
	if a.Dedup != nil {
		a.Dedup.Close()
		a.Dedup = nil
	}
}
