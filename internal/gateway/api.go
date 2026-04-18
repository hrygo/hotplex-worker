package gateway

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/hotplex/hotplex-worker/internal/security"
	"github.com/hotplex/hotplex-worker/internal/worker"

	"github.com/hotplex/hotplex-worker/pkg/aep"
)

type GatewayAPI struct {
	auth   *security.Authenticator
	sm     SessionManager
	bridge SessionStarter
}

func NewGatewayAPI(auth *security.Authenticator, sm SessionManager, bridge SessionStarter) *GatewayAPI {
	return &GatewayAPI{auth: auth, sm: sm, bridge: bridge}
}

func respondJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (g *GatewayAPI) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID, _, err := g.auth.AuthenticateRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	_ = userID
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
	sessions, err := g.sm.List(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]any{"sessions": sessions, "limit": limit, "offset": offset})
}

func (g *GatewayAPI) CreateSession(w http.ResponseWriter, r *http.Request) {
	userID, botID, err := g.auth.AuthenticateRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := r.URL.Query().Get("session_id")
	wt := worker.WorkerType(r.URL.Query().Get("worker_type"))
	if wt == "" {
		wt = worker.TypeClaudeCode
	}
	if id == "" {
		id = aep.NewSessionID()
	}
	if userID == "" {
		userID = "anonymous"
	}
	if err := g.bridge.StartSession(r.Context(), id, userID, botID, wt, nil, ""); err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]string{"session_id": id})
}

func (g *GatewayAPI) GetSession(w http.ResponseWriter, r *http.Request) {
	if _, _, err := g.auth.AuthenticateRequest(r); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	si, err := g.sm.Get(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	respondJSON(w, si)
}

func (g *GatewayAPI) DeleteSession(w http.ResponseWriter, r *http.Request) {
	if _, _, err := g.auth.AuthenticateRequest(r); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	if err := g.sm.Delete(r.Context(), id); err != nil {
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
