package admin

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

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
	Get(ctx context.Context, id string) (any, error)
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

type TurnStatsProvider interface {
	TurnStats(ctx context.Context, sessionID string) (*eventstore.TurnStats, error)
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
	turnStore     TurnStatsProvider
	hub           HubProvider
	bridge        BridgeProvider
	configWatcher ConfigWatcherProvider
	cron          CronSchedulerProvider
	logCollector  LogCollector
	rateLimiter   atomic.Value // *simpleRateLimiter
	allowedCIDRs  atomic.Value // []string
	version       func() string
	newSessionID  func() string
	startedAt     time.Time
}

type Deps struct {
	Log           *slog.Logger
	Config        ConfigProvider
	SessionMgr    SessionManagerProvider
	TurnStats     TurnStatsProvider
	Hub           HubProvider
	Bridge        BridgeProvider
	ConfigWatcher ConfigWatcherProvider
	Cron          CronSchedulerProvider
	LogCollector  LogCollector
	Version       func() string
	NewSessionID  func() string
}

func New(deps Deps) *AdminAPI {
	lc := deps.LogCollector
	if lc == nil {
		lc = LogRing
	}
	a := &AdminAPI{
		log:           deps.Log,
		cfg:           deps.Config,
		sm:            deps.SessionMgr,
		turnStore:     deps.TurnStats,
		hub:           deps.Hub,
		bridge:        deps.Bridge,
		configWatcher: deps.ConfigWatcher,
		cron:          deps.Cron,
		logCollector:  lc,
		version:       deps.Version,
		newSessionID:  deps.NewSessionID,
		startedAt:     time.Now(),
	}
	return a
}

func (a *AdminAPI) Mux() *http.ServeMux {
	return http.NewServeMux()
}

func (a *AdminAPI) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		defer func() {
			if rv := recover(); rv != nil {
				a.log.Error("admin: panic recovered",
					"error", rv,
					"path", r.URL.Path,
					"method", r.Method,
					"stack", string(debug.Stack()),
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()

		if rl, _ := a.rateLimiter.Load().(*simpleRateLimiter); rl != nil {
			if !rl.Allow() {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
		}

		if cidrs, _ := a.allowedCIDRs.Load().([]string); len(cidrs) > 0 {
			addr := clientIP(r)
			if !ipAllowed(addr, cidrs) {
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
	tb := []byte(token)
	if cfg.Admin.TokenScopes != nil {
		for t, scopes := range cfg.Admin.TokenScopes {
			if subtle.ConstantTimeCompare(tb, []byte(t)) == 1 {
				return scopes, true
			}
		}
	}
	for _, t := range cfg.Admin.Tokens {
		if subtle.ConstantTimeCompare(tb, []byte(t)) == 1 {
			if len(cfg.Admin.DefaultScopes) > 0 {
				return cfg.Admin.DefaultScopes, true
			}
			return []string{ScopeSessionRead, ScopeStatsRead, ScopeHealthRead}, true
		}
	}
	return nil, false
}

func (a *AdminAPI) SetRateLimiter(rl *simpleRateLimiter) {
	a.rateLimiter.Store(rl)
}

func (a *AdminAPI) SetAllowedCIDRs(cidrs []string) {
	a.allowedCIDRs.Store(cidrs)
}
