package gateway

import (
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/security"
)

// HandlerDeps groups all dependencies for Handler construction.
type HandlerDeps struct {
	Log           *slog.Logger
	Hub           *Hub
	SM            SessionManager
	Auth          *security.Authenticator
	JWTValidator  *security.JWTValidator
	Bridge        *Bridge
	SkillsLocator SkillsLocator
}

// BridgeDeps groups all dependencies for Bridge construction.
type BridgeDeps struct {
	Log                *slog.Logger
	Hub                *Hub
	SM                 bridgeSM
	EventCollector     *eventstore.Collector // optional; nil means event storage disabled
	RetryCtrl          *LLMRetryController
	AgentConfigDir     string
	TurnTimeout        time.Duration
	WorkerEnv          []string // extra env vars from worker.environment config
	WorkerEnvBlocklist []string // extra blocklist entries from worker.env_blocklist config
	CronEnv            []string // env vars injected only into cron platform sessions (e.g. admin API creds)
	MCPConfigJSON      string   // pre-serialized MCP config JSON; "" = not configured → Claude Code default discovery
}
