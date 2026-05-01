package gateway

import (
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
)

// HandlerDeps groups all dependencies for Handler construction.
type HandlerDeps struct {
	Log           *slog.Logger
	Hub           *Hub
	SM            SessionManager
	JWTValidator  *security.JWTValidator
	Bridge        *Bridge
	ConvStore     session.ConversationStore
	SkillsLocator SkillsLocator
}

// BridgeDeps groups all dependencies for Bridge construction.
type BridgeDeps struct {
	Log                *slog.Logger
	Hub                *Hub
	SM                 SessionManager
	ConvStore          session.ConversationStore
	EventCollector     *eventstore.Collector // optional; nil means event storage disabled
	RetryCtrl          *LLMRetryController
	AgentConfigDir     string
	TurnTimeout        time.Duration
	WorkerEnv          []string // extra env vars from worker.environment config
	WorkerEnvWhitelist []string // extra whitelist entries from worker.env_whitelist config
}
