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
	SM            *session.Manager
	JWTValidator  *security.JWTValidator
	Bridge        *Bridge                   // was SetBridge
	ConvStore     session.ConversationStore // was SetConvStore
	SkillsLocator SkillsLocator             // was SetSkillsLocator
}

// BridgeDeps groups all dependencies for Bridge construction.
type BridgeDeps struct {
	Log            *slog.Logger
	Hub            *Hub
	SM             SessionManager
	ConvStore      session.ConversationStore // was SetConvStore
	EventCollector *eventstore.Collector     // optional; nil means event storage disabled
	RetryCtrl      *LLMRetryController       // was SetRetryController
	AgentConfigDir string                    // was SetAgentConfigDir
	TurnTimeout    time.Duration             // was SetTurnTimeout
}
