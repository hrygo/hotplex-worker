package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

func (a *AdminAPI) HandleStats(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeStatsRead) {
		http.Error(w, "insufficient scope: need stats:read", http.StatusForbidden)
		return
	}
	total, _, _ := a.sm.Stats()
	sessions, _ := a.sm.List(r.Context(), "", "", 0, 0)

	byType := make(map[string]map[string]any)
	for _, si := range sessions {
		siMap, ok := si.(map[string]any)
		if !ok {
			continue
		}
		key, _ := siMap["worker_type"].(string)
		if byType[key] == nil {
			byType[key] = map[string]any{
				"sessions":        0,
				"avg_memory_mb":   0,
				"avg_cpu_percent": 0,
			}
		}
		m := byType[key]
		m["sessions"] = m["sessions"].(int) + 1 //nolint:errcheck // guaranteed by filter logic
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
			"db_size_mb":     0,
		},
	})
}

func (a *AdminAPI) HandleHealth(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Get()
	dbHealthy := true
	if _, err := a.sm.List(r.Context(), "", "", 1, 0); err != nil {
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
				"path":   cfg.DB.Path,
			},
			"workers": map[string]any{
				"status": "healthy",
			},
		},
		"version": a.version(),
	})
}

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
	logs := LogRing.Recent(limit)
	if logs == nil {
		logs = []logEntry{}
	}
	respondJSON(w, map[string]any{
		"logs":  logs,
		"total": LogRing.Total(),
		"limit": limit,
	})
}

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
		Gateway *struct {
			Addr               string `json:"addr"`
			ReadBufferSize     int    `json:"read_buffer_size"`
			WriteBufferSize    int    `json:"write_buffer_size"`
			BroadcastQueueSize int    `json:"broadcast_queue_size"`
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

	if body.DB != nil {
		if body.DB.Path != "" && len(body.DB.Path) > 4096 {
			validationErrs = append(validationErrs, "db.path exceeds maximum length")
		}
	}

	if body.Pool != nil {
		if body.Pool.MaxSize <= 0 {
			validationErrs = append(validationErrs, "pool.max_size must be positive")
		}
		if body.Pool.MaxSize > 10000 {
			validationErrs = append(validationErrs, "pool.max_size must not exceed 10000")
		}
	}

	valid := len(validationErrs) == 0
	cfg := a.cfg.Get()
	if len(cfg.Security.APIKeys) == 0 {
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
		Version int `json:"version"`
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

	snap, _ := a.sm.DebugSnapshot(id)

	siMap, _ := si.(map[string]any)
	respondJSON(w, map[string]any{
		"session": siMap,
		"debug": map[string]any{
			"has_worker":    snap.HasWorker,
			"turn_count":    snap.TurnCount,
			"last_seq_sent": a.hub.NextSeqPeek(id),
			"worker_health": snap.WorkerHealth,
		},
	})
}
