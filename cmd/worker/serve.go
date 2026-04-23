package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"syscall"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/hotplex/hotplex-worker/internal/admin"
	"github.com/hotplex/hotplex-worker/internal/config"
	"github.com/hotplex/hotplex-worker/internal/gateway"
	"github.com/hotplex/hotplex-worker/internal/messaging"
	"github.com/hotplex/hotplex-worker/internal/messaging/feishu"
	"github.com/hotplex/hotplex-worker/internal/messaging/slack"
	"github.com/hotplex/hotplex-worker/internal/messaging/stt"
	"github.com/hotplex/hotplex-worker/internal/security"
	"github.com/hotplex/hotplex-worker/internal/session"
	"github.com/hotplex/hotplex-worker/internal/tracing"
	"github.com/hotplex/hotplex-worker/internal/worker"
	_ "github.com/hotplex/hotplex-worker/internal/worker/claudecode"
	_ "github.com/hotplex/hotplex-worker/internal/worker/opencodeserver"
	_ "github.com/hotplex/hotplex-worker/internal/worker/pi"
	"github.com/hotplex/hotplex-worker/internal/worker/proc"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

func newGatewayCmd() *cobra.Command {
	var configPath string
	var devMode bool

	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Start the gateway server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGateway(configPath, devMode)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "~/.hotplex/config.yaml", "config file path")
	cmd.Flags().BoolVar(&devMode, "dev", false, "development mode")
	return cmd
}

func runGateway(configPath string, devMode bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := loadConfig(configPath, devMode)

	cfgStore := config.NewConfigStore(cfg, slog.Default())

	var logHandler slog.Handler
	levelVar := &slog.LevelVar{}
	if err := levelVar.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		levelVar.Set(slog.LevelInfo)
	}

	opts := &slog.HandlerOptions{
		Level: levelVar,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.String(slog.TimeKey, a.Value.Time().Format("2006-01-02T15:04:05.0000"))
			}
			return a
		},
	}
	if cfg.Log.Format == "text" {
		logHandler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		logHandler = slog.NewJSONHandler(os.Stderr, opts)
	}

	log := slog.New(logHandler).With(
		"service", "hotplex-gateway",
		"version", versionString(),
	)
	slog.SetDefault(log)

	pidDir := cfg.Worker.PIDDir
	pidTracker := proc.InitTracker(pidDir, log)
	if err := pidTracker.EnsureDir(); err != nil {
		log.Warn("gateway: pid dir setup failed, orphan cleanup disabled", "dir", pidDir, "err", err)
	} else {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			results := pidTracker.CleanupOrphans(ctx, 3, 5*time.Second)
			killed := 0
			for _, r := range results {
				if r.Err != nil {
					log.Warn("gateway: orphan cleanup error", "key", r.Key, "pgid", r.PGID, "err", r.Err)
				} else if r.Killed {
					log.Info("gateway: killed orphan process", "key", r.Key, "pgid", r.PGID)
					killed++
				}
			}
			if len(results) > 0 {
				log.Info("gateway: orphan cleanup complete", "scanned", len(results), "killed", killed)
			}
		}()
	}

	tracing.Init(ctx, log, "hotplex-worker-gateway")

	log.Info("gateway: starting",
		"go", runtime.Version(),
		"addr", cfg.Gateway.Addr,
		"config", configPath,
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

	sm, err := session.NewManager(ctx, log, cfg, cfgStore, store, msgStore)
	if err != nil {
		return err
	}

	sm.OnTerminate = func(sessionID string) {
		log.Info("gateway: session terminated", "session_id", sessionID)
	}

	hub := gateway.NewHub(log, cfgStore)

	hub.LogHandler = func(level, msg, sessionID string) {
		admin.AddLog(level, msg, sessionID)
	}

	var configWatcher *config.Watcher
	if configPath != "" {
		configWatcher = config.NewWatcher(log, configPath, nil, cfgStore,
			func(newCfg *config.Config) {
				log.Info("config: hot reload applied",
					"gateway.addr", newCfg.Gateway.Addr,
					"pool.max_size", newCfg.Pool.MaxSize,
					"gc_scan_interval", newCfg.Session.GCScanInterval,
				)
			},
			func(field string) {
				log.Warn("config: static field changed, restart required to apply",
					"field", field,
				)
			},
		)
		configWatcher.SetInitial(cfg)
	}

	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if prev.Log.Level != next.Log.Level {
			var newLevel slog.Level
			if err := newLevel.UnmarshalText([]byte(next.Log.Level)); err == nil {
				levelVar.Set(newLevel)
				log.Info("config: log level updated", "old", prev.Log.Level, "new", next.Log.Level)
			}
		}
	})

	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if prev.Pool.MaxSize != next.Pool.MaxSize || prev.Pool.MaxIdlePerUser != next.Pool.MaxIdlePerUser {
			sm.Pool().UpdateLimits(next.Pool.MaxSize, next.Pool.MaxIdlePerUser)
		}
	})

	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if prev.Session.GCScanInterval != next.Session.GCScanInterval {
			sm.ResetGCInterval(next.Session.GCScanInterval)
		}
	})

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

	handler := gateway.NewHandler(log, hub, sm, jwtValidator)
	bridge := gateway.NewBridge(log, hub, sm, msgStore)
	handler.SetBridge(bridge)

	retryCtrl := gateway.NewLLMRetryController(cfg.Worker.AutoRetry, log)
	bridge.SetRetryController(retryCtrl)
	if cfg.Worker.AutoRetry.Enabled {
		log.Info("gateway: LLM auto-retry enabled", "max_retries", cfg.Worker.AutoRetry.MaxRetries, "base_delay", cfg.Worker.AutoRetry.BaseDelay)
	}

	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if !reflect.DeepEqual(prev.Worker.AutoRetry, next.Worker.AutoRetry) {
			retryCtrl.UpdateConfig(next.Worker.AutoRetry)
		}
	})

	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if !reflect.DeepEqual(prev.Security.APIKeys, next.Security.APIKeys) {
			auth.ReloadKeys(&next.Security)
		}
	})

	mux := http.NewServeMux()
	deps := &GatewayDeps{
		Log:           log,
		Config:        cfg,
		ConfigStore:   cfgStore,
		Hub:           hub,
		SessionMgr:    sm,
		Auth:          auth,
		Handler:       handler,
		Bridge:        bridge,
		ConfigWatcher: configWatcher,
	}

	msgAdapters, adapterStatuses := startMessagingAdapters(ctx, log, cfg, hub, sm, handler, bridge)

	setupRoutes(mux, deps)

	server := &http.Server{
		Addr:         cfg.Gateway.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.Gateway.IdleTimeout,
		WriteTimeout: cfg.Gateway.WriteTimeout,
	}

	if configWatcher != nil {
		if err := configWatcher.Start(ctx); err != nil {
			log.Warn("config: watcher start failed", "err", err)
		}
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	adminAddr := cfg.Admin.Addr
	if !cfg.Admin.Enabled {
		adminAddr = ""
	}
	printStartupBanner(os.Stdout, newBuildInfo(), RuntimeStatus{
		GatewayAddr:  cfg.Gateway.Addr,
		AdminAddr:    adminAddr,
		WebChatAddr:  cfg.WebChat.Addr,
		DBPath:       cfg.DB.Path,
		PoolMax:      cfg.Pool.MaxSize,
		PoolIdle:     cfg.Pool.MaxIdlePerUser,
		Adapters:     adapterStatuses,
		RetryEnabled: cfg.Worker.AutoRetry.Enabled,
		RetryMax:     cfg.Worker.AutoRetry.MaxRetries,
		RetryDelay:   cfg.Worker.AutoRetry.BaseDelay.String(),
	}, configPath)

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

	for _, adapter := range msgAdapters {
		if err := adapter.Close(shutdownCtx); err != nil {
			log.Warn("messaging: adapter close", "err", err)
		}
	}

	bridge.Shutdown()

	pidTracker.RemoveAll()

	if err := sm.Close(); err != nil {
		log.Warn("gateway: session manager close", "err", err)
	}

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Warn("gateway: http server shutdown", "err", err)
	}

	log.Info("gateway: stopped")
	return ctx.Err()
}

func loadConfig(configPath string, devMode bool) *config.Config {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		slog.Error("config: resolve path", "path", configPath, "err", err)
		os.Exit(1)
	}

	cfg, err := config.Load(absPath, config.LoadOptions{})
	if err != nil {
		slog.Error("config: load failed", "path", absPath, "err", err)
		os.Exit(1)
	}
	if devMode {
		cfg.Security.APIKeys = nil
		cfg.Admin.Tokens = nil
	}
	return cfg
}

type GatewayDeps struct {
	Log           *slog.Logger
	Config        *config.Config
	ConfigStore   *config.ConfigStore
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

	gatewayAPI := gateway.NewGatewayAPI(auth, sm, bridge)

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
	configAdapter := &configAdapter{cfgStore: deps.ConfigStore}
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
		limiter := admin.NewRateLimiter(cfg.Admin.RequestsPerSec, cfg.Admin.Burst)
		adminAPI.SetRateLimiter(limiter)

		deps.ConfigStore.RegisterFunc(func(prev, next *config.Config) {
			if prev.Admin.RequestsPerSec != next.Admin.RequestsPerSec || prev.Admin.Burst != next.Admin.Burst {
				limiter.UpdateRate(next.Admin.RequestsPerSec, next.Admin.Burst)
			}
		})
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
	cfgStore *config.ConfigStore
}

func (a *configAdapter) Get() *config.Config {
	return a.cfgStore.Load()
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

func startMessagingAdapters(ctx context.Context, log *slog.Logger, cfg *config.Config,
	hub *gateway.Hub, sm *session.Manager, handler *gateway.Handler, gwBridge *gateway.Bridge,
) ([]messaging.PlatformAdapterInterface, []AdapterStatus) {
	var adapters []messaging.PlatformAdapterInterface
	var statuses []AdapterStatus
	for _, pt := range messaging.RegisteredTypes() {
		var workerType, workDir string
		switch pt {
		case messaging.PlatformSlack:
			if !cfg.Messaging.Slack.Enabled {
				statuses = append(statuses, AdapterStatus{Name: "Slack", Started: false})
				continue
			}
			workerType = cfg.Messaging.Slack.WorkerType
			workDir = cfg.Messaging.Slack.WorkDir
		case messaging.PlatformFeishu:
			if !cfg.Messaging.Feishu.Enabled {
				statuses = append(statuses, AdapterStatus{Name: "Feishu", Started: false})
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

		switch pt {
		case messaging.PlatformSlack:
			if sa, ok := adapter.(*slack.Adapter); ok {
				sa.Configure(cfg.Messaging.Slack.BotToken, cfg.Messaging.Slack.AppToken, msgBridge)
				gate := slack.NewGate(
					cfg.Messaging.Slack.DMPolicy,
					cfg.Messaging.Slack.GroupPolicy,
					cfg.Messaging.Slack.RequireMention,
					cfg.Messaging.Slack.AllowFrom,
					cfg.Messaging.Slack.AllowDMFrom,
					cfg.Messaging.Slack.AllowGroupFrom,
				)
				sa.SetGate(gate)
				sa.SetAssistantEnabled(cfg.Messaging.Slack.AssistantAPIEnabled)
				sa.SetReconnectDelays(cfg.Messaging.Slack.ReconnectBaseDelay, cfg.Messaging.Slack.ReconnectMaxDelay)
				if t := buildSlackTranscriber(cfg.Messaging.Slack, log); t != nil {
					sa.SetTranscriber(t)
				}
			}
		case messaging.PlatformFeishu:
			if fa, ok := adapter.(*feishu.Adapter); ok {
				fa.Configure(cfg.Messaging.Feishu.AppID, cfg.Messaging.Feishu.AppSecret, msgBridge)
				gate := feishu.NewGate(
					cfg.Messaging.Feishu.DMPolicy,
					cfg.Messaging.Feishu.GroupPolicy,
					cfg.Messaging.Feishu.RequireMention,
					cfg.Messaging.Feishu.AllowFrom,
					cfg.Messaging.Feishu.AllowDMFrom,
					cfg.Messaging.Feishu.AllowGroupFrom,
				)
				fa.SetGate(gate)

				if t := buildTranscriber(cfg.Messaging.Feishu, log); t != nil {
					fa.SetTranscriber(t)
				}
			}
		}

		if a, ok := adapter.(interface{ SetHub(messaging.HubInterface) }); ok {
			a.SetHub(hub)
		}
		if a, ok := adapter.(interface {
			SetSessionManager(messaging.SessionManager)
		}); ok {
			a.SetSessionManager(sm)
		}
		if a, ok := adapter.(interface {
			SetHandler(messaging.HandlerInterface)
		}); ok {
			a.SetHandler(handler)
		}
		if a, ok := adapter.(interface{ SetBridge(*messaging.Bridge) }); ok {
			a.SetBridge(msgBridge)
		}

		if err := adapter.Start(ctx); err != nil {
			log.Warn("messaging: start failed", "platform", pt, "err", err)
			statuses = append(statuses, AdapterStatus{Name: string(pt), Started: false})
			continue
		}
		adapters = append(adapters, adapter)
		statuses = append(statuses, AdapterStatus{Name: string(pt), Started: true})
		log.Info("messaging: adapter started", "platform", pt)
	}
	return adapters, statuses
}

func buildTranscriber(cfg config.FeishuConfig, log *slog.Logger) stt.Transcriber {
	switch cfg.Provider {
	case config.STTProviderFeishu:
		client := lark.NewClient(cfg.AppID, cfg.AppSecret)
		return feishu.NewFeishuSTT(client, log)
	case config.STTProviderLocal:
		return buildLocalSTT("feishu", cfg.STTConfig, log)
	case config.STTProviderFeishuLocal:
		if cfg.LocalCmd == "" {
			log.Warn("feishu: stt_provider=feishu+local but stt_local_cmd is empty, using feishu only")
			client := lark.NewClient(cfg.AppID, cfg.AppSecret)
			return feishu.NewFeishuSTT(client, log)
		}
		client := lark.NewClient(cfg.AppID, cfg.AppSecret)
		return stt.NewFallbackSTT(
			feishu.NewFeishuSTT(client, log),
			buildLocalSTT("feishu", cfg.STTConfig, log),
			log,
		)
	default:
		return nil
	}
}

func buildSlackTranscriber(cfg config.SlackConfig, log *slog.Logger) stt.Transcriber {
	if cfg.Provider != config.STTProviderLocal {
		return nil
	}
	return buildLocalSTT("slack", cfg.STTConfig, log)
}

func buildLocalSTT(platform string, cfg config.STTConfig, log *slog.Logger) stt.Transcriber {
	if cfg.LocalCmd == "" {
		log.Warn(platform + ": stt_provider=local but stt_local_cmd is empty, STT disabled")
		return nil
	}
	if cfg.LocalMode == config.STTModePersistent {
		return stt.NewPersistentSTT(cfg.LocalCmd, cfg.LocalIdleTTL, log)
	}
	return stt.NewLocalSTT(cfg.LocalCmd, log)
}

func waitForSignal() os.Signal {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	return <-sig
}
