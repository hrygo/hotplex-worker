package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/admin"
	"github.com/hrygo/hotplex/internal/assets"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/gateway"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/tracing"
	"github.com/hrygo/hotplex/internal/webchat"
	"github.com/hrygo/hotplex/internal/worker/claudecode"
	"github.com/hrygo/hotplex/internal/worker/opencodeserver"
	"github.com/hrygo/hotplex/internal/worker/proc"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

type GatewayDeps struct {
	Log            *slog.Logger
	Config         *config.Config
	ConfigStore    *config.ConfigStore
	Hub            *gateway.Hub
	SessionMgr     *session.Manager
	ConvStore      session.ConversationStore
	EventStore     *eventstore.SQLiteStore
	EventCollector *eventstore.Collector
	Auth           *security.Authenticator
	Handler        *gateway.Handler
	Bridge         *gateway.Bridge
	ConfigWatcher  *config.Watcher
}

const defaultConfigPath = "~/.hotplex/config.yaml"

func configFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVarP(target, "config", "c", defaultConfigPath, "config file path")
}

func runGateway(configPath string, devMode bool) (err error) {
	defer func() {
		if err != nil {
			removeGatewayPID()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := loadConfig(configPath, devMode)
	if err != nil {
		return err
	}

	// Extract embedded Python scripts to ~/.hotplex/scripts
	scriptsDir := filepath.Join(config.HotplexHome(), "scripts")
	if err := assets.InstallScripts(scriptsDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: assets: script extraction failed: %s\n", err)
	}

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
	var cleanupWG sync.WaitGroup
	if err := pidTracker.EnsureDir(); err != nil {
		log.Warn("gateway: pid dir setup failed, orphan cleanup disabled", "dir", pidDir, "err", err)
	} else {
		cleanupWG.Add(1)
		go func() {
			defer cleanupWG.Done()
			cleanupCtx, cleanupCancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cleanupCancel()
			results := pidTracker.CleanupOrphans(cleanupCtx, 3, 5*time.Second)
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

	tracing.Init(ctx, log, "hotplex-gateway")

	log.Info("gateway: starting",
		"go", runtime.Version(),
		"addr", cfg.Gateway.Addr,
		"config", configPath,
	)

	store, err := session.NewSQLiteStore(ctx, cfg)
	if err != nil {
		return err
	}

	convStore, err := session.NewSQLiteConversationStore(ctx, cfg)
	if err != nil {
		_ = store.Close()
		return fmt.Errorf("gateway: init conversation store: %w", err)
	}

	eventDBPath := filepath.Join(filepath.Dir(cfg.DB.Path), "events.db")
	eventStore, err := eventstore.NewSQLiteStore(ctx, eventDBPath)
	if err != nil {
		_ = store.Close()
		_ = convStore.Close()
		return fmt.Errorf("gateway: init event store: %w", err)
	}
	eventCollector := eventstore.NewCollector(eventStore, log)

	sm, err := session.NewManager(ctx, log, cfg, cfgStore, store)
	if err != nil {
		return err
	}

	sm.OnTerminate = func(sessionID string) {
		log.Info("gateway: session terminated", "session_id", sessionID)
	}

	// Wait for orphan process cleanup to finish before repairing sessions.
	cleanupWG.Wait()

	// Repair sessions orphaned by previous gateway crash/restart.
	// Sessions stuck in RUNNING state have no live worker — their processes
	// were killed by CleanupOrphans above. Transition them to TERMINATED so
	// clients get a clean reconnect instead of crash-looping on resume.
	repaired, repairErr := sm.RepairRunningSessions(ctx)
	if repairErr != nil {
		log.Warn("gateway: session state repair failed", "err", repairErr)
	} else if repaired > 0 {
		log.Info("gateway: repaired orphaned sessions", "count", repaired)
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
					"gateway_addr", newCfg.Gateway.Addr,
					"pool_max_size", newCfg.Pool.MaxSize,
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

	retryCtrl := gateway.NewLLMRetryController(cfg.Worker.AutoRetry, log)

	agentConfigDir := ""
	if cfg.AgentConfig.Enabled {
		agentConfigDir = cfg.AgentConfig.ConfigDir
	}

	bridge := gateway.NewBridge(gateway.BridgeDeps{
		Log:            log,
		Hub:            hub,
		SM:             sm,
		ConvStore:      convStore,
		RetryCtrl:      retryCtrl,
		AgentConfigDir: agentConfigDir,
		TurnTimeout:    cfg.Worker.TurnTimeout,
	})

	handler := gateway.NewHandler(gateway.HandlerDeps{
		Log:           log,
		Hub:           hub,
		SM:            sm,
		JWTValidator:  jwtValidator,
		Bridge:        bridge,
		ConvStore:     convStore,
		SkillsLocator: skills.NewLocator(log, cfg.Skills.CacheTTL),
	})

	if cfg.Worker.AutoRetry.Enabled {
		log.Info("gateway: LLM auto-retry enabled", "max_retries", cfg.Worker.AutoRetry.MaxRetries, "base_delay", cfg.Worker.AutoRetry.BaseDelay)
	}

	// Initialize OpenCode Server singleton process manager.
	opencodeserver.InitSingleton(log, cfg.Worker.OpenCodeServer)

	// Initialize Claude Code worker with configured command.
	claudecode.InitConfig(cfg.Worker.ClaudeCode)

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

	cfgStore.RegisterFunc(func(prev, next *config.Config) {
		if prev.Worker.ClaudeCode.Command != next.Worker.ClaudeCode.Command {
			claudecode.InitConfig(next.Worker.ClaudeCode)
		}
	})

	mux := http.NewServeMux()
	deps := &GatewayDeps{
		Log:           log,
		Config:        cfg,
		ConfigStore:   cfgStore,
		Hub:           hub,
		SessionMgr:    sm,
		ConvStore:     convStore,
		EventStore:    eventStore,
		Auth:          auth,
		Handler:       handler,
		Bridge:        bridge,
		ConfigWatcher: configWatcher,
	}

	msgAdapters, adapterStatuses := startMessagingAdapters(ctx, deps)

	setupRoutes(mux, deps)

	// Wrap mux with webchat SPA fallback: API/WS routes are handled by the
	// mux first; unmatched paths fall through to the embedded webchat handler.
	var rootHandler http.Handler = mux
	if cfg.WebChat.Enabled {
		spa := webchat.Handler()
		rootHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h, pattern := mux.Handler(r)
			if pattern != "" {
				h.ServeHTTP(w, r)
				return
			}
			spa.ServeHTTP(w, r)
		})
	}

	server := &http.Server{
		Addr:         cfg.Gateway.Addr,
		Handler:      rootHandler,
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
		GatewayAddr:     cfg.Gateway.Addr,
		AdminAddr:       adminAddr,
		WebChatAddr:     cfg.WebChat.Addr,
		WebChatEmbedded: cfg.WebChat.Enabled,
		DBPath:          cfg.DB.Path,
		PoolMax:         cfg.Pool.MaxSize,
		PoolIdle:        cfg.Pool.MaxIdlePerUser,
		Adapters:        adapterStatuses,
		RetryEnabled:    cfg.Worker.AutoRetry.Enabled,
		RetryMax:        cfg.Worker.AutoRetry.MaxRetries,
		RetryDelay:      cfg.Worker.AutoRetry.BaseDelay.String(),
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

	closeSTTCache(shutdownCtx, log)

	bridge.Shutdown(shutdownCtx)
	opencodeserver.ShutdownSingleton(shutdownCtx)

	if jwtValidator != nil {
		jwtValidator.Stop()
	}

	cleanupWG.Wait()
	pidTracker.RemoveAll()

	if err := sm.Close(); err != nil {
		log.Warn("gateway: session manager close", "err", err)
	}

	if err := eventCollector.Close(); err != nil {
		log.Warn("gateway: event collector close", "err", err)
	}
	if err := eventStore.Close(); err != nil {
		log.Warn("gateway: event store close", "err", err)
	}
	if err := convStore.Close(); err != nil {
		log.Warn("gateway: conversation store close", "err", err)
	}

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Warn("gateway: http server shutdown", "err", err)
	}

	log.Info("gateway: stopped")
	return ctx.Err()
}

func loadConfig(configPath string, devMode bool) (*config.Config, error) {
	absPath, err := config.ExpandAndAbs(configPath)
	if err != nil {
		return nil, fmt.Errorf("config: resolve path %q: %w", configPath, err)
	}

	loadEnvFile(filepath.Dir(absPath))

	cfg, err := config.Load(absPath, config.LoadOptions{})
	if err != nil {
		return nil, fmt.Errorf("config: load %q: %w", absPath, err)
	}
	if devMode {
		cfg.Security.APIKeys = nil
		cfg.Admin.Tokens = nil
	}
	return cfg, nil
}

func loadEnvFile(dir string) {
	envPath := filepath.Join(dir, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return
	}

	var loaded int
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
			loaded++
		}
	}
	if loaded > 0 {
		fmt.Fprintf(os.Stderr, "  env loaded %d vars from %s\n", loaded, envPath)
	}
}

func waitForSignal() os.Signal {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	return <-sig
}
