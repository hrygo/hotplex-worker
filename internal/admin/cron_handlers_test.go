package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- Mock cron scheduler ---

type mockCronScheduler struct {
	createJobFn  func(ctx context.Context, job any) error
	updateJobFn  func(ctx context.Context, id string, updates map[string]any) error
	deleteJobFn  func(ctx context.Context, id string) error
	getJobFn     func(ctx context.Context, id string) (any, error)
	listJobsFn   func(ctx context.Context) (any, error)
	triggerJobFn func(ctx context.Context, id string) error
	runHistoryFn func(ctx context.Context, id string) (any, error)
}

func (m *mockCronScheduler) CreateJob(ctx context.Context, job any) error {
	if m.createJobFn != nil {
		return m.createJobFn(ctx, job)
	}
	return nil
}

func (m *mockCronScheduler) UpdateJob(ctx context.Context, id string, updates map[string]any) error {
	if m.updateJobFn != nil {
		return m.updateJobFn(ctx, id, updates)
	}
	return nil
}

func (m *mockCronScheduler) DeleteJob(ctx context.Context, id string) error {
	if m.deleteJobFn != nil {
		return m.deleteJobFn(ctx, id)
	}
	return nil
}

func (m *mockCronScheduler) GetJob(ctx context.Context, id string) (any, error) {
	if m.getJobFn != nil {
		return m.getJobFn(ctx, id)
	}
	return nil, nil
}

func (m *mockCronScheduler) ListJobs(ctx context.Context) (any, error) {
	if m.listJobsFn != nil {
		return m.listJobsFn(ctx)
	}
	return []any{}, nil
}

func (m *mockCronScheduler) TriggerJob(ctx context.Context, id string) error {
	if m.triggerJobFn != nil {
		return m.triggerJobFn(ctx, id)
	}
	return nil
}

func (m *mockCronScheduler) RunHistory(ctx context.Context, id string) (any, error) {
	if m.runHistoryFn != nil {
		return m.runHistoryFn(ctx, id)
	}
	return []any{}, nil
}

// --- Helpers ---

func newCronTestAPI(overrides ...func(*Deps)) *AdminAPI {
	deps := Deps{
		Log:          slog.Default(),
		Config:       &mockConfig{},
		SessionMgr:   &mockSessionManager{},
		Hub:          &mockHub{conns: 2},
		Bridge:       &mockBridge{},
		Cron:         &mockCronScheduler{},
		Version:      func() string { return "test-v1" },
		NewSessionID: func() string { return "sid-123" },
	}
	for _, o := range overrides {
		o(&deps)
	}
	return New(deps)
}

func cronAPIWithMock(fn func(*mockCronScheduler)) *AdminAPI {
	mc := &mockCronScheduler{}
	fn(mc)
	return newCronTestAPI(func(d *Deps) {
		d.Cron = mc
	})
}

// --- HandleCronList tests ---

func TestHandleCronList_Success(t *testing.T) {
	t.Parallel()
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.listJobsFn = func(_ context.Context) (any, error) {
			return []map[string]any{{"id": "j1", "name": "daily"}}, nil
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleCronList(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var result []map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Len(t, result, 1)
	require.Equal(t, "j1", result[0]["id"])
}

func TestHandleCronList_Forbidden(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs", nil)
	r = withScope(r, ScopeSessionRead)

	api.HandleCronList(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleCronList_SchedulerError(t *testing.T) {
	t.Parallel()
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.listJobsFn = func(_ context.Context) (any, error) {
			return nil, errors.New("db unavailable")
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleCronList(w, r)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleCronList_NilScheduler(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI(func(d *Deps) {
		d.Cron = nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleCronList(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var result []any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Equal(t, []any{}, result)
}

// --- HandleCronGet tests ---

func TestHandleCronGet_Success(t *testing.T) {
	t.Parallel()
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.getJobFn = func(_ context.Context, id string) (any, error) {
			require.Equal(t, "job-42", id)
			return map[string]any{"id": "job-42", "name": "hourly"}, nil
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs/job-42", nil)
	r.SetPathValue("id", "job-42")
	r = withScope(r, ScopeAdminRead)

	api.HandleCronGet(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var result map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Equal(t, "job-42", result["id"])
}

func TestHandleCronGet_Forbidden(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs/1", nil)
	r = withScope(r, ScopeSessionRead)

	api.HandleCronGet(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleCronGet_NotFound(t *testing.T) {
	t.Parallel()
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.getJobFn = func(_ context.Context, _ string) (any, error) {
			return nil, errors.New("not found")
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs/missing", nil)
	r.SetPathValue("id", "missing")
	r = withScope(r, ScopeAdminRead)

	api.HandleCronGet(w, r)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleCronGet_NilScheduler(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI(func(d *Deps) {
		d.Cron = nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs/1", nil)
	r.SetPathValue("id", "1")
	r = withScope(r, ScopeAdminRead)

	api.HandleCronGet(w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- HandleCronCreate tests ---

func TestHandleCronCreate_Success(t *testing.T) {
	t.Parallel()
	var received map[string]any
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.createJobFn = func(_ context.Context, job any) error {
			received = job.(map[string]any)
			return nil
		}
	})
	w := httptest.NewRecorder()
	body := `{"name":"daily-health","schedule":"cron:0 9 * * 1-5"}`
	r := httptest.NewRequest(http.MethodPost, "/cron/jobs", strings.NewReader(body))
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronCreate(w, r)

	require.Equal(t, http.StatusCreated, w.Code)
	require.Equal(t, "daily-health", received["name"])
}

func TestHandleCronCreate_Forbidden(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/cron/jobs", strings.NewReader(`{}`))
	r = withScope(r, ScopeAdminRead) // read-only, needs write

	api.HandleCronCreate(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleCronCreate_InvalidJSON(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/cron/jobs", strings.NewReader("not-json"))
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronCreate(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid JSON")
}

func TestHandleCronCreate_SchedulerError(t *testing.T) {
	t.Parallel()
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.createJobFn = func(_ context.Context, _ any) error {
			return errors.New("duplicate name")
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/cron/jobs", strings.NewReader(`{"name":"dup"}`))
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronCreate(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "create job")
}

func TestHandleCronCreate_NilScheduler(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI(func(d *Deps) {
		d.Cron = nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/cron/jobs", strings.NewReader(`{}`))
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronCreate(w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- HandleCronUpdate tests ---

func TestHandleCronUpdate_Success(t *testing.T) {
	t.Parallel()
	var (
		receivedID   string
		receivedBody map[string]any
	)
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.updateJobFn = func(_ context.Context, id string, updates map[string]any) error {
			receivedID = id
			receivedBody = updates
			return nil
		}
	})
	w := httptest.NewRecorder()
	body := `{"enabled":false}`
	r := httptest.NewRequest(http.MethodPut, "/cron/jobs/j1", strings.NewReader(body))
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronUpdate(w, r)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, "j1", receivedID)
	require.Equal(t, false, receivedBody["enabled"])
}

func TestHandleCronUpdate_Forbidden(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/cron/jobs/j1", strings.NewReader(`{}`))
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminRead)

	api.HandleCronUpdate(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleCronUpdate_InvalidJSON(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/cron/jobs/j1", strings.NewReader("bad"))
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronUpdate(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid JSON")
}

func TestHandleCronUpdate_SchedulerError(t *testing.T) {
	t.Parallel()
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.updateJobFn = func(_ context.Context, _ string, _ map[string]any) error {
			return errors.New("job not found")
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/cron/jobs/j1", strings.NewReader(`{"name":"x"}`))
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronUpdate(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "update job")
}

func TestHandleCronUpdate_NilScheduler(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI(func(d *Deps) {
		d.Cron = nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/cron/jobs/j1", strings.NewReader(`{}`))
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronUpdate(w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- HandleCronDelete tests ---

func TestHandleCronDelete_Success(t *testing.T) {
	t.Parallel()
	var deletedID string
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.deleteJobFn = func(_ context.Context, id string) error {
			deletedID = id
			return nil
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/cron/jobs/j1", nil)
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronDelete(w, r)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, "j1", deletedID)
}

func TestHandleCronDelete_Forbidden(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/cron/jobs/j1", nil)
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminRead)

	api.HandleCronDelete(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleCronDelete_NotFound(t *testing.T) {
	t.Parallel()
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.deleteJobFn = func(_ context.Context, _ string) error {
			return errors.New("not found")
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/cron/jobs/missing", nil)
	r.SetPathValue("id", "missing")
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronDelete(w, r)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleCronDelete_NilScheduler(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI(func(d *Deps) {
		d.Cron = nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/cron/jobs/j1", nil)
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronDelete(w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- HandleCronTrigger tests ---

func TestHandleCronTrigger_Success(t *testing.T) {
	t.Parallel()
	var triggeredID string
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.triggerJobFn = func(_ context.Context, id string) error {
			triggeredID = id
			return nil
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/cron/jobs/j1/trigger", nil)
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronTrigger(w, r)

	require.Equal(t, http.StatusAccepted, w.Code)
	require.Equal(t, "j1", triggeredID)
}

func TestHandleCronTrigger_Forbidden(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/cron/jobs/j1/trigger", nil)
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminRead)

	api.HandleCronTrigger(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleCronTrigger_NotFound(t *testing.T) {
	t.Parallel()
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.triggerJobFn = func(_ context.Context, _ string) error {
			return errors.New("job not found")
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/cron/jobs/missing/trigger", nil)
	r.SetPathValue("id", "missing")
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronTrigger(w, r)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleCronTrigger_NilScheduler(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI(func(d *Deps) {
		d.Cron = nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/cron/jobs/j1/trigger", nil)
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminWrite)

	api.HandleCronTrigger(w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- HandleCronRunHistory tests ---

func TestHandleCronRunHistory_Success(t *testing.T) {
	t.Parallel()
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.runHistoryFn = func(_ context.Context, id string) (any, error) {
			require.Equal(t, "j1", id)
			return []map[string]any{{"run_id": "r1", "status": "completed"}}, nil
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs/j1/runs", nil)
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminRead)

	api.HandleCronRunHistory(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var result []map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	require.Len(t, result, 1)
	require.Equal(t, "r1", result[0]["run_id"])
}

func TestHandleCronRunHistory_Forbidden(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs/j1/runs", nil)
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeSessionRead)

	api.HandleCronRunHistory(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleCronRunHistory_SchedulerError(t *testing.T) {
	t.Parallel()
	api := cronAPIWithMock(func(mc *mockCronScheduler) {
		mc.runHistoryFn = func(_ context.Context, _ string) (any, error) {
			return nil, errors.New("db error")
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs/j1/runs", nil)
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminRead)

	api.HandleCronRunHistory(w, r)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleCronRunHistory_NilScheduler(t *testing.T) {
	t.Parallel()
	api := newCronTestAPI(func(d *Deps) {
		d.Cron = nil
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/cron/jobs/j1/runs", nil)
	r.SetPathValue("id", "j1")
	r = withScope(r, ScopeAdminRead)

	api.HandleCronRunHistory(w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}
