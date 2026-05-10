package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// CronSchedulerProvider abstracts the cron scheduler for the admin API.
type CronSchedulerProvider interface {
	CreateJob(ctx context.Context, job any) error
	UpdateJob(ctx context.Context, id string, updates map[string]any) error
	DeleteJob(ctx context.Context, id string) error
	GetJob(ctx context.Context, id string) (any, error)
	ListJobs(ctx context.Context) (any, error)
	TriggerJob(ctx context.Context, id string) error
	RunHistory(ctx context.Context, id string) (any, error)
}

// HandleCronList returns all cron jobs.
func (a *AdminAPI) HandleCronList(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.cron == nil {
		respondJSON(w, []any{})
		return
	}
	result, err := a.cron.ListJobs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, result)
}

// HandleCronGet returns a single cron job.
func (a *AdminAPI) HandleCronGet(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	result, err := a.cron.GetJob(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	respondJSON(w, result)
}

// HandleCronCreate creates a new cron job.
func (a *AdminAPI) HandleCronCreate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := a.cron.CreateJob(r.Context(), body); err != nil {
		http.Error(w, fmt.Sprintf("create job: %s", err), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// HandleCronUpdate updates an existing cron job.
func (a *AdminAPI) HandleCronUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := a.cron.UpdateJob(r.Context(), id, body); err != nil {
		http.Error(w, fmt.Sprintf("update job: %s", err), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleCronDelete deletes a cron job.
func (a *AdminAPI) HandleCronDelete(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := a.cron.DeleteJob(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleCronTrigger manually triggers a cron job run.
func (a *AdminAPI) HandleCronTrigger(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := a.cron.TriggerJob(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// HandleCronRunHistory returns the turn history for a cron job's latest run.
func (a *AdminAPI) HandleCronRunHistory(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	result, err := a.cron.RunHistory(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, result)
}
