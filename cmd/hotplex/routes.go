package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hrygo/hotplex/internal/admin"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/gateway"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

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
