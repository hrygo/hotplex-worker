package admin

import (
	"net/http"
	"strconv"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

func addCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Api-Key")
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
		id = a.newSessionID()
	}
	if userID == "" {
		userID = "anonymous"
	}

	if err := a.bridge.StartSession(r.Context(), id, userID, "", wt, nil, "", "", nil, ""); err != nil {
		a.log.Error("admin: create session failed", "err", err)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	a.log.Info("admin: session created",
		"session_id", id, "user_id", userID, "worker_type", string(wt),
		"admin", adminKeyPrefix(r))

	respondJSON(w, map[string]string{"session_id": id})
}

func (a *AdminAPI) ListSessions(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionRead) {
		http.Error(w, "insufficient scope: need session:read", http.StatusForbidden)
		return
	}
	limit := 100
	offset := 0
	platform := r.URL.Query().Get("platform")
	userID := r.URL.Query().Get("user_id")

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

	sessions, err := a.sm.List(r.Context(), userID, platform, limit, offset)
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
	si, err := a.sm.Get(r.Context(), id)
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
		a.log.Error("admin: delete session failed", "session_id", id, "err", err)
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}

	a.log.Info("admin: session deleted",
		"session_id", id, "admin", adminKeyPrefix(r))

	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminAPI) TerminateSession(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionWrite) {
		http.Error(w, "insufficient scope: need session:write", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if err := a.sm.Transition(r.Context(), id, events.StateTerminated); err != nil {
		a.log.Warn("admin: terminate session failed", "id", id, "err", err, "admin", adminKeyPrefix(r))
		http.Error(w, "failed to terminate session", http.StatusInternalServerError)
		return
	}

	a.log.Info("admin: session terminated",
		"session_id", id, "admin", adminKeyPrefix(r))

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

func (a *AdminAPI) HandleSessionStats(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeSessionRead) {
		http.Error(w, "insufficient scope: need session:read", http.StatusForbidden)
		return
	}
	if a.turnStore == nil {
		http.Error(w, "turn stats not available", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	stats, err := a.turnStore.TurnStats(r.Context(), id)
	if err != nil {
		if r.Context().Err() != nil {
			http.Error(w, "request cancelled", http.StatusServiceUnavailable)
			return
		}
		a.log.Warn("admin: session stats", "id", id, "err", err)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	respondJSON(w, stats)
}

// adminKeyPrefix returns a truncated prefix of the admin token for audit logging.
func adminKeyPrefix(r *http.Request) string {
	token := extractBearerToken(r)
	if token == "" {
		return "none"
	}
	if len(token) > 8 {
		return token[:8] + "..."
	}
	return token + "..."
}
