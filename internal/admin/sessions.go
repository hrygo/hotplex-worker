package admin

import (
	"net/http"
	"strconv"

	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

func addCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Api-Key")
}

// Preflight handles CORS preflight requests (OPTIONS).
func preflight(w http.ResponseWriter) {
	addCORSHeaders(w)
	w.WriteHeader(http.StatusOK)
}

func (a *AdminAPI) CreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		preflight(w)
		return
	}
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

	if err := a.bridge.StartSession(r.Context(), id, userID, "", wt, nil, "", "", nil); err != nil {
		a.log.Error("admin: create session", "err", err)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	addCORSHeaders(w)
	respondJSON(w, map[string]string{"session_id": id})
}

func (a *AdminAPI) ListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		preflight(w)
		return
	}
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

	addCORSHeaders(w)
	respondJSON(w, map[string]any{
		"sessions": sessions,
		"limit":    limit,
		"offset":   offset,
	})
}

func (a *AdminAPI) GetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		preflight(w)
		return
	}
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

	addCORSHeaders(w)
	respondJSON(w, si)
}

func (a *AdminAPI) DeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		preflight(w)
		return
	}
	if !hasScope(r, ScopeSessionKill) {
		http.Error(w, "insufficient scope: need session:delete", http.StatusForbidden)
		return
	}
	id := r.PathValue("id")
	if err := a.sm.Delete(r.Context(), id); err != nil {
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}

	addCORSHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminAPI) TerminateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		preflight(w)
		return
	}
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

	addCORSHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminAPI) PoolStats(w http.ResponseWriter, r *http.Request) {
	if !hasScope(r, ScopeStatsRead) {
		http.Error(w, "insufficient scope: need stats:read", http.StatusForbidden)
		return
	}
	addCORSHeaders(w)
	total, max, users := a.sm.Stats()
	respondJSON(w, map[string]int{
		"total": total,
		"max":   max,
		"users": users,
	})
}
