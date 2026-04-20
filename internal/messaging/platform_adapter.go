package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hotplex/hotplex-worker/pkg/events"
)

// PlatformType identifies the messaging platform.
type PlatformType string

const (
	PlatformSlack  PlatformType = "slack"
	PlatformFeishu PlatformType = "feishu"
)

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
}

// HubInterface is the subset of gateway.Hub methods needed by platform adapters.
type HubInterface interface {
	JoinPlatformSession(sessionID string, pc PlatformConn)
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
	StartPlatformSession(ctx context.Context, sessionID, ownerID, workerType, workDir, platform string, platformKey map[string]string) error
}

// SetHub injects the gateway Hub. Called by main.go during wiring.
func (a *PlatformAdapter) SetHub(hub HubInterface) { a.hub = hub }

// SetSessionManager injects the session manager. Called by main.go during wiring.
func (a *PlatformAdapter) SetSessionManager(sm SessionManager) { a.sm = sm }

// SetHandler injects the gateway Handler. Called by main.go during wiring.
func (a *PlatformAdapter) SetHandler(h HandlerInterface) { a.handler = h }

// SetBridge injects the messaging Bridge. Called by main.go during wiring.
func (a *PlatformAdapter) SetBridge(b *Bridge) { a.bridge = b }
