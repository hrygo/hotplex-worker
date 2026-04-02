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

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"hotplex-worker/internal/admin"
	"hotplex-worker/internal/aep"
	"hotplex-worker/internal/config"
	"hotplex-worker/internal/gateway"
	"hotplex-worker/internal/security"
	"hotplex-worker/internal/session"
	"hotplex-worker/internal/tracing"
	"hotplex-worker/internal/worker"
	_ "hotplex-worker/internal/worker/claudecode"
	_ "hotplex-worker/internal/worker/opencodecli"
	_ "hotplex-worker/internal/worker/opencodeserver"
	_ "hotplex-worker/internal/worker/pi"
	"hotplex-worker/pkg/events"
)

var (
	flagConfig = flag.String("config", "", "Path to config file (YAML)")
	flagDev    = flag.Bool("dev", false, "Enable development mode (relaxed security)")
)

func main() {
	flag.Parse()
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

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(log)

	cfg := loadConfig()

	tracing.Init(ctx, log, "hotplex-worker-gateway")

	log.Info("gateway: starting",
		"version", version(),
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
		Version:       version,
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

func (a *bridgeAdapter) StartSession(ctx context.Context, id, userID string, wt worker.WorkerType) error {
	return a.bridge.StartSession(ctx, id, userID, wt)
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

func waitForSignal() os.Signal {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	return <-sig
}

func version() string {
	return "v1.0.0"
}

func newSessionID() string {
	return aep.NewSessionID()
}
