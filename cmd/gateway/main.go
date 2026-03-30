// Package main is the entry point for the HotPlex Worker Gateway.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"hotplex-worker/internal/aep"
	"hotplex-worker/internal/config"
	"hotplex-worker/internal/gateway"
	"hotplex-worker/internal/security"
	"hotplex-worker/internal/session"
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

	// Load configuration.
	cfg := loadConfig()

	log.Info("gateway: starting",
		"version", version(),
		"go", runtime.Version(),
		"addr", cfg.Gateway.Addr,
		"config", *flagConfig,
	)

	// Initialize components.
	store, err := session.NewSQLiteStore(ctx, cfg)
	if err != nil {
		return err
	}

	sm, err := session.NewManager(ctx, log, cfg, store)
	if err != nil {
		return err
	}

	// Hook session termination (e.g. for logging or extra cleanup).
	sm.OnTerminate = func(sessionID string) {
		log.Info("gateway: session terminated", "session_id", sessionID)
	}

	hub := gateway.NewHub(log, cfg)

	sm.StateNotifier = func(ctx context.Context, sessionID string, state events.SessionState, message string) {
		env := events.NewEnvelope(aep.NewID(), sessionID, hub.NextSeq(sessionID), events.State, events.StateData{
			State:   state,
			Message: message,
		})
		_ = hub.SendToSession(ctx, env)
	}

	auth := security.NewAuthenticator(&cfg.Security)
	handler := gateway.NewHandler(log, cfg, hub, sm)
	bridge := gateway.NewBridge(log, hub, sm)

	// Register HTTP routes.
	mux := http.NewServeMux()
	setupRoutes(mux, log, cfg, hub, sm, auth, handler, bridge)

	server := &http.Server{
		Addr:         cfg.Gateway.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.Gateway.IdleTimeout,
		WriteTimeout: cfg.Gateway.WriteTimeout,
	}

	// Start hub's broadcast loop.
	go hub.Run()

	// Start HTTP server.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("gateway: listening", "addr", cfg.Gateway.Addr)
		serverErr <- server.ListenAndServe()
	}()

	// Wait for shutdown signal.
	sig := waitForSignal()
	log.Info("gateway: shutdown", "signal", sig)

	// Graceful shutdown.
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := hub.Shutdown(shutdownCtx); err != nil {
		log.Warn("gateway: hub shutdown", "err", err)
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
	if *flagConfig != "" {
		cfg, err := config.Load(*flagConfig)
		if err != nil {
			slog.Error("config: load failed", "path", *flagConfig, "err", err)
			os.Exit(1)
		}
		if *flagDev {
			cfg.Security.APIKeys = nil
		}
		return cfg
	}
	cfg := config.Default()
	if *flagDev {
		cfg.Security.APIKeys = nil
	}
	return cfg
}

func setupRoutes(
	mux *http.ServeMux,
	log *slog.Logger,
	cfg *config.Config,
	hub *gateway.Hub,
	sm *session.Manager,
	auth *security.Authenticator,
	handler *gateway.Handler,
	bridge *gateway.Bridge,
) {
	// Health check.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// WebSocket endpoint (AEP v1 handshake handled in ReadPump).
	mux.Handle("GET /ws", hub.HandleHTTP(auth, sm, handler, bridge))

	// Admin API.
	admin := &AdminAPI{
		log:    log,
		sm:     sm,
		hub:    hub,
		bridge: bridge,
	}
	mux.Handle("POST /admin/sessions", auth.Middleware(http.HandlerFunc(admin.CreateSession)))
	mux.Handle("GET /admin/sessions", auth.Middleware(http.HandlerFunc(admin.ListSessions)))
	mux.Handle("GET /admin/sessions/{id}", auth.Middleware(http.HandlerFunc(admin.GetSession)))
	mux.Handle("DELETE /admin/sessions/{id}", auth.Middleware(http.HandlerFunc(admin.DeleteSession)))
	mux.Handle("POST /admin/sessions/{id}/terminate", auth.Middleware(http.HandlerFunc(admin.TerminateSession)))
	mux.Handle("GET /admin/pool/stats", auth.Middleware(http.HandlerFunc(admin.PoolStats)))
}

func respondJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// AdminAPI handles administrative operations.
type AdminAPI struct {
	log    *slog.Logger
	sm     *session.Manager
	hub    *gateway.Hub
	bridge *gateway.Bridge
}

func (a *AdminAPI) CreateSession(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("session_id")
	userID := r.URL.Query().Get("user_id")
	wt := worker.WorkerType(r.URL.Query().Get("worker_type"))
	if wt == "" {
		wt = worker.TypeClaudeCode
	}
	if id == "" {
		id = newSessionID()
	}
	if userID == "" {
		userID = "anonymous"
	}

	if err := a.bridge.StartSession(r.Context(), id, userID, wt); err != nil {
		a.log.Error("admin: create session", "err", err)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]string{"session_id": id})
}

func (a *AdminAPI) ListSessions(w http.ResponseWriter, r *http.Request) {
	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	sessions, err := a.sm.List(r.Context(), limit, offset)
	if err != nil {
		a.log.Error("admin: list sessions", "err", err)
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]any{
		"sessions": sessions,
		"limit":    limit,
		"offset":   offset,
	})
}

func (a *AdminAPI) GetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	si, err := a.sm.Get(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	respondJSON(w, si)
}

func (a *AdminAPI) DeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.sm.Delete(r.Context(), id); err != nil {
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminAPI) TerminateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.sm.Transition(r.Context(), id, events.StateTerminated); err != nil {
		a.log.Warn("admin: terminate session", "id", id, "err", err)
		http.Error(w, "failed to terminate session", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminAPI) PoolStats(w http.ResponseWriter, r *http.Request) {
	total, max, users := a.sm.Stats()
	respondJSON(w, map[string]int{
		"total": total,
		"max":   max,
		"users": users,
	})
}

func waitForSignal() os.Signal {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	return <-sig
}

func version() string {
	return "v1.0.0"
}

// newSessionID returns a new session ID using aep.NewSessionID().
func newSessionID() string {
	return aep.NewSessionID()
}
