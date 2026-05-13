package admin

import (
	"encoding/json"
	"net/http"
)

// BotListerProvider abstracts bot registry access for the admin API.
type BotListerProvider interface {
	ListBots() []BotEntry
	GetBot(name string) (*BotEntry, bool)
}

// BotEntry is a read-only view of a registered bot.
type BotEntry struct {
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	BotID       string `json:"bot_id"`
	Status      string `json:"status"`
	ConnectedAt string `json:"connected_at,omitempty"`
	Soul        string `json:"soul,omitempty"`
	WorkerType  string `json:"worker_type,omitempty"`
}

// HandleListBots returns all registered bots.
func (a *AdminAPI) HandleListBots(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}

	if a.botLister == nil {
		writeJSON(w, http.StatusOK, []BotEntry{})
		return
	}

	entries := a.botLister.ListBots()
	if entries == nil {
		entries = []BotEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// HandleGetBot returns details for a single bot by name.
func (a *AdminAPI) HandleGetBot(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}

	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}

	if a.botLister == nil {
		http.Error(w, "bot registry not available", http.StatusNotFound)
		return
	}

	entry, ok := a.botLister.GetBot(name)
	if !ok {
		http.Error(w, "bot not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "encode failed", http.StatusInternalServerError)
	}
}
