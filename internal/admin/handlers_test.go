package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// --- Mock providers ---

type mockSessionManager struct {
	statsFn         func() (total, max, unique int)
	listFn          func(ctx context.Context, userID, platform string, limit, offset int) ([]any, error)
	getFn           func(id string) (any, error)
	deleteFn        func(ctx context.Context, id string) error
	transitionFn    func(ctx context.Context, id string, to events.SessionState) error
	workerHealthFn  func() []worker.WorkerHealth
	debugSnapshotFn func(id string) (DebugSessionSnapshot, bool)
}

func (m *mockSessionManager) Stats() (total, max, unique int) {
	if m.statsFn != nil {
		return m.statsFn()
	}
	return 5, 10, 3
}
func (m *mockSessionManager) List(ctx context.Context, userID, platform string, limit, offset int) ([]any, error) {
	if m.listFn != nil {
		return m.listFn(ctx, userID, platform, limit, offset)
	}
	return []any{}, nil
}
func (m *mockSessionManager) Get(id string) (any, error) {
	if m.getFn != nil {
		return m.getFn(id)
	}
	return nil, errors.New("not found")
}
func (m *mockSessionManager) Delete(ctx context.Context, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}
func (m *mockSessionManager) DeletePhysical(ctx context.Context, id string) error { return nil }
func (m *mockSessionManager) WorkerHealthStatuses() []worker.WorkerHealth {
	if m.workerHealthFn != nil {
		return m.workerHealthFn()
	}
	return nil
}
func (m *mockSessionManager) DebugSnapshot(id string) (DebugSessionSnapshot, bool) {
	if m.debugSnapshotFn != nil {
		return m.debugSnapshotFn(id)
	}
	return DebugSessionSnapshot{}, false
}
func (m *mockSessionManager) Transition(ctx context.Context, id string, to events.SessionState) error {
	if m.transitionFn != nil {
		return m.transitionFn(ctx, id, to)
	}
	return nil
}

type mockHub struct{ conns int }

func (m *mockHub) ConnectionsOpen() int     { return m.conns }
func (m *mockHub) NextSeqPeek(string) int64 { return 42 }

type mockBridge struct{ err error }

func (m *mockBridge) StartSession(context.Context, string, string, string, worker.WorkerType, []string, string, string, map[string]string, string) error {
	return m.err
}

type mockConfig struct {
	cfg *config.Config
}

func (m *mockConfig) Get() *config.Config {
	if m.cfg != nil {
		return m.cfg
	}
	return &config.Config{
		Admin: config.AdminConfig{
			Tokens:        []string{"test-token"},
			DefaultScopes: []string{ScopeSessionRead, ScopeSessionWrite, ScopeSessionKill, ScopeStatsRead, ScopeHealthRead, ScopeAdminRead, ScopeConfigRead},
		},
	}
}

// --- Helpers ---

func newTestAPI(overrides ...func(*Deps)) *AdminAPI {
	deps := Deps{
		Log:          slog.Default(),
		Config:       &mockConfig{},
		SessionMgr:   &mockSessionManager{},
		Hub:          &mockHub{conns: 2},
		Bridge:       &mockBridge{},
		Version:      func() string { return "test-v1" },
		NewSessionID: func() string { return "sid-123" },
	}
	for _, o := range overrides {
		o(&deps)
	}
	return New(deps)
}

func withScope(r *http.Request, scopes ...string) *http.Request {
	ctx := context.WithValue(r.Context(), scopeContextKey{}, scopes)
	return r.WithContext(ctx)
}

// --- Handler tests ---

func TestCreateSession_Success(t *testing.T) {
	api := newTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/sessions?user_id=u1", nil)
	r = withScope(r, ScopeSessionWrite)

	api.CreateSession(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "sid-123", resp["session_id"])
}

func TestCreateSession_Forbidden(t *testing.T) {
	api := newTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/sessions", nil)
	r = withScope(r, ScopeSessionRead) // wrong scope

	api.CreateSession(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestCreateSession_BridgeError(t *testing.T) {
	api := newTestAPI(func(d *Deps) {
		d.Bridge = &mockBridge{err: errors.New("boom")}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/sessions", nil)
	r = withScope(r, ScopeSessionWrite)

	api.CreateSession(w, r)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestListSessions_Success(t *testing.T) {
	api := newTestAPI(func(d *Deps) {
		d.SessionMgr = &mockSessionManager{
			listFn: func(_ context.Context, _, _ string, limit, offset int) ([]any, error) {
				require.Equal(t, 50, limit)
				require.Equal(t, 10, offset)
				return []any{map[string]any{"id": "s1"}}, nil
			},
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/sessions?limit=50&offset=10", nil)
	r = withScope(r, ScopeSessionRead)

	api.ListSessions(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Contains(t, resp, "sessions")
}

func TestGetSession_NotFound(t *testing.T) {
	api := newTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/sessions/missing", nil)
	r.SetPathValue("id", "missing")
	r = withScope(r, ScopeSessionRead)

	api.GetSession(w, r)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteSession_Success(t *testing.T) {
	var deletedID string
	api := newTestAPI(func(d *Deps) {
		d.SessionMgr = &mockSessionManager{
			deleteFn: func(_ context.Context, id string) error {
				deletedID = id
				return nil
			},
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/sessions/s1", nil)
	r.SetPathValue("id", "s1")
	r = withScope(r, ScopeSessionKill)

	api.DeleteSession(w, r)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, "s1", deletedID)
}

func TestTerminateSession_Success(t *testing.T) {
	var terminatedID string
	api := newTestAPI(func(d *Deps) {
		d.SessionMgr = &mockSessionManager{
			transitionFn: func(_ context.Context, id string, to events.SessionState) error {
				terminatedID = id
				require.Equal(t, events.StateTerminated, to)
				return nil
			},
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/sessions/s1/terminate", nil)
	r.SetPathValue("id", "s1")
	r = withScope(r, ScopeSessionWrite)

	api.TerminateSession(w, r)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, "s1", terminatedID)
}

func TestPoolStats_Success(t *testing.T) {
	api := newTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/pool/stats", nil)
	r = withScope(r, ScopeStatsRead)

	api.PoolStats(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]int
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, 5, resp["total"])
}

func TestHandleHealth(t *testing.T) {
	api := newTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/health", nil)

	api.HandleHealth(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "healthy", resp["status"])
}

func TestHandleHealth_Degraded(t *testing.T) {
	api := newTestAPI(func(d *Deps) {
		d.SessionMgr = &mockSessionManager{
			listFn: func(_ context.Context, _ string, _ string, _ int, _ int) ([]any, error) {
				return nil, errors.New("db down")
			},
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/health", nil)

	api.HandleHealth(w, r)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "degraded", resp["status"])
}

func TestHandleWorkerHealth_AllHealthy(t *testing.T) {
	api := newTestAPI(func(d *Deps) {
		d.SessionMgr = &mockSessionManager{
			workerHealthFn: func() []worker.WorkerHealth {
				return []worker.WorkerHealth{{Healthy: true}, {Healthy: true}}
			},
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/workers/health", nil)
	r = withScope(r, ScopeHealthRead)

	api.HandleWorkerHealth(w, r)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestHandleWorkerHealth_Degraded(t *testing.T) {
	api := newTestAPI(func(d *Deps) {
		d.SessionMgr = &mockSessionManager{
			workerHealthFn: func() []worker.WorkerHealth {
				return []worker.WorkerHealth{{Healthy: true}, {Healthy: false}}
			},
		}
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/workers/health", nil)
	r = withScope(r, ScopeHealthRead)

	api.HandleWorkerHealth(w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleConfigValidate(t *testing.T) {
	api := newTestAPI()
	w := httptest.NewRecorder()
	body := `{"pool": {"max_size": 5000}}`
	r := httptest.NewRequest(http.MethodPost, "/config/validate", strings.NewReader(body))
	r = withScope(r, ScopeConfigRead)

	api.HandleConfigValidate(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, true, resp["valid"])
}

func TestHandleConfigValidate_Invalid(t *testing.T) {
	api := newTestAPI()
	w := httptest.NewRecorder()
	body := `{"pool": {"max_size": -1}}`
	r := httptest.NewRequest(http.MethodPost, "/config/validate", strings.NewReader(body))
	r = withScope(r, ScopeConfigRead)

	api.HandleConfigValidate(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleConfigValidate_BadJSON(t *testing.T) {
	api := newTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/config/validate", strings.NewReader("not json"))
	r = withScope(r, ScopeConfigRead)

	api.HandleConfigValidate(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleLogs(t *testing.T) {
	LogRing.Add("info", "test message", "sess1")
	defer func() { LogRing = newLogRing(100) }()

	api := newTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/logs", nil)
	r = withScope(r, ScopeAdminRead)

	api.HandleLogs(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Contains(t, resp, "logs")
	require.Contains(t, resp, "total")
}

// --- Middleware integration tests ---

func TestMiddleware_MissingToken(t *testing.T) {
	api := newTestAPI()
	handler := api.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("should not reach handler")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(w, r)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_ValidToken(t *testing.T) {
	api := newTestAPI()
	var scopes []string
	handler := api.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		scopes = getScopes(r)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer test-token")

	handler.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, scopes, ScopeSessionRead)
}

func TestMiddleware_InvalidToken(t *testing.T) {
	api := newTestAPI()
	handler := api.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("should not reach handler")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer wrong-token")

	handler.ServeHTTP(w, r)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_RateLimit(t *testing.T) {
	api := newTestAPI()
	api.SetRateLimiter(NewRateLimiter(1, 1)) // 1 burst

	handler := api.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	// First request should pass
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodGet, "/", nil)
	r1.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(w1, r1)
	require.Equal(t, http.StatusOK, w1.Code)

	// Second request should be rate limited
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(w2, r2)
	require.Equal(t, http.StatusTooManyRequests, w2.Code)
}

func TestMiddleware_IPWhitelist(t *testing.T) {
	api := newTestAPI()
	api.SetAllowedCIDRs([]string{"10.0.0.0/8"})

	handler := api.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "8.8.8.8:1234"
	r.Header.Set("Authorization", "Bearer test-token")

	handler.ServeHTTP(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestMiddleware_CORS(t *testing.T) {
	api := newTestAPI()
	handler := api.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/", nil)

	handler.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestMiddleware_PanicRecovery(t *testing.T) {
	api := newTestAPI()
	handler := api.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("test panic")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer test-token")

	handler.ServeHTTP(w, r)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestMiddleware_TokenScopes(t *testing.T) {
	api := newTestAPI(func(d *Deps) {
		d.Config = &mockConfig{cfg: &config.Config{
			Admin: config.AdminConfig{
				TokenScopes: map[string][]string{
					"readonly-token": {ScopeSessionRead, ScopeStatsRead},
				},
			},
		}}
	})

	var scopes []string
	handler := api.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		scopes = getScopes(r)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer readonly-token")

	handler.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{ScopeSessionRead, ScopeStatsRead}, scopes)
}

func TestHandleConfigRollback_NoWatcher(t *testing.T) {
	api := newTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/config/rollback", strings.NewReader(`{"version": 1}`))
	r = withScope(r, ScopeConfigRead)

	api.HandleConfigRollback(w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleConfigValidate_EmptyBody(t *testing.T) {
	api := newTestAPI()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/config/validate", nil)
	r = withScope(r, ScopeConfigRead)

	api.HandleConfigValidate(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleConfigValidate_LargeBody(t *testing.T) {
	api := newTestAPI()
	w := httptest.NewRecorder()
	// Body larger than 1MB should fail
	largeBody := strings.NewReader(strings.Repeat("x", 2<<20))
	r := httptest.NewRequest(http.MethodPost, "/config/validate", io.LimitReader(largeBody, 2<<20))
	r = withScope(r, ScopeConfigRead)

	api.HandleConfigValidate(w, r)

	// Should get an error (bad request or JSON decode failure)
	require.True(t, w.Code >= 400)
}
