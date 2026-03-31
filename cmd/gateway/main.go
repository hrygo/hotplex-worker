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
	"net/netip"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

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

	// Load configuration.
	cfg := loadConfig()

	// Initialize OpenTelemetry tracing (idempotent, optional).
	tracing.Init(ctx, log, "hotplex-worker-gateway")

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

	// EVT-004/EVT-011: optional event persistence via pluggable MessageStore builder.
	// Controlled via session.event_store_enabled and session.event_store_type in config.
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

	// Hook session termination (e.g. for logging or extra cleanup).
	sm.OnTerminate = func(sessionID string) {
		log.Info("gateway: session terminated", "session_id", sessionID)
	}

	hub := gateway.NewHub(log, cfg)

	// ADMIN-008: wire hub event logging into the ring buffer.
	hub.LogHandler = func(level, msg, sessionID string) {
		logRing.Add(level, msg, sessionID)
	}

	// CONFIG-006/007/008: Start file-system config watcher for hot reload.
	// Only active when a config file path was provided (flagConfig != "").
	var configWatcher *config.Watcher
	if *flagConfig != "" {
		configWatcher = config.NewWatcher(log, *flagConfig, nil,
			func(newCfg *config.Config) {
				// Hot update: apply non-static fields.
				// The hub, sm, and other components read cfg at startup;
				// for hot-reloadable fields, update the live config reference.
				log.Info("config: hot reload applied",
					"gateway.addr", newCfg.Gateway.Addr,
					"pool.max_size", newCfg.Pool.MaxSize,
					"gc_scan_interval", newCfg.Session.GCScanInterval,
				)
				// Update the live cfg reference for components that re-read it.
				// Note: components that captured cfg at startup (like Hub's broadcastQueueSize)
				// will use their captured values until restarted.
				cfg = newCfg
			},
			func(field string) {
				// Static field changed — log but do not apply (requires restart).
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

	// Initialize JWT validator if secret is configured (loaded via config.Load secrets provider).
	// Pass it to NewAuthenticator so botID can be extracted from JWT at HTTP upgrade time.
	var jwtValidator *security.JWTValidator
	if len(cfg.Security.JWTSecret) > 0 {
		jwtValidator = security.NewJWTValidator(cfg.Security.JWTSecret, cfg.Security.JWTAudience)
	}

	auth := security.NewAuthenticator(&cfg.Security, jwtValidator)

	handler := gateway.NewHandler(log, cfg, hub, sm, jwtValidator)
	bridge := gateway.NewBridge(log, hub, sm, msgStore)

	// Register HTTP routes.
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
		// Dev mode: disable auth requirements.
		cfg.Security.APIKeys = nil
		cfg.Admin.Tokens = nil
	}
	return cfg
}

// GatewayDeps collects the dependencies needed by setupRoutes.
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
	// Health check (no auth).
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Admin /admin/health and /admin/health/ready — unauthenticated per RFC 7807.
	// Registered before the admin mux so they bypass auth middleware.
	// Note: admin is created below after adminMux is set up.
	mux.HandleFunc("GET /admin/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Prometheus metrics endpoint.
	mux.Handle("GET /admin/metrics", promhttp.Handler())

	// WebSocket endpoint (AEP v1 handshake handled in ReadPump).
	mux.Handle("GET /ws", hub.HandleHTTP(auth, sm, handler, bridge))

	// Admin API (requires auth middleware).
	admin := &AdminAPI{
		log:    log,
		cfg:    cfg,
		sm:     sm,
		hub:    hub,
		bridge: bridge,
		configWatcher: configWatcher,
	}
	// Initialize middleware components once at startup.
	if cfg.Admin.RateLimitEnabled {
		admin.rateLimiter = newRateLimiter(cfg.Admin.RequestsPerSec, cfg.Admin.Burst)
	}
	if cfg.Admin.IPWhitelistEnabled {
		admin.allowedCIDRs = cfg.Admin.AllowedCIDRs
	}
	adminMux := admin.Mux()

	// Wire all admin routes.
	adminMux.HandleFunc("GET /admin/stats", admin.HandleStats)
	// /admin/health/workers requires auth (ScopeHealthRead).
	adminMux.HandleFunc("GET /admin/health/workers", admin.HandleWorkerHealth)
	adminMux.HandleFunc("GET /admin/logs", admin.HandleLogs)
	adminMux.HandleFunc("POST /admin/config/validate", admin.HandleConfigValidate)
	adminMux.HandleFunc("POST /admin/config/rollback", admin.HandleConfigRollback)
	adminMux.HandleFunc("GET /admin/debug/sessions/{id}", admin.HandleDebugSession)

	// Session CRUD.
	adminMux.HandleFunc("GET /admin/sessions", admin.ListSessions)
	adminMux.HandleFunc("GET /admin/sessions/{id}", admin.GetSession)
	adminMux.HandleFunc("DELETE /admin/sessions/{id}", admin.DeleteSession)
	adminMux.HandleFunc("POST /admin/sessions/{id}/terminate", admin.TerminateSession)

	// /admin/health — full gateway health check, no auth required.
	mux.HandleFunc("GET /admin/health", admin.HandleHealth)

	// Mount admin mux with middleware chain.
	// NOTE: /admin/health and /admin/health/ready are registered above (before this line)
	// and therefore bypass the auth middleware.
	mux.Handle("/admin/", admin.Middleware(adminMux))
}

func respondJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// Admin API scope constants (per RBAC Permission Matrix).
const (
	ScopeSessionRead  = "session:read"   // list/get sessions
	ScopeSessionWrite = "session:write"  // create/modify/terminate sessions
	ScopeSessionKill  = "session:delete" // force delete sessions
	ScopeStatsRead    = "stats:read"     // read statistics
	ScopeHealthRead   = "health:read"    // read health checks
	ScopeConfigRead   = "config:read"    // read/validate config
	ScopeAdminRead    = "admin:read"     // debug logs, debug session
	ScopeAdminWrite   = "admin:write"    // write admin ops
)

// scopeContextKey is the context key for the authenticated scopes.
type scopeContextKey struct{}

// getScopes returns the scopes stored in the request context by Middleware.
func getScopes(r *http.Request) []string {
	if scopes, ok := r.Context().Value(scopeContextKey{}).([]string); ok {
		return scopes
	}
	return nil
}

// hasScope reports whether the request has the required scope.
func hasScope(r *http.Request, required string) bool {
	for _, s := range getScopes(r) {
		if s == required {
			return true
		}
	}
	return false
}

// AdminAPI handles administrative operations.
type AdminAPI struct {
	log          *slog.Logger
	cfg          *config.Config
	sm           *session.Manager
	hub          *gateway.Hub
	bridge       *gateway.Bridge
	configWatcher *config.Watcher // CONFIG-009: for config rollback
	rateLimiter  *simpleRateLimiter
	allowedCIDRs []string
}

// Middleware returns an http.Handler with all auth middleware applied:
// IP whitelist → Rate limit → Bearer token → scopes injected into context.
func (a *AdminAPI) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Rate limit.
		if a.rateLimiter != nil {
			if !a.rateLimiter.Allow() {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
		}

		// IP whitelist.
		if len(a.allowedCIDRs) > 0 {
			addr := clientIP(r)
			if !ipAllowed(addr, a.allowedCIDRs) {
				a.log.Warn("admin: IP not whitelisted", "ip", addr)
				http.Error(w, "IP not allowed", http.StatusForbidden)
				return
			}
		}

		// Bearer token.
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

		// Inject scopes into context for per-handler scope checks.
		ctx := context.WithValue(r.Context(), scopeContextKey{}, scopes)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// clientIP extracts the real client IP from the request, respecting X-Forwarded-For.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			xff = xff[:idx]
		}
		return strings.TrimSpace(xff)
	}
	// Strip port from RemoteAddr.
	host, _, _ := strings.Cut(r.RemoteAddr, ":")
	return host
}

// extractBearerToken extracts the bearer token from the Authorization header or query string.
func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return h[7:]
	}
	return r.URL.Query().Get("access_token")
}

// Mux returns a new ServeMux for admin routes.
func (a *AdminAPI) Mux() *http.ServeMux {
	return http.NewServeMux()
}

// HandleStats returns gateway/worker/database statistics.
func (a *AdminAPI) HandleStats(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeStatsRead) {
		http.Error(w, "insufficient scope: need stats:read", http.StatusForbidden)
		return
	}
	total, _, _ := a.sm.Stats()
	sessions, _ := a.sm.List(r.Context(), 0, 0)

	byType := make(map[string]map[string]any)
	for _, si := range sessions {
		key := string(si.WorkerType)
		if byType[key] == nil {
			byType[key] = map[string]any{
				"sessions":        0,
				"avg_memory_mb":   0,
				"avg_cpu_percent": 0,
			}
		}
		m := byType[key]
		m["sessions"] = m["sessions"].(int) + 1
	}

	respondJSON(w, map[string]any{
		"gateway": map[string]any{
			"uptime_seconds":        int(time.Since(startTime).Seconds()),
			"websocket_connections": a.hub.ConnectionsOpen(),
			"sessions_active":       total,
			"sessions_total":        len(sessions),
		},
		"workers": byType,
		"database": map[string]any{
			"sessions_count": len(sessions),
			"db_size_mb":     0, // placeholder
		},
	})
}

// HandleHealth returns the gateway health check response.
// No scope required — this endpoint is intentionally unauthenticated
// to support Kubernetes liveness/readiness probes and load balancers.
func (a *AdminAPI) HandleHealth(w http.ResponseWriter, r *http.Request) {
	dbHealthy := true
	if _, err := a.sm.List(r.Context(), 1, 0); err != nil {
		dbHealthy = false
	}

	status := "healthy"
	if !dbHealthy {
		status = "degraded"
	}

	respondJSON(w, map[string]any{
		"status": status,
		"checks": map[string]any{
			"gateway": map[string]any{
				"status":         "healthy",
				"uptime_seconds": int(time.Since(startTime).Seconds()),
			},
			"database": map[string]any{
				"status": map[bool]string{true: "healthy", false: "unhealthy"}[dbHealthy],
				"type":   "sqlite",
				"path":   a.cfg.DB.Path,
			},
			"workers": map[string]any{
				"status": "healthy",
			},
		},
		"version": version(),
	})
}

// HandleWorkerHealth returns per-worker health status for all active sessions.
func (a *AdminAPI) HandleWorkerHealth(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeHealthRead) {
		http.Error(w, "insufficient scope: need health:read", http.StatusForbidden)
		return
	}

	statuses := a.sm.WorkerHealthStatuses()
	allHealthy := true
	for _, ws := range statuses {
		if !ws.Healthy {
			allHealthy = false
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	body, _ := json.Marshal(map[string]any{
		"status":     map[bool]string{true: "ok", false: "degraded"}[allHealthy],
		"workers":    statuses,
		"checked_at": time.Now().UTC().Format(time.RFC3339),
	})
	if !allHealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_, _ = w.Write(body)
}

// HandleLogs returns recent log entries.
func (a *AdminAPI) HandleLogs(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminRead) {
		http.Error(w, "insufficient scope: need admin:read", http.StatusForbidden)
		return
	}
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	logs := logRing.Recent(limit)
	if logs == nil {
		logs = []logEntry{}
	}
	respondJSON(w, map[string]any{
		"logs":  logs,
		"total": logRing.n,
		"limit": limit,
	})
}

// HandleConfigValidate validates the gateway configuration.
// It parses the request body as a config YAML/JSON fragment and runs
// structural and business-rule validation against it.
func (a *AdminAPI) HandleConfigValidate(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeConfigRead) {
		http.Error(w, "insufficient scope: need config:read", http.StatusForbidden)
		return
	}
	if r.Body == nil {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}
	var body struct {
		Gateway  *struct {
			Addr               string `json:"addr"`
			ReadBufferSize     int    `json:"read_buffer_size"`
			WriteBufferSize    int    `json:"write_buffer_size"`
			BroadcastQueueSize  int    `json:"broadcast_queue_size"`
		} `json:"gateway"`
		DB *struct {
			Path string `json:"path"`
		} `json:"db"`
		Worker *struct {
			IdleTimeout      string `json:"idle_timeout"`
			ExecutionTimeout string `json:"execution_timeout"`
		} `json:"worker"`
		Security *struct {
			TLSEnabled bool `json:"tls_enabled"`
		} `json:"security"`
		Session *struct {
			RetentionPeriod string `json:"retention_period"`
			GCScanInterval  string `json:"gc_scan_interval"`
		} `json:"session"`
		Pool *struct {
			MaxSize int `json:"max_size"`
		} `json:"pool"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	var validationErrs []string
	var warnings []string

	// Gateway validation.
	if body.Gateway != nil {
		if body.Gateway.ReadBufferSize < 0 {
			validationErrs = append(validationErrs, "gateway.read_buffer_size must be non-negative")
		}
		if body.Gateway.WriteBufferSize < 0 {
			validationErrs = append(validationErrs, "gateway.write_buffer_size must be non-negative")
		}
		if body.Gateway.BroadcastQueueSize < 0 {
			validationErrs = append(validationErrs, "gateway.broadcast_queue_size must be non-negative")
		}
	}

	// DB validation.
	if body.DB != nil {
		if body.DB.Path != "" && (len(body.DB.Path) > 4096) {
			validationErrs = append(validationErrs, "db.path exceeds maximum length")
		}
	}

	// Pool validation.
	if body.Pool != nil {
		if body.Pool.MaxSize <= 0 {
			validationErrs = append(validationErrs, "pool.max_size must be positive")
		}
		if body.Pool.MaxSize > 10000 {
			validationErrs = append(validationErrs, "pool.max_size must not exceed 10000")
		}
	}

	valid := len(validationErrs) == 0
	if len(a.cfg.Security.APIKeys) == 0 {
		warnings = append(warnings, "no API keys configured; running in open-access mode")
	}

	status := http.StatusOK
	if !valid {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	respondJSON(w, map[string]any{
		"valid":    valid,
		"errors":   validationErrs,
		"warnings": warnings,
	})
}

// HandleConfigRollback rolls back the configuration to a previous version.
// version=1 reverts to the immediately previous config; version=2 to two steps back, etc.
func (a *AdminAPI) HandleConfigRollback(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeConfigRead) {
		http.Error(w, "insufficient scope: need config:read", http.StatusForbidden)
		return
	}
	if a.configWatcher == nil {
		http.Error(w, "config rollback is not available (no config file specified)", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Version int `json:"version"` // steps back: 1 = previous, 2 = two steps back, etc.
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Version < 1 {
		http.Error(w, "version must be a positive integer", http.StatusBadRequest)
		return
	}

	_, idx, err := a.configWatcher.Rollback(body.Version)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	a.log.Info("config: rollback applied", "version", body.Version, "history_index", idx)
	respondJSON(w, map[string]any{
		"ok":            true,
		"rolled_back":   body.Version,
		"history_index": idx,
	})
}
func (a *AdminAPI) HandleDebugSession(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeAdminRead) {
		http.Error(w, "insufficient scope: need admin:read", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	si, err := a.sm.Get(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Collect extended debug info from session manager via DebugSnapshot
	// (safe: lock acquisition happens inside the session package to respect lock ordering).
	snap, _ := a.sm.DebugSnapshot(id)
	respondJSON(w, map[string]any{
		"session": map[string]any{
			"id":          si.ID,
			"state":       si.State,
			"user_id":     si.UserID,
			"worker_type": si.WorkerType,
			"created_at":  si.CreatedAt,
			"updated_at":  si.UpdatedAt,
		},
		"debug": map[string]any{
			"has_worker":     snap.HasWorker,
			"turn_count":    snap.TurnCount,
			"last_seq_sent": a.hub.NextSeqPeek(id),
			"worker_health": snap.WorkerHealth,
		},
	})
}

// validateToken checks the admin bearer token against configured values.
// Returns scopes for the token, or (nil, false) if invalid.
func (a *AdminAPI) validateToken(token string) ([]string, bool) {
	// Check token-specific scopes first.
	if a.cfg.Admin.TokenScopes != nil {
		if scopes, ok := a.cfg.Admin.TokenScopes[token]; ok {
			return scopes, true
		}
	}
	// Fall back to tokens list with default scopes.
	for _, t := range a.cfg.Admin.Tokens {
		if t == token {
			if len(a.cfg.Admin.DefaultScopes) > 0 {
				return a.cfg.Admin.DefaultScopes, true
			}
			return []string{ScopeSessionRead, ScopeStatsRead, ScopeHealthRead}, true
		}
	}
	return nil, false
}

// ipAllowed reports whether addr matches any allowed CIDR.
func ipAllowed(addr string, cidrs []string) bool {
	if len(cidrs) == 0 {
		return true
	}
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return false
	}
	for _, cidr := range cidrs {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			continue
		}
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// newRateLimiter creates a token-bucket rate limiter.
func newRateLimiter(reqPerSec, burst int) *simpleRateLimiter {
	return &simpleRateLimiter{
		tokens:     float64(burst),
		maxTokens:  float64(burst),
		refillRate: float64(reqPerSec),
		lastRefill: time.Now(),
	}
}

type simpleRateLimiter struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

func (r *simpleRateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	elapsed := time.Since(r.lastRefill).Seconds()
	r.tokens = min(r.maxTokens, r.tokens+elapsed*r.refillRate)
	r.lastRefill = time.Now()
	if r.tokens >= 1 {
		r.tokens--
		return true
	}
	return false
}

var startTime = time.Now()

// logRing is a thread-safe ring buffer for recent log entries served by /admin/logs.
var logRing = newLogRing(100)

type logEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Msg     string `json:"msg"`
	Session string `json:"session_id,omitempty"`
}

type logRingBuffer struct {
	mu   sync.Mutex
	ent  []logEntry
	head int
	n    int // total entries ever added
}

func newLogRing(cap int) *logRingBuffer {
	return &logRingBuffer{ent: make([]logEntry, cap)}
}

func (r *logRingBuffer) Add(level, msg, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ent[r.head%len(r.ent)] = logEntry{
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Level:   level,
		Msg:     msg,
		Session: sessionID,
	}
	r.head++
	r.n++
}

func (r *logRingBuffer) Recent(limit int) []logEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.n == 0 {
		return nil
	}
	size := len(r.ent)
	if r.n < size {
		size = r.n
	}
	if limit > 0 && limit < size {
		size = limit
	}
	// start from oldest
	start := (r.head - size) % len(r.ent)
	out := make([]logEntry, 0, size)
	for i := 0; i < size; i++ {
		idx := (start + i) % len(r.ent)
		out = append(out, r.ent[idx])
	}
	return out
}

func (a *AdminAPI) CreateSession(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionWrite) {
		http.Error(w, "insufficient scope: need session:write", http.StatusForbidden)
		return
	}
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
	if !hasScope(r, ScopeSessionRead) {
		http.Error(w, "insufficient scope: need session:read", http.StatusForbidden)
		return
	}
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
	if !hasScope(r, ScopeSessionRead) {
		http.Error(w, "insufficient scope: need session:read", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	si, err := a.sm.Get(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	respondJSON(w, si)
}

func (a *AdminAPI) DeleteSession(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionKill) {
		http.Error(w, "insufficient scope: need session:delete", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if err := a.sm.Delete(r.Context(), id); err != nil {
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminAPI) TerminateSession(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionWrite) {
		http.Error(w, "insufficient scope: need session:write", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if err := a.sm.Transition(r.Context(), id, events.StateTerminated); err != nil {
		a.log.Warn("admin: terminate session", "id", id, "err", err)
		http.Error(w, "failed to terminate session", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminAPI) PoolStats(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeStatsRead) {
		http.Error(w, "insufficient scope: need stats:read", http.StatusForbidden)
		return
	}
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
