package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// ─── Test helpers ───────────────────────────────────────────────────────────────

func newTestAuth(t *testing.T) *security.Authenticator {
	t.Helper()
	return security.NewAuthenticator(&config.SecurityConfig{}, nil)
}

type mockHistoryStore struct {
	mock.Mock
}

func (m *mockHistoryStore) GetBySession(ctx context.Context, sessionID string, limit, offset int) ([]*session.ConversationRecord, error) {
	args := m.Called(ctx, sessionID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*session.ConversationRecord), args.Error(1)
}

func newTestAPI(t *testing.T, sm *mockAPISM, bridge *mockAPIBridge, histStore HistoryStore) *GatewayAPI {
	t.Helper()
	if histStore == nil {
		histStore = &mockHistoryStore{}
	}
	return NewGatewayAPI(newTestAuth(t), sm, bridge, config.NewConfigStore(&config.Config{}, nil), histStore)
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
	mux.HandleFunc("GET /api/sessions/{id}/history", api.GetHistory)
	mux.HandleFunc("DELETE /api/sessions/{id}", api.DeleteSession)
	mux.HandleFunc("POST /api/sessions/{id}/cd", api.SwitchWorkDir)
	return mux
}

// ─── CreateSession tests ────────────────────────────────────────────────────────

func TestCreateSession_TitlePreferred(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

	w := httptest.NewRecorder()
	api.CreateSession(w, authedReq("POST", "/api/sessions", nil))

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "title is required")
}

func TestCreateSession_IdempotentActiveSession(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

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

// ─── DeleteSession tests ────────────────────────────────────────────────────────

func TestDeleteSession_GracefulTermination(t *testing.T) {
	t.Parallel()
	sm := new(mockAPISM)
	bridge := new(mockAPIBridge)
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

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
	api := newTestAPI(t, sm, bridge, nil)

	sm.On("Get", "no-sess").Return(nil, session.ErrSessionNotFound)

	mux := setupMux(api)
	w := httptest.NewRecorder()
	req := authedReq("POST", "/api/sessions/no-sess/cd", strings.NewReader(`{"work_dir":"/tmp"}`))
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetHistory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sessionID  string
		query      string
		setupMocks func(*mockAPISM, *mockHistoryStore)
		wantCode   int
		wantBody   func(t *testing.T, body map[string]any)
	}{
		{
			name:      "unauthorized",
			sessionID: "sess1",
			query:     "",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				// No auth header → 401, mocks not called
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name:      "session not found",
			sessionID: "nonexistent",
			query:     "",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				sm.On("Get", "nonexistent").Return(nil, session.ErrSessionNotFound)
			},
			wantCode: http.StatusNotFound,
		},
		{
			name:      "happy path",
			sessionID: "sess1",
			query:     "",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				sm.On("Get", "sess1").Return(&session.SessionInfo{
					ID:     "sess1",
					UserID: "anonymous",
					State:  events.StateIdle,
				}, nil)
				hs.On("GetBySession", mock.Anything, "sess1", 51, 0).Return([]*session.ConversationRecord{
					{ID: "t1", SessionID: "sess1", Seq: 1, Role: "user", Content: "hello"},
					{ID: "t2", SessionID: "sess1", Seq: 2, Role: "assistant", Content: "world"},
				}, nil)
			},
			wantCode: http.StatusOK,
			wantBody: func(t *testing.T, body map[string]any) {
				turns, ok := body["turns"].([]any)
				require.True(t, ok, "turns should be an array")
				require.Len(t, turns, 2)
				hasMore, ok := body["has_more"].(bool)
				require.True(t, ok)
				require.False(t, hasMore)
			},
		},
		{
			name:      "has_more detection",
			sessionID: "sess1",
			query:     "limit=2&offset=0",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				sm.On("Get", "sess1").Return(&session.SessionInfo{
					ID:     "sess1",
					UserID: "anonymous",
					State:  events.StateIdle,
				}, nil)
				// Return 3 records (limit+1=3) to trigger has_more
				hs.On("GetBySession", mock.Anything, "sess1", 3, 0).Return([]*session.ConversationRecord{
					{ID: "t1", SessionID: "sess1", Seq: 1, Role: "user", Content: "a"},
					{ID: "t2", SessionID: "sess1", Seq: 2, Role: "assistant", Content: "b"},
					{ID: "t3", SessionID: "sess1", Seq: 3, Role: "user", Content: "c"},
				}, nil)
			},
			wantCode: http.StatusOK,
			wantBody: func(t *testing.T, body map[string]any) {
				turns, ok := body["turns"].([]any)
				require.True(t, ok)
				require.Len(t, turns, 2, "should return only limit records, not limit+1")
				hasMore, ok := body["has_more"].(bool)
				require.True(t, ok)
				require.True(t, hasMore, "has_more should be true when extra record exists")
			},
		},
		{
			name:      "ownership check",
			sessionID: "sess_other",
			query:     "",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				sm.On("Get", "sess_other").Return(&session.SessionInfo{
					ID:     "sess_other",
					UserID: "different_user", // Not "anonymous"
					State:  events.StateIdle,
				}, nil)
			},
			wantCode: http.StatusForbidden,
		},
		{
			name:      "custom limit and offset",
			sessionID: "sess1",
			query:     "limit=10&offset=5",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				sm.On("Get", "sess1").Return(&session.SessionInfo{
					ID:     "sess1",
					UserID: "anonymous",
					State:  events.StateIdle,
				}, nil)
				hs.On("GetBySession", mock.Anything, "sess1", 11, 5).Return([]*session.ConversationRecord{
					{ID: "t6", SessionID: "sess1", Seq: 6, Role: "user", Content: "hello6"},
					{ID: "t7", SessionID: "sess1", Seq: 7, Role: "assistant", Content: "world7"},
				}, nil)
			},
			wantCode: http.StatusOK,
			wantBody: func(t *testing.T, body map[string]any) {
				turns, ok := body["turns"].([]any)
				require.True(t, ok)
				require.Len(t, turns, 2)
				hasMore, ok := body["has_more"].(bool)
				require.True(t, ok)
				require.False(t, hasMore)
			},
		},
		{
			name:      "history store error",
			sessionID: "sess1",
			query:     "",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				sm.On("Get", "sess1").Return(&session.SessionInfo{
					ID:     "sess1",
					UserID: "anonymous",
					State:  events.StateIdle,
				}, nil)
				hs.On("GetBySession", mock.Anything, "sess1", 51, 0).Return(([]*session.ConversationRecord)(nil), errors.New("database error"))
			},
			wantCode: http.StatusInternalServerError,
		},
		{
			name:      "invalid limit parameter",
			sessionID: "sess1",
			query:     "limit=abc&offset=0",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				sm.On("Get", "sess1").Return(&session.SessionInfo{
					ID:     "sess1",
					UserID: "anonymous",
					State:  events.StateIdle,
				}, nil)
				// Should use default limit (50) + 1 = 51
				hs.On("GetBySession", mock.Anything, "sess1", 51, 0).Return([]*session.ConversationRecord{
					{ID: "t1", SessionID: "sess1", Seq: 1, Role: "user", Content: "test"},
				}, nil)
			},
			wantCode: http.StatusOK,
			wantBody: func(t *testing.T, body map[string]any) {
				turns, ok := body["turns"].([]any)
				require.True(t, ok)
				require.Len(t, turns, 1)
			},
		},
		{
			name:      "negative limit uses default",
			sessionID: "sess1",
			query:     "limit=-5&offset=0",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				sm.On("Get", "sess1").Return(&session.SessionInfo{
					ID:     "sess1",
					UserID: "anonymous",
					State:  events.StateIdle,
				}, nil)
				hs.On("GetBySession", mock.Anything, "sess1", 51, 0).Return([]*session.ConversationRecord{
					{ID: "t1", SessionID: "sess1", Seq: 1, Role: "user", Content: "test"},
				}, nil)
			},
			wantCode: http.StatusOK,
			wantBody: func(t *testing.T, body map[string]any) {
				turns, ok := body["turns"].([]any)
				require.True(t, ok)
				require.Len(t, turns, 1)
			},
		},
		{
			name:      "max limit enforced",
			sessionID: "sess1",
			query:     "limit=300&offset=0",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				sm.On("Get", "sess1").Return(&session.SessionInfo{
					ID:     "sess1",
					UserID: "anonymous",
					State:  events.StateIdle,
				}, nil)
				// Max limit is 200, so should request 201 records (limit+1)
				hs.On("GetBySession", mock.Anything, "sess1", 201, 0).Return([]*session.ConversationRecord{
					{ID: "t1", SessionID: "sess1", Seq: 1, Role: "user", Content: "test"},
				}, nil)
			},
			wantCode: http.StatusOK,
			wantBody: func(t *testing.T, body map[string]any) {
				turns, ok := body["turns"].([]any)
				require.True(t, ok)
				require.Len(t, turns, 1)
			},
		},
		{
			name:      "invalid offset uses default",
			sessionID: "sess1",
			query:     "limit=10&offset=xyz",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				sm.On("Get", "sess1").Return(&session.SessionInfo{
					ID:     "sess1",
					UserID: "anonymous",
					State:  events.StateIdle,
				}, nil)
				// Should use default offset (0)
				hs.On("GetBySession", mock.Anything, "sess1", 11, 0).Return([]*session.ConversationRecord{
					{ID: "t1", SessionID: "sess1", Seq: 1, Role: "user", Content: "test"},
				}, nil)
			},
			wantCode: http.StatusOK,
			wantBody: func(t *testing.T, body map[string]any) {
				turns, ok := body["turns"].([]any)
				require.True(t, ok)
				require.Len(t, turns, 1)
			},
		},
		{
			name:      "negative offset uses default",
			sessionID: "sess1",
			query:     "limit=10&offset=-5",
			setupMocks: func(sm *mockAPISM, hs *mockHistoryStore) {
				sm.On("Get", "sess1").Return(&session.SessionInfo{
					ID:     "sess1",
					UserID: "anonymous",
					State:  events.StateIdle,
				}, nil)
				// Should use default offset (0)
				hs.On("GetBySession", mock.Anything, "sess1", 11, 0).Return([]*session.ConversationRecord{
					{ID: "t1", SessionID: "sess1", Seq: 1, Role: "user", Content: "test"},
				}, nil)
			},
			wantCode: http.StatusOK,
			wantBody: func(t *testing.T, body map[string]any) {
				turns, ok := body["turns"].([]any)
				require.True(t, ok)
				require.Len(t, turns, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sm := new(mockAPISM)
			hs := new(mockHistoryStore)
			tt.setupMocks(sm, hs)

			api := newTestAPI(t, sm, nil, hs)
			mux := setupMux(api)

			var url string
			if tt.query != "" {
				url = fmt.Sprintf("/api/sessions/%s/history?%s", tt.sessionID, tt.query)
			} else {
				url = fmt.Sprintf("/api/sessions/%s/history", tt.sessionID)
			}

			w := httptest.NewRecorder()
			var req *http.Request
			if tt.name == "unauthorized" {
				req = httptest.NewRequest("GET", url, nil)
			} else {
				req = authedReq("GET", url, nil)
			}
			mux.ServeHTTP(w, req)

			require.Equal(t, tt.wantCode, w.Code)

			if tt.wantBody != nil && w.Code == http.StatusOK {
				var body map[string]any
				err := json.NewDecoder(w.Body).Decode(&body)
				require.NoError(t, err)
				tt.wantBody(t, body)
			}

			sm.AssertExpectations(t)
			hs.AssertExpectations(t)
		})
	}
}
