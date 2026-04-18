// Package main is the entry point for the HotPlex Worker Gateway.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hotplex/hotplex-worker/internal/admin"
	"github.com/hotplex/hotplex-worker/internal/config"
	"github.com/hotplex/hotplex-worker/internal/gateway"
	"github.com/hotplex/hotplex-worker/internal/messaging"
	"github.com/hotplex/hotplex-worker/internal/messaging/feishu"
	"github.com/hotplex/hotplex-worker/internal/messaging/slack"
	"github.com/hotplex/hotplex-worker/internal/security"
	"github.com/hotplex/hotplex-worker/internal/session"
	"github.com/hotplex/hotplex-worker/internal/tracing"
	"github.com/hotplex/hotplex-worker/internal/worker"
	_ "github.com/hotplex/hotplex-worker/internal/worker/claudecode"
	_ "github.com/hotplex/hotplex-worker/internal/worker/opencodecli"
	_ "github.com/hotplex/hotplex-worker/internal/worker/opencodeserver"
	_ "github.com/hotplex/hotplex-worker/internal/worker/pi"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

var (
	flagConfig  = flag.String("config", "", "Path to config file (YAML)")
	flagDev     = flag.Bool("dev", false, "Enable development mode (relaxed security)")
	flagVersion = flag.Bool("version", false, "Print version and exit")
)

func main() {
	flag.Parse()

	if *flagVersion {
		fmt.Println("hotplex-worker", versionString())
		return
	}

	// Load .env file if present (for local development).
	_ = godotenv.Load()
	_ = godotenv.Load(".env.local")

	if err := run(); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("gateway: fatal", "err", err)
			os.Exit(1)
		}
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := loadConfig()

	// Initialize logger based on config.
	var logHandler slog.Handler
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	if cfg.Log.Format == "text" {
		logHandler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		logHandler = slog.NewJSONHandler(os.Stdout, opts)
	}

	log := slog.New(logHandler).With(
		"service", "hotplex-gateway",
		"version", versionString(),
	)
	slog.SetDefault(log)

	tracing.Init(ctx, log, "hotplex-worker-gateway")

	log.Info("gateway: starting",
		"go", runtime.Version(),
		"addr", cfg.Gateway.Addr,
		"config", *flagConfig,
	)

	store, err := session.NewSQLiteStore(ctx, cfg)
	if err != nil {
		return err
	}

	var msgStore session.MessageStore
	if cfg.Session.EventStoreEnabled {
		msgStore, err = session.NewMessageStore(ctx, cfg)
		if err != nil {
			_ = store.Close()
			return fmt.Errorf("gateway: init message store: %w", err)
		}
	}

	sm, err := session.NewManager(ctx, log, cfg, store, msgStore)
	if err != nil {
		return err
	}

	sm.OnTerminate = func(sessionID string) {
		log.Info("gateway: session terminated", "session_id", sessionID)
	}

	hub := gateway.NewHub(log, cfg)

	hub.LogHandler = func(level, msg, sessionID string) {
		admin.AddLog(level, msg, sessionID)
	}

	var configWatcher *config.Watcher
	if *flagConfig != "" {
		configWatcher = config.NewWatcher(log, *flagConfig, nil,
			func(newCfg *config.Config) {
				log.Info("config: hot reload applied",
					"gateway.addr", newCfg.Gateway.Addr,
					"pool.max_size", newCfg.Pool.MaxSize,
					"gc_scan_interval", newCfg.Session.GCScanInterval,
				)
				cfg = newCfg
			},
			func(field string) {
				log.Warn("config: static field changed, restart required to apply",
					"field", field,
				)
			},
		)
		configWatcher.SetInitial(cfg)
		if err := configWatcher.Start(ctx); err != nil {
			log.Warn("config: watcher start failed, hot reload disabled", "error", err)
			configWatcher = nil
		}
	}

	sm.StateNotifier = func(ctx context.Context, sessionID string, state events.SessionState, message string) {
		env := events.NewEnvelope(aep.NewID(), sessionID, hub.NextSeq(sessionID), events.State, events.StateData{
			State:   state,
			Message: message,
		})
		_ = hub.SendToSession(ctx, env)
	}

	var jwtValidator *security.JWTValidator
	if len(cfg.Security.JWTSecret) > 0 {
		jwtValidator = security.NewJWTValidator(cfg.Security.JWTSecret, cfg.Security.JWTAudience)
	}

	auth := security.NewAuthenticator(&cfg.Security, jwtValidator)

	handler := gateway.NewHandler(log, cfg, hub, sm, jwtValidator)
	bridge := gateway.NewBridge(log, hub, sm, msgStore)

	mux := http.NewServeMux()
	deps := &GatewayDeps{
		Log:           log,
		Config:        cfg,
		Hub:           hub,
		SessionMgr:    sm,
		Auth:          auth,
		Handler:       handler,
		Bridge:        bridge,
		ConfigWatcher: configWatcher,
	}

	// Initialize messaging platform adapters.
	msgAdapters := startMessagingAdapters(ctx, log, cfg, hub, sm, handler, bridge)

	setupRoutes(mux, deps)

	server := &http.Server{
		Addr:         cfg.Gateway.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.Gateway.IdleTimeout,
		WriteTimeout: cfg.Gateway.WriteTimeout,
	}

	go hub.Run()

	serverErr := make(chan error, 1)
	go func() {
		log.Info("gateway: listening", "addr", cfg.Gateway.Addr)
		serverErr <- server.ListenAndServe()
	}()

	sig := waitForSignal()
	log.Info("gateway: shutdown", "signal", sig)

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() {
		if err := tracing.Shutdown(shutdownCtx); err != nil {
			log.Warn("tracing: shutdown", "error", err)
		}
		shutdownCancel()
	}()

	if err := hub.Shutdown(shutdownCtx); err != nil {
		log.Warn("gateway: hub shutdown", "err", err)
	}

	if configWatcher != nil {
		_ = configWatcher.Close()
	}

	// Close messaging adapters.
	for _, adapter := range msgAdapters {
		if err := adapter.Close(shutdownCtx); err != nil {
			log.Warn("messaging: adapter close", "err", err)
		}
	}

	if err := sm.Close(); err != nil {
		log.Warn("gateway: session manager close", "err", err)
	}

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Warn("gateway: http server shutdown", "err", err)
	}

	log.Info("gateway: stopped")
	return ctx.Err()
}

func loadConfig() *config.Config {
	cfg, err := config.Load(*flagConfig, config.LoadOptions{})
	if err != nil {
		slog.Error("config: load failed", "path", *flagConfig, "err", err)
		os.Exit(1)
	}
	if *flagDev {
		cfg.Security.APIKeys = nil
		cfg.Admin.Tokens = nil
	}
	return cfg
}

type GatewayDeps struct {
	Log           *slog.Logger
	Config        *config.Config
	Hub           *gateway.Hub
	SessionMgr    *session.Manager
	Auth          *security.Authenticator
	Handler       *gateway.Handler
	Bridge        *gateway.Bridge
	ConfigWatcher *config.Watcher
}

func setupRoutes(
	mux *http.ServeMux,
	deps *GatewayDeps,
) {
	log := deps.Log
	cfg := deps.Config
	hub := deps.Hub
	sm := deps.SessionMgr
	auth := deps.Auth
	handler := deps.Handler
	bridge := deps.Bridge
	configWatcher := deps.ConfigWatcher

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Browser-accessible session API (auth via api_key query param).
	gatewayAPI := gateway.NewGatewayAPI(auth, sm, bridge)

	// CORS preflight handler (OPTIONS)
	corsPreflight := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}

	mux.HandleFunc("GET /api/sessions", gatewayAPI.ListSessions)
	mux.HandleFunc("POST /api/sessions", gatewayAPI.CreateSession)
	mux.HandleFunc("GET /api/sessions/", gatewayAPI.GetSession)
	mux.HandleFunc("DELETE /api/sessions/", gatewayAPI.DeleteSession)
	mux.HandleFunc("OPTIONS /api/sessions", corsPreflight)
	mux.HandleFunc("OPTIONS /api/sessions/", corsPreflight)

	mux.HandleFunc("GET /admin/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.Handle("GET /admin/metrics", promhttp.Handler())

	mux.Handle("GET /ws", hub.HandleHTTP(auth, sm, handler, bridge))

	sessionAdapter := &sessionManagerAdapter{sm: sm}
	hubAdapter := &hubAdapter{hub: hub}
	bridgeAdapter := &bridgeAdapter{bridge: bridge}
	configAdapter := &configAdapter{cfg: cfg}
	configWatcherAdapter := &configWatcherAdapter{watcher: configWatcher}

	adminAPI := admin.New(admin.Deps{
		Log:           log,
		Config:        configAdapter,
		SessionMgr:    sessionAdapter,
		Hub:           hubAdapter,
		Bridge:        bridgeAdapter,
		ConfigWatcher: configWatcherAdapter,
		Version:       versionString,
		NewSessionID:  newSessionID,
	})

	if cfg.Admin.RateLimitEnabled {
		adminAPI.SetRateLimiter(admin.NewRateLimiter(cfg.Admin.RequestsPerSec, cfg.Admin.Burst))
	}
	if cfg.Admin.IPWhitelistEnabled {
		adminAPI.SetAllowedCIDRs(cfg.Admin.AllowedCIDRs)
	}

	adminMux := adminAPI.Mux()

	adminMux.HandleFunc("GET /admin/stats", adminAPI.HandleStats)
	adminMux.HandleFunc("GET /admin/health/workers", adminAPI.HandleWorkerHealth)
	adminMux.HandleFunc("GET /admin/logs", adminAPI.HandleLogs)
	adminMux.HandleFunc("POST /admin/config/validate", adminAPI.HandleConfigValidate)
	adminMux.HandleFunc("POST /admin/config/rollback", adminAPI.HandleConfigRollback)
	adminMux.HandleFunc("GET /admin/debug/sessions/{id}", adminAPI.HandleDebugSession)

	adminMux.HandleFunc("GET /admin/sessions", adminAPI.ListSessions)
	adminMux.HandleFunc("GET /admin/sessions/{id}", adminAPI.GetSession)
	adminMux.HandleFunc("DELETE /admin/sessions/{id}", adminAPI.DeleteSession)
	adminMux.HandleFunc("POST /admin/sessions/{id}/terminate", adminAPI.TerminateSession)

	mux.HandleFunc("GET /admin/health", adminAPI.HandleHealth)

	mux.Handle("/admin/", adminAPI.Middleware(adminMux))
}

type sessionManagerAdapter struct {
	sm *session.Manager
}

func (a *sessionManagerAdapter) Stats() (int, int, int) {
	return a.sm.Stats()
}

func (a *sessionManagerAdapter) List(ctx context.Context, limit, offset int) ([]any, error) {
	sessions, err := a.sm.List(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]any, len(sessions))
	for i, s := range sessions {
		result[i] = s
	}
	return result, nil
}

func (a *sessionManagerAdapter) Get(id string) (any, error) {
	return a.sm.Get(id)
}

func (a *sessionManagerAdapter) Delete(ctx context.Context, id string) error {
	return a.sm.Delete(ctx, id)
}

func (a *sessionManagerAdapter) WorkerHealthStatuses() []worker.WorkerHealth {
	return a.sm.WorkerHealthStatuses()
}

func (a *sessionManagerAdapter) DebugSnapshot(id string) (admin.DebugSessionSnapshot, bool) {
	snap, ok := a.sm.DebugSnapshot(id)
	if !ok {
		return admin.DebugSessionSnapshot{}, false
	}
	return admin.DebugSessionSnapshot{
		TurnCount:    snap.TurnCount,
		WorkerHealth: snap.WorkerHealth,
		HasWorker:    snap.HasWorker,
	}, true
}

func (a *sessionManagerAdapter) Transition(ctx context.Context, id string, to events.SessionState) error {
	return a.sm.Transition(ctx, id, to)
}

type hubAdapter struct {
	hub *gateway.Hub
}

func (a *hubAdapter) ConnectionsOpen() int {
	return a.hub.ConnectionsOpen()
}

func (a *hubAdapter) NextSeqPeek(sessionID string) int64 {
	return a.hub.NextSeqPeek(sessionID)
}

type bridgeAdapter struct {
	bridge *gateway.Bridge
}

func (a *bridgeAdapter) StartSession(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, workDir, platform string, platformKey map[string]string) error {
	return a.bridge.StartSession(ctx, id, userID, botID, wt, allowedTools, workDir, platform, platformKey)
}

type configAdapter struct {
	cfg *config.Config
}

func (a *configAdapter) Get() *config.Config {
	return a.cfg
}

type configWatcherAdapter struct {
	watcher *config.Watcher
}

func (a *configWatcherAdapter) Rollback(version int) (*config.Config, int, error) {
	if a.watcher == nil {
		return nil, -1, errors.New("config watcher is nil")
	}
	return a.watcher.Rollback(version)
}

// startMessagingAdapters initializes and starts all enabled messaging platform adapters.
func startMessagingAdapters(ctx context.Context, log *slog.Logger, cfg *config.Config,
	hub *gateway.Hub, sm *session.Manager, handler *gateway.Handler, gwBridge *gateway.Bridge,
) []messaging.PlatformAdapterInterface {
	var adapters []messaging.PlatformAdapterInterface
	for _, pt := range messaging.RegisteredTypes() {
		// Check if platform is enabled and resolve per-platform config.
		var workerType, workDir string
		switch pt {
		case messaging.PlatformSlack:
			if !cfg.Messaging.Slack.Enabled {
				continue
			}
			workerType = cfg.Messaging.Slack.WorkerType
			workDir = cfg.Messaging.Slack.WorkDir
		case messaging.PlatformFeishu:
			if !cfg.Messaging.Feishu.Enabled {
				continue
			}
			workerType = cfg.Messaging.Feishu.WorkerType
			workDir = cfg.Messaging.Feishu.WorkDir
		}

		adapter, err := messaging.New(pt, log)
		if err != nil {
			log.Warn("messaging: skip adapter", "platform", pt, "err", err)
			continue
		}

		msgBridge := messaging.NewBridge(log, pt, hub, sm, handler, gwBridge, workerType, workDir)

		// Configure platform-specific credentials and conn factory.
		switch pt {
		case messaging.PlatformSlack:
			if sa, ok := adapter.(*slack.Adapter); ok {
				sa.Configure(cfg.Messaging.Slack.BotToken, cfg.Messaging.Slack.AppToken, msgBridge)
				msgBridge.SetConnFactory(func(sessionID string, _ *events.Envelope) messaging.PlatformConn {
					channelID, threadTS := slack.ExtractChannelThread(sessionID)
					if channelID == "" {
						return nil
					}
					return slack.NewSlackConn(sa, channelID, threadTS)
				})
			}
		case messaging.PlatformFeishu:
			if fa, ok := adapter.(*feishu.Adapter); ok {
				fa.Configure(cfg.Messaging.Feishu.AppID, cfg.Messaging.Feishu.AppSecret, msgBridge)
				gate := feishu.NewGate(
					cfg.Messaging.Feishu.DMPolicy,
					cfg.Messaging.Feishu.GroupPolicy,
					cfg.Messaging.Feishu.RequireMention,
					cfg.Messaging.Feishu.AllowFrom,
				)
				fa.SetGate(gate)
				msgBridge.SetConnFactory(func(sessionID string, env *events.Envelope) messaging.PlatformConn {
					chatID := feishu.ExtractChatID(env)
					if chatID == "" {
						return nil
					}
					return fa.GetOrCreateConn(chatID)
				})
			}
		}

		// Inject dependencies via embedded PlatformAdapter.
		adapter.(interface{ SetHub(messaging.HubInterface) }).SetHub(hub) //nolint:errcheck // adapter guaranteed by registration
		adapter.(interface {                                              //nolint:errcheck // adapter guaranteed by registration
			SetSessionManager(messaging.SessionManager)
		}).SetSessionManager(sm)
		adapter.(interface { //nolint:errcheck // adapter guaranteed by registration
			SetHandler(messaging.HandlerInterface)
		}).SetHandler(handler)
		adapter.(interface{ SetBridge(*messaging.Bridge) }).SetBridge(msgBridge) //nolint:errcheck // adapter guaranteed by registration

		if err := adapter.Start(ctx); err != nil {
			log.Warn("messaging: start failed", "platform", pt, "err", err)
			continue
		}
		adapters = append(adapters, adapter)
		log.Info("messaging: adapter started", "platform", pt)
	}
	return adapters
}

func waitForSignal() os.Signal {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	return <-sig
}

const defaultVersion = "v1.0.0"

// version is injected by ldflags: -X main.version=$(GIT_SHA)
var version = defaultVersion

func versionString() string { return version }

func newSessionID() string {
	return aep.NewSessionID()
}
