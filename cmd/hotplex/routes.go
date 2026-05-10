package main

import (
	"encoding/json"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hrygo/hotplex/internal/admin"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/docs"
	"github.com/hrygo/hotplex/internal/gateway"
)

func setupRoutes(
	mux *http.ServeMux,
	deps *GatewayDeps,
) http.Handler {
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

	gatewayAPI := gateway.NewGatewayAPI(log, auth, sm, bridge, deps.ConfigStore, deps.EventStore, deps.EventStore)

	// withCORS wraps a handler to inject CORS headers.
	withCORS := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Api-Key")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			h(w, r)
		}
	}

	mux.HandleFunc("GET /api/sessions", withCORS(gatewayAPI.ListSessions))
	mux.HandleFunc("POST /api/sessions", withCORS(gatewayAPI.CreateSession))
	mux.HandleFunc("GET /api/sessions/{id}", withCORS(gatewayAPI.GetSession))
	mux.HandleFunc("DELETE /api/sessions/{id}", withCORS(gatewayAPI.DeleteSession))
	mux.HandleFunc("POST /api/sessions/{id}/cd", withCORS(gatewayAPI.SwitchWorkDir))
	mux.HandleFunc("GET /api/sessions/{id}/history", withCORS(gatewayAPI.GetHistory))
	mux.HandleFunc("GET /api/sessions/{id}/events", withCORS(gatewayAPI.GetEvents))
	mux.HandleFunc("OPTIONS /api/sessions", withCORS(func(w http.ResponseWriter, r *http.Request) {}))
	mux.HandleFunc("OPTIONS /api/sessions/", withCORS(func(w http.ResponseWriter, r *http.Request) {}))
	mux.HandleFunc("OPTIONS /api/sessions/{id}", withCORS(func(w http.ResponseWriter, r *http.Request) {}))
	mux.HandleFunc("OPTIONS /api/sessions/{id}/history", withCORS(func(w http.ResponseWriter, r *http.Request) {}))
	mux.HandleFunc("OPTIONS /api/sessions/{id}/events", withCORS(func(w http.ResponseWriter, r *http.Request) {}))

	mux.Handle("GET /ws", hub.HandleHTTP(auth, handler, bridge))

	sessionAdapter := &sessionManagerAdapter{sm: sm}
	hubAdapter := &hubAdapter{hub: hub}
	bridgeAdapter := &bridgeAdapter{bridge: bridge}
	configAdapter := &configAdapter{cfgStore: deps.ConfigStore}
	configWatcherAdapter := &configWatcherAdapter{watcher: configWatcher}
	turnsAdapter := &turnsStoreAdapter{es: deps.EventStore}

	var cronProvider admin.CronSchedulerProvider
	if deps.CronScheduler != nil {
		cronProvider = &cronAdminAdapter{scheduler: deps.CronScheduler, turnsStore: deps.EventStore}
	}

	adminAPI := admin.New(admin.Deps{
		Log:           log,
		Config:        configAdapter,
		SessionMgr:    sessionAdapter,
		TurnStats:     turnsAdapter,
		Hub:           hubAdapter,
		Bridge:        bridgeAdapter,
		ConfigWatcher: configWatcherAdapter,
		Cron:          cronProvider,
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

	adminMux.HandleFunc("GET /admin/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	adminMux.Handle("GET /admin/metrics", promhttp.Handler())

	adminMux.HandleFunc("GET /admin/stats", adminAPI.HandleStats)
	adminMux.HandleFunc("GET /admin/health/workers", adminAPI.HandleWorkerHealth)
	adminMux.HandleFunc("GET /admin/health", adminAPI.HandleHealth)
	adminMux.HandleFunc("GET /admin/logs", adminAPI.HandleLogs)
	adminMux.HandleFunc("POST /admin/config/validate", adminAPI.HandleConfigValidate)
	adminMux.HandleFunc("POST /admin/config/rollback", adminAPI.HandleConfigRollback)
	adminMux.HandleFunc("GET /admin/debug/sessions/{id}", adminAPI.HandleDebugSession)

	adminMux.HandleFunc("GET /admin/sessions", adminAPI.ListSessions)
	adminMux.HandleFunc("GET /admin/sessions/{id}", adminAPI.GetSession)
	adminMux.HandleFunc("DELETE /admin/sessions/{id}", adminAPI.DeleteSession)
	adminMux.HandleFunc("POST /admin/sessions/{id}/terminate", adminAPI.TerminateSession)
	adminMux.HandleFunc("GET /admin/sessions/{id}/stats", adminAPI.HandleSessionStats)

	// Cron API
	adminMux.HandleFunc("GET /api/cron/jobs", adminAPI.HandleCronList)
	adminMux.HandleFunc("GET /api/cron/jobs/{id}", adminAPI.HandleCronGet)
	adminMux.HandleFunc("POST /api/cron/jobs", adminAPI.HandleCronCreate)
	adminMux.HandleFunc("PATCH /api/cron/jobs/{id}", adminAPI.HandleCronUpdate)
	adminMux.HandleFunc("DELETE /api/cron/jobs/{id}", adminAPI.HandleCronDelete)
	adminMux.HandleFunc("POST /api/cron/jobs/{id}/run", adminAPI.HandleCronTrigger)
	adminMux.HandleFunc("GET /api/cron/jobs/{id}/runs", adminAPI.HandleCronRunHistory)

	// Documentation
	mux.Handle("GET /docs/", http.StripPrefix("/docs", docs.Handler()))

	// Webchat SPA is NOT registered on the mux directly.
	// Instead, the caller wraps the mux with a fallback handler below.
	return adminAPI.Middleware(adminMux)
}
