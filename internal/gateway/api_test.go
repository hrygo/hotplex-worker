package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── Mock SessionManager for API tests ─────────────────────────────────────────

type mockAPISM struct {
	mock.Mock
}

func (m *mockAPISM) CreateWithBot(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, platform string, platformKey map[string]string, workDir string, title string) (*session.SessionInfo, error) {
	args := m.Called(ctx, id, userID, botID, wt, allowedTools, platform, platformKey, workDir, title)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*session.SessionInfo), args.Error(1)
}

func (m *mockAPISM) AttachWorker(id string, w worker.Worker) error {
	return m.Called(id, w).Error(0)
}

func (m *mockAPISM) DetachWorker(id string) { m.Called(id) }

func (m *mockAPISM) DetachWorkerIf(id string, expected worker.Worker) bool {
	return m.Called(id, expected).Bool(0)
}

func (m *mockAPISM) Transition(ctx context.Context, id string, to events.SessionState) error {
	return m.Called(ctx, id, to).Error(0)
}

func (m *mockAPISM) Get(id string) (*session.SessionInfo, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*session.SessionInfo), args.Error(1)
}

func (m *mockAPISM) GetWorker(id string) worker.Worker {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(worker.Worker)
}

func (m *mockAPISM) Delete(ctx context.Context, id string) error {
	return m.Called(ctx, id).Error(0)
}

func (m *mockAPISM) DeletePhysical(ctx context.Context, id string) error {
	return m.Called(ctx, id).Error(0)
}

func (m *mockAPISM) List(ctx context.Context, userID, platform string, limit, offset int) ([]*session.SessionInfo, error) {
	args := m.Called(ctx, userID, platform, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*session.SessionInfo), args.Error(1)
}

func (m *mockAPISM) UpdateWorkerSessionID(ctx context.Context, id, workerSessionID string) error {
	return m.Called(ctx, id, workerSessionID).Error(0)
}

func (m *mockAPISM) ResetExpiry(ctx context.Context, id string) error {
	return m.Called(ctx, id).Error(0)
}

func (m *mockAPISM) UpdateWorkDir(ctx context.Context, id, workDir string) error {
	return m.Called(ctx, id, workDir).Error(0)
}

// ─── Mock SessionStarter for API tests ─────────────────────────────────────────

type mockAPIBridge struct {
	mock.Mock
}

func (m *mockAPIBridge) StartSession(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, workDir string, platform string, platformKey map[string]string, title string) error {
	return m.Called(ctx, id, userID, botID, wt, allowedTools, workDir, platform, platformKey, title).Error(0)
}

func (m *mockAPIBridge) ResumeSession(ctx context.Context, id string, workDir string) error {
	return m.Called(ctx, id, workDir).Error(0)
}

func (m *mockAPIBridge) SwitchWorkDir(ctx context.Context, oldSessionID, newWorkDir string) (*SwitchWorkDirResult, error) {
	args := m.Called(ctx, oldSessionID, newWorkDir)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SwitchWorkDirResult), args.Error(1)
}

// ─── Mock ConversationStoreReader for API tests ────────────────────────────────

type mockAPIConvStore struct {
	mock.Mock
}

func (m *mockAPIConvStore) GetBySession(ctx context.Context, sessionID string, limit, offset int) ([]*session.ConversationRecord, error) {
	args := m.Called(ctx, sessionID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*session.ConversationRecord), args.Error(1)
}

func (m *mockAPIConvStore) GetBySessionBefore(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]*session.ConversationRecord, error) {
	args := m.Called(ctx, sessionID, beforeSeq, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*session.ConversationRecord), args.Error(1)
}

// ─── Test helpers ───────────────────────────────────────────────────────────────

func newTestAuth(t *testing.T) *security.Authenticator {
	t.Helper()
	return security.NewAuthenticator(&config.SecurityConfig{}, nil)
}

func newTestAPI(t *testing.T, sm *mockAPISM, bridge *mockAPIBridge) *GatewayAPI {
	t.Helper()
	return NewGatewayAPI(slog.Default(), newTestAuth(t), sm, bridge, config.NewConfigStore(&config.Config{}, nil), nil, nil)
}

func newTestAPIWithConv(t *testing.T, sm *mockAPISM, bridge *mockAPIBridge, convStore *mockAPIConvStore) *GatewayAPI {
	t.Helper()
	return NewGatewayAPI(slog.Default(), newTestAuth(t), sm, bridge, config.NewConfigStore(&config.Config{}, nil), convStore, nil)
}

func authedReq(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Header.Set("X-API-Key", "test-key")
	return r
}

// setupMux creates a ServeMux with API routes for tests that need r.PathValue.
func setupMux(api *GatewayAPI) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", api.ListSessions)
	mux.HandleFunc("POST /api/sessions", api.CreateSession)
	mux.HandleFunc("GET /api/sessions/{id}", api.GetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", api.DeleteSession)
	mux.HandleFunc("POST /api/sessions/{id}/cd", api.SwitchWorkDir)
	mux.HandleFunc("GET /api/sessions/{id}/history", api.GetHistory)
	return mux
}

// ─── CreateSession tests ────────────────────────────────────────────────────────

func TestCreateSession_TitlePreferred(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	// Get returns not found → no idempotency path
	sm.On("Get", mock.Anything).Return(nil, session.ErrSessionNotFound)
	bridge.On("StartSession", mock.Anything, mock.Anything, "anonymous", "", worker.TypeClaudeCode,
		([]string)(nil), "", "webchat", map[string]string(nil), "my-title").Return(nil)

	w := httptest.NewRecorder()
	api.CreateSession(w, authedReq("POST", "/api/sessions?title=my-title", nil))

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotEmpty(t, resp["session_id"])
	bridge.AssertExpectations(t)
}

func TestCreateSession_SessionIDOnlyRejected(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	// session_id alone without title is rejected
	w := httptest.NewRecorder()
	api.CreateSession(w, authedReq("POST", "/api/sessions?session_id=fallback-id", nil))

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "title is required")
}

func TestCreateSession_MissingTitleAndSessionID(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	w := httptest.NewRecorder()
	api.CreateSession(w, authedReq("POST", "/api/sessions", nil))

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "title is required")
}

func TestCreateSession_IdempotentActiveSession(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	active := &session.SessionInfo{ID: "existing-id", State: events.StateRunning}
	sm.On("Get", mock.Anything).Return(active, nil)
	// bridge.StartSession should NOT be called

	w := httptest.NewRecorder()
	api.CreateSession(w, authedReq("POST", "/api/sessions?title=test", nil))

	require.Equal(t, http.StatusOK, w.Code)
	bridge.AssertNotCalled(t, "StartSession", mock.Anything)
}

func TestCreateSession_DeletedSessionRecreated(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	deleted := &session.SessionInfo{ID: "deleted-id", State: events.StateDeleted}
	sm.On("Get", mock.Anything).Return(deleted, nil)
	sm.On("DeletePhysical", mock.Anything, mock.Anything).Return(nil)
	bridge.On("StartSession", mock.Anything, mock.Anything, "anonymous", "",
		worker.TypeClaudeCode, ([]string)(nil), "", "webchat", map[string]string(nil), "test").Return(nil)

	w := httptest.NewRecorder()
	api.CreateSession(w, authedReq("POST", "/api/sessions?title=test", nil))

	require.Equal(t, http.StatusOK, w.Code)
	sm.AssertCalled(t, "DeletePhysical", mock.Anything, mock.Anything)
	bridge.AssertExpectations(t)
}

func TestCreateSession_BridgeError(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	sm.On("Get", mock.Anything).Return(nil, session.ErrSessionNotFound)
	bridge.On("StartSession", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(errTestBridge)

	w := httptest.NewRecorder()
	api.CreateSession(w, authedReq("POST", "/api/sessions?title=fail", nil))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "failed to create session")
}

var errTestBridge = fmt.Errorf("test bridge error")

func TestCreateSession_WithWorkDir(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	sm.On("Get", mock.Anything).Return(nil, session.ErrSessionNotFound)
	bridge.On("StartSession", mock.Anything, mock.Anything, "anonymous", "",
		worker.TypeClaudeCode, ([]string)(nil), mock.Anything, "webchat", map[string]string(nil), "with-workdir").
		Return(nil)

	w := httptest.NewRecorder()
	api.CreateSession(w, authedReq("POST", "/api/sessions?title=with-workdir&work_dir=/tmp", nil))

	require.Equal(t, http.StatusOK, w.Code)
	bridge.AssertExpectations(t)
}

// ─── DeleteSession tests ────────────────────────────────────────────────────────

func TestDeleteSession_GracefulTermination(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	sm.On("Transition", mock.Anything, "sess-1", events.StateTerminated).Return(nil)
	sm.On("DeletePhysical", mock.Anything, "sess-1").Return(nil)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authedReq("DELETE", "/api/sessions/sess-1", nil))

	require.Equal(t, http.StatusNoContent, w.Code)
	sm.AssertCalled(t, "Transition", mock.Anything, "sess-1", events.StateTerminated)
	sm.AssertCalled(t, "DeletePhysical", mock.Anything, "sess-1")
}

func TestDeleteSession_TransitionFailsStillDeletes(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	sm.On("Transition", mock.Anything, "sess-2", events.StateTerminated).Return(errTestBridge)
	sm.On("DeletePhysical", mock.Anything, "sess-2").Return(nil)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authedReq("DELETE", "/api/sessions/sess-2", nil))

	// Transition failure is tolerated; delete still proceeds
	require.Equal(t, http.StatusNoContent, w.Code)
	sm.AssertCalled(t, "DeletePhysical", mock.Anything, "sess-2")
}

func TestDeleteSession_MissingID(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authedReq("DELETE", "/api/sessions/", nil))

	// No {id} match → 404 from mux (no path value)
	require.Equal(t, http.StatusNotFound, w.Code)
}

// ─── ListSessions tests ─────────────────────────────────────────────────────────

func TestListSessions(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	now := time.Now()
	sessions := []*session.SessionInfo{
		{ID: "s1", State: events.StateRunning, CreatedAt: now},
		{ID: "s2", State: events.StateIdle, CreatedAt: now},
	}
	sm.On("List", mock.Anything, "anonymous", "webchat", 100, 0).Return(sessions, nil)

	w := httptest.NewRecorder()
	api.ListSessions(w, authedReq("GET", "/api/sessions", nil))

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	list := resp["sessions"].([]any)
	require.Len(t, list, 2)
}

func TestListSessions_Unauthorized(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	w := httptest.NewRecorder()
	api.ListSessions(w, httptest.NewRequest("GET", "/api/sessions", nil))
	// No X-API-Key header → unauthorized

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

// ─── GetSession tests ───────────────────────────────────────────────────────────

func TestGetSession(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	si := &session.SessionInfo{ID: "sess-x", State: events.StateRunning, Title: "my session"}
	sm.On("Get", "sess-x").Return(si, nil)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authedReq("GET", "/api/sessions/sess-x", nil))

	require.Equal(t, http.StatusOK, w.Code)
	var got session.SessionInfo
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	require.Equal(t, "sess-x", got.ID)
	require.Equal(t, "my session", got.Title)
}

func TestGetSession_NotFound(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	sm.On("Get", "no-such").Return(nil, session.ErrSessionNotFound)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authedReq("GET", "/api/sessions/no-such", nil))

	require.Equal(t, http.StatusNotFound, w.Code)
}

// ─── SwitchWorkDir tests ────────────────────────────────────────────────────────

func TestSwitchWorkDir_Success(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	si := &session.SessionInfo{ID: "sess-cd", State: events.StateRunning, UserID: "anonymous"}
	sm.On("Get", "sess-cd").Return(si, nil)
	bridge.On("SwitchWorkDir", mock.Anything, "sess-cd", mock.MatchedBy(func(p string) bool {
		return strings.HasSuffix(p, "tmp")
	})).Return(&SwitchWorkDirResult{OldSessionID: "sess-cd", NewSessionID: "sess-new", WorkDir: "/tmp"}, nil)

	mux := setupMux(api)
	body := strings.NewReader(`{"work_dir":"/tmp"}`)
	w := httptest.NewRecorder()
	req := authedReq("POST", "/api/sessions/sess-cd/cd", body)
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "sess-new", resp["new_session_id"])
}

func TestSwitchWorkDir_EmptyBody(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	req := authedReq("POST", "/api/sessions/sess-cd/cd", strings.NewReader(`{}`))
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "work_dir is required")
}

func TestSwitchWorkDir_SessionNotFound(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	sm.On("Get", "no-sess").Return(nil, session.ErrSessionNotFound)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	req := authedReq("POST", "/api/sessions/no-sess/cd", strings.NewReader(`{"work_dir":"/tmp"}`))
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

// ─── GetHistory tests ───────────────────────────────────────────────────────────

func TestGetHistory_Success(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	convStore := new(mockAPIConvStore)
	api := newTestAPIWithConv(t, sm, bridge, convStore)

	sm.On("Get", "sess-1").Return(&session.SessionInfo{ID: "sess-1", UserID: "anonymous"}, nil)
	records := []*session.ConversationRecord{
		{ID: "conv-1", SessionID: "sess-1", Seq: 1, Role: "user", Content: "hello"},
	}
	convStore.On("GetBySession", mock.Anything, "sess-1", 51, 0).Return(records, nil)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	r := authedReq("GET", "/api/sessions/sess-1/history?limit=50", nil)
	mux.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Records []*session.ConversationRecord `json:"records"`
		HasMore bool                          `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Records, 1)
	require.False(t, resp.HasMore)
}

func TestGetHistory_HasMore(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	convStore := new(mockAPIConvStore)
	api := newTestAPIWithConv(t, sm, bridge, convStore)

	sm.On("Get", "sess-1").Return(&session.SessionInfo{ID: "sess-1", UserID: "anonymous"}, nil)
	records := []*session.ConversationRecord{
		{ID: "conv-1", Seq: 1},
		{ID: "conv-2", Seq: 2},
		{ID: "conv-3", Seq: 3},
	}
	convStore.On("GetBySession", mock.Anything, "sess-1", 3, 0).Return(records, nil)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	r := authedReq("GET", "/api/sessions/sess-1/history?limit=2", nil)
	mux.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Records []*session.ConversationRecord `json:"records"`
		HasMore bool                          `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Records, 2)
	require.True(t, resp.HasMore)
}

func TestGetHistory_NoRecords(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	convStore := new(mockAPIConvStore)
	api := newTestAPIWithConv(t, sm, bridge, convStore)

	sm.On("Get", "sess-1").Return(&session.SessionInfo{ID: "sess-1", UserID: "anonymous"}, nil)
	convStore.On("GetBySession", mock.Anything, "sess-1", 51, 0).Return(nil, session.ErrConvNotFound)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	r := authedReq("GET", "/api/sessions/sess-1/history", nil)
	mux.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Records []any `json:"records"`
		HasMore bool  `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Empty(t, resp.Records)
	require.False(t, resp.HasMore)
}

func TestGetHistory_Unauthorized(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	convStore := new(mockAPIConvStore)
	api := newTestAPIWithConv(t, sm, bridge, convStore)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/sessions/sess-1/history", nil)
	mux.ServeHTTP(w, r)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetHistory_OwnershipCheck(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	convStore := new(mockAPIConvStore)
	api := newTestAPIWithConv(t, sm, bridge, convStore)

	sm.On("Get", "sess-1").Return(&session.SessionInfo{ID: "sess-1", UserID: "other-user"}, nil)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	r := authedReq("GET", "/api/sessions/sess-1/history", nil)
	mux.ServeHTTP(w, r)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestGetHistory_WithBeforeSeq(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	convStore := new(mockAPIConvStore)
	api := newTestAPIWithConv(t, sm, bridge, convStore)

	sm.On("Get", "sess-1").Return(&session.SessionInfo{ID: "sess-1", UserID: "anonymous"}, nil)
	records := []*session.ConversationRecord{
		{ID: "conv-1", Seq: 1},
	}
	convStore.On("GetBySessionBefore", mock.Anything, "sess-1", int64(5), 11).Return(records, nil)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	r := authedReq("GET", "/api/sessions/sess-1/history?before_seq=5&limit=10", nil)
	mux.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
}

func TestGetHistory_NilConvStore(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge)

	sm.On("Get", "sess-1").Return(&session.SessionInfo{ID: "sess-1", UserID: "anonymous"}, nil)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	r := authedReq("GET", "/api/sessions/sess-1/history", nil)
	mux.ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Records []any `json:"records"`
		HasMore bool  `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Empty(t, resp.Records)
	require.False(t, resp.HasMore)
}
