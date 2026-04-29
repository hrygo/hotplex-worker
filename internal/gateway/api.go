package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"

	"github.com/hrygo/hotplex/pkg/events"
)

type GatewayAPI struct {
	auth      *security.Authenticator
	sm        SessionManager
	bridge    SessionStarter
	cfgStore  *config.ConfigStore
	convStore ConversationStoreReader
	log       *slog.Logger
}

// ConversationStoreReader defines the subset of ConversationStore needed by the history API.
type ConversationStoreReader interface {
	GetBySession(ctx context.Context, sessionID string, limit, offset int) ([]*session.ConversationRecord, error)
	GetBySessionBefore(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]*session.ConversationRecord, error)
}

func NewGatewayAPI(log *slog.Logger, auth *security.Authenticator, sm SessionManager, bridge SessionStarter, cfgStore *config.ConfigStore, convStore ConversationStoreReader) *GatewayAPI {
	return &GatewayAPI{auth: auth, sm: sm, bridge: bridge, cfgStore: cfgStore, convStore: convStore, log: log.With("component", "api")}
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
	limit := 100
	offset := 0
	platform := platformWebChat // Default to webchat as requested

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
	if p := r.URL.Query().Get("platform"); p != "" {
		if p == "all" {
			platform = ""
		} else {
			platform = p
		}
	}

	sessions, err := g.sm.List(r.Context(), userID, platform, limit, offset)
	if err != nil {
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}
	respondJSON(w, map[string]any{"sessions": sessions, "limit": limit, "offset": offset, "platform": platform})
}

func (g *GatewayAPI) CreateSession(w http.ResponseWriter, r *http.Request) {
	userID, botID, err := g.auth.AuthenticateRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	title = messaging.SanitizeText(title)
	wt := worker.WorkerType(r.URL.Query().Get("worker_type"))
	if wt == "" {
		wt = worker.TypeClaudeCode
	}
	if title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if len(title) > 256 {
		http.Error(w, "title too long (max 256 chars)", http.StatusBadRequest)
		return
	}

	// Resolve work dir: use client-provided value or default from config.
	workDir := r.URL.Query().Get("work_dir")
	if workDir == "" {
		workDir = g.cfgStore.Load().Worker.DefaultWorkDir
	}
	if workDir != "" {
		if err := security.ValidateWorkDir(workDir); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Derive session ID via UUIDv5 for consistency with WebSocket path.
	// Both REST and WS use the auth userID ("anonymous" in dev mode, "api_user"
	// with API keys) so they produce the same derived session ID.
	id := session.DeriveSessionKey(userID, wt, title, workDir)

	// Default userID after derivation — bridge expects non-empty.
	if userID == "" {
		userID = "anonymous"
	}

	// Idempotency check: if session exists and is active, just return it.
	if si, err := g.sm.Get(id); err == nil {
		if si.State != events.StateDeleted {
			respondJSON(w, map[string]string{"session_id": id})
			return
		}
		// If it's deleted, we must physically remove it before re-creating
		// to avoid StateMachine transition errors and primary key conflicts.
		_ = g.sm.DeletePhysical(r.Context(), id)
	}

	if err := g.bridge.StartSession(r.Context(), id, userID, botID, wt, nil, workDir, platformWebChat, nil, title); err != nil {
		g.log.Error("gateway: create session failed", "session_id", id, "worker_type", wt, "work_dir", workDir, "err", err)
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

	// Gracefully terminate the worker through the state machine before deleting.
	// Transition sends SIGTERM → wait → SIGKILL and releases pool quota.
	if err := g.sm.Transition(r.Context(), id, events.StateTerminated); err != nil {
		g.log.Debug("gateway: pre-delete transition skipped", "session_id", id, "err", err)
	}

	if err := g.sm.DeletePhysical(r.Context(), id); err != nil {
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *GatewayAPI) SwitchWorkDir(w http.ResponseWriter, r *http.Request) {
	userID, _, err := g.auth.AuthenticateRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}

	var body struct {
		WorkDir string `json:"work_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.WorkDir == "" {
		http.Error(w, "work_dir is required", http.StatusBadRequest)
		return
	}

	// Expand ~ and resolve to absolute path.
	expanded, err := config.ExpandAndAbs(body.WorkDir)
	if err != nil {
		http.Error(w, "invalid work_dir: "+err.Error(), http.StatusBadRequest)
		return
	}
	body.WorkDir = expanded

	// Ownership check.
	si, err := g.sm.Get(id)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if si.UserID != userID {
		http.Error(w, "ownership required", http.StatusForbidden)
		return
	}
	if !si.State.IsActive() {
		http.Error(w, "session not active", http.StatusConflict)
		return
	}

	// Delegate to bridge.
	result, err := g.bridge.SwitchWorkDir(r.Context(), id, body.WorkDir)
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) || strings.Contains(err.Error(), "not a directory") {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, map[string]string{
		"old_session_id": result.OldSessionID,
		"new_session_id": result.NewSessionID,
		"work_dir":       result.WorkDir,
	})
}

func (g *GatewayAPI) GetHistory(w http.ResponseWriter, r *http.Request) {
	userID, _, err := g.auth.AuthenticateRequest(r)
	if err != nil {
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
	if si.UserID != userID {
		http.Error(w, "ownership required", http.StatusForbidden)
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}

	beforeSeq := int64(0)
	if bs := r.URL.Query().Get("before_seq"); bs != "" {
		if v, err := strconv.ParseInt(bs, 10, 64); err == nil && v > 0 {
			beforeSeq = v
		}
	}

	if g.convStore == nil {
		respondJSON(w, map[string]any{"records": []any{}, "has_more": false})
		return
	}

	fetchLimit := limit + 1
	var records []*session.ConversationRecord

	if beforeSeq > 0 {
		records, err = g.convStore.GetBySessionBefore(r.Context(), id, beforeSeq, fetchLimit)
	} else {
		records, err = g.convStore.GetBySession(r.Context(), id, fetchLimit, 0)
	}

	if err != nil {
		if errors.Is(err, session.ErrConvNotFound) {
			respondJSON(w, map[string]any{"records": []any{}, "has_more": false})
			return
		}
		g.log.Error("gateway: get history failed", "session_id", id, "err", err)
		http.Error(w, "failed to get history", http.StatusInternalServerError)
		return
	}

	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}

	respondJSON(w, map[string]any{"records": records, "has_more": hasMore})
}
