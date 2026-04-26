package admin

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

var startTime = time.Now()

const (
	ScopeSessionRead  = "session:read"
	ScopeSessionWrite = "session:write"
	ScopeSessionKill  = "session:delete"
	ScopeStatsRead    = "stats:read"
	ScopeHealthRead   = "health:read"
	ScopeConfigRead   = "config:read"
	ScopeAdminRead    = "admin:read"
	ScopeAdminWrite   = "admin:write"
)

type SessionManagerProvider interface {
	Stats() (total, max, unique int)
	List(ctx context.Context, userID, platform string, limit, offset int) ([]any, error)
	Get(id string) (any, error)
	Delete(ctx context.Context, id string) error
	DeletePhysical(ctx context.Context, id string) error
	WorkerHealthStatuses() []worker.WorkerHealth
	DebugSnapshot(id string) (DebugSessionSnapshot, bool)
	Transition(ctx context.Context, id string, to events.SessionState) error
}

type HubProvider interface {
	ConnectionsOpen() int
	NextSeqPeek(sessionID string) int64
}

type BridgeProvider interface {
	StartSession(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, workDir string, platform string, platformKey map[string]string, title string) error
}

type ConfigProvider interface {
	Get() *config.Config
}

type ConfigWatcherProvider interface {
	Rollback(version int) (*config.Config, int, error)
}

type MessageStoreProvider interface {
	SessionStats(ctx context.Context, sessionID string) (any, error)
}

type DebugSessionSnapshot struct {
	TurnCount    int
	WorkerHealth worker.WorkerHealth
	HasWorker    bool
}

type AdminAPI struct {
	log           *slog.Logger
	cfg           ConfigProvider
	sm            SessionManagerProvider
	msgStore      MessageStoreProvider
	hub           HubProvider
	bridge        BridgeProvider
	configWatcher ConfigWatcherProvider
	rateLimiter   *simpleRateLimiter
	allowedCIDRs  []string
	version       func() string
	newSessionID  func() string
}

type Deps struct {
	Log           *slog.Logger
	Config        ConfigProvider
	SessionMgr    SessionManagerProvider
	MsgStore      MessageStoreProvider
	Hub           HubProvider
	Bridge        BridgeProvider
	ConfigWatcher ConfigWatcherProvider
	Version       func() string
	NewSessionID  func() string
}

func New(deps Deps) *AdminAPI {
	a := &AdminAPI{
		log:           deps.Log,
		cfg:           deps.Config,
		sm:            deps.SessionMgr,
		msgStore:      deps.MsgStore,
		hub:           deps.Hub,
		bridge:        deps.Bridge,
		configWatcher: deps.ConfigWatcher,
		version:       deps.Version,
		newSessionID:  deps.NewSessionID,
	}
	return a
}

func (a *AdminAPI) Mux() *http.ServeMux {
	return http.NewServeMux()
}

func (a *AdminAPI) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.rateLimiter != nil {
			if !a.rateLimiter.Allow() {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
		}

		if len(a.allowedCIDRs) > 0 {
			addr := clientIP(r)
			if !ipAllowed(addr, a.allowedCIDRs) {
				a.log.Warn("admin: IP not whitelisted", "ip", addr)
				http.Error(w, "IP not allowed", http.StatusForbidden)
				return
			}
		}

		token := extractBearerToken(r)
		if token == "" {
			http.Error(w, "missing admin token", http.StatusUnauthorized)
			return
		}
		scopes, ok := a.validateToken(token)
		if !ok {
			http.Error(w, "invalid admin token", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), scopeContextKey{}, scopes)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *AdminAPI) validateToken(token string) ([]string, bool) {
	cfg := a.cfg.Get()
	if cfg.Admin.TokenScopes != nil {
		if scopes, ok := cfg.Admin.TokenScopes[token]; ok {
			return scopes, true
		}
	}
	for _, t := range cfg.Admin.Tokens {
		if t == token {
			if len(cfg.Admin.DefaultScopes) > 0 {
				return cfg.Admin.DefaultScopes, true
			}
			return []string{ScopeSessionRead, ScopeStatsRead, ScopeHealthRead}, true
		}
	}
	return nil, false
}

func (a *AdminAPI) SetRateLimiter(rl *simpleRateLimiter) {
	a.rateLimiter = rl
}

func (a *AdminAPI) SetAllowedCIDRs(cidrs []string) {
	a.allowedCIDRs = cidrs
}
