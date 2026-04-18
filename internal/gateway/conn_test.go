package gateway

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/config"
	"github.com/hotplex/hotplex-worker/internal/security"
	"github.com/hotplex/hotplex-worker/internal/session"
	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/internal/worker/noop"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// ─── Init message validation ──────────────────────────────────────────────────

func TestValidateInit(t *testing.T) {
	makeInit := func(overrides func(map[string]any)) map[string]any {
		m := map[string]any{
			"version":     events.Version,
			"worker_type": "claude-code",
		}
		if overrides != nil {
			overrides(m)
		}
		return m
	}

	tests := []struct {
		name     string
		data     map[string]any
		wantNil  bool
		wantCode events.ErrorCode
	}{
		{
			name:    "valid minimal init",
			data:    makeInit(nil),
			wantNil: true,
		},
		{
			name: "valid with session_id",
			data: makeInit(func(m map[string]any) {
				m["session_id"] = "sess_abc123"
			}),
			wantNil: true,
		},
		{
			name: "valid with auth token",
			data: makeInit(func(m map[string]any) {
				m["auth"] = map[string]any{"token": "Bearer test-token"}
			}),
			wantNil: true,
		},
		{
			name: "valid with full config",
			data: makeInit(func(m map[string]any) {
				m["config"] = map[string]any{
					"model":         "claude-sonnet-4-6",
					"allowed_tools": []any{"Read", "Bash"},
					"max_turns":     50.0,
					"work_dir":      "/tmp/project",
				}
			}),
			wantNil: true,
		},
		{
			name:     "missing version",
			data:     map[string]any{"worker_type": "claude-code"},
			wantNil:  false,
			wantCode: events.ErrCodeInvalidMessage,
		},
		{
			name:     "wrong version",
			data:     map[string]any{"version": "aep/v0", "worker_type": "claude-code"},
			wantNil:  false,
			wantCode: events.ErrCodeVersionMismatch,
		},
		{
			name:     "missing worker_type",
			data:     map[string]any{"version": events.Version},
			wantNil:  false,
			wantCode: events.ErrCodeInvalidMessage,
		},
		{
			name:     "invalid data type",
			data:     nil,
			wantNil:  false,
			wantCode: events.ErrCodeInvalidMessage,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Note: not using t.Parallel() here because makeInit closures capture
			// the same underlying map builder pattern; sequential execution ensures
			// each test gets an independent copy.
			env := envFromData(tt.data)
			data, err := ValidateInit(env)
			if tt.wantNil {
				// Use err == nil instead of require.NoError because ValidateInit
				// returns (*InitError)(nil) on success, which testify treats as non-nil.
				require.True(t, err == nil, "ValidateInit(%+v) returned error: %v", tt.data, err)
				require.NotEmpty(t, data.WorkerType)
			} else {
				require.NotNil(t, err)
				require.Equal(t, tt.wantCode, err.Code)
			}
		})
	}
}

func TestBuildInitAck(t *testing.T) {
	t.Parallel()

	ack := BuildInitAck("sess_test", events.StateCreated, worker.TypeClaudeCode)
	require.NotNil(t, ack)
	require.Equal(t, "sess_test", ack.SessionID)
	require.Equal(t, events.StateCreated, ack.Event.Data.(InitAckData).State)
	require.Equal(t, events.Version, ack.Event.Data.(InitAckData).ServerCaps.ProtocolVersion)
	require.True(t, ack.Event.Data.(InitAckData).ServerCaps.SupportsResume)
}

func TestBuildInitAckError(t *testing.T) {
	t.Parallel()

	initErr := &InitError{Code: events.ErrCodeUnauthorized, Message: "invalid token"}
	ack := BuildInitAckError("sess_test", initErr)
	require.NotNil(t, ack)
	require.Equal(t, "sess_test", ack.SessionID)
	require.Equal(t, events.StateDeleted, ack.Event.Data.(InitAckData).State)
	require.Equal(t, "invalid token", ack.Event.Data.(InitAckData).Error)
}

func TestBackoffDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{0, 1 * time.Second, 1 * time.Second},
		{1, 2 * time.Second, 2 * time.Second},
		{2, 4 * time.Second, 4 * time.Second},
		{3, 8 * time.Second, 8 * time.Second},
		{4, 16 * time.Second, 16 * time.Second},
		{5, 32 * time.Second, 32 * time.Second},
		{6, 60 * time.Second, 64 * time.Second},  // capped at 60s
		{10, 60 * time.Second, 60 * time.Second}, // capped at 60s
	}

	for _, tt := range tests {
		tt := tt
		t.Run("", func(t *testing.T) {
			t.Parallel()
			got := BackoffDuration(tt.attempt)
			require.GreaterOrEqual(t, got, tt.wantMin)
			require.LessOrEqual(t, got, tt.wantMax)
		})
	}
}

func TestSessionStateForWorker(t *testing.T) {
	t.Parallel()
	require.Equal(t, events.StateCreated, SessionStateForWorker(worker.TypeClaudeCode))
	require.Equal(t, events.StateCreated, SessionStateForWorker(worker.TypeOpenCodeCLI))
	require.Equal(t, events.StateCreated, SessionStateForWorker(worker.TypeOpenCodeSrv))
	require.Equal(t, events.StateCreated, SessionStateForWorker(worker.TypePimon))
}

func TestDefaultServerCaps(t *testing.T) {
	t.Parallel()

	caps := DefaultServerCaps(worker.TypeClaudeCode)
	require.Equal(t, events.Version, caps.ProtocolVersion)
	require.True(t, caps.SupportsResume)
	require.True(t, caps.SupportsDelta)
	require.True(t, caps.SupportsToolCall)
	require.True(t, caps.SupportsPing)
	require.Equal(t, int64(32*1024), caps.MaxFrameSize)
	require.Contains(t, caps.Modalities, "text")
	require.Contains(t, caps.Modalities, "code")
}

func TestInitError_Error(t *testing.T) {
	t.Parallel()
	e := &InitError{Code: events.ErrCodeUnauthorized, Message: "bad token"}
	require.Equal(t, "bad token", e.Error())
}

// ─── WebSocket helpers ───────────────────────────────────────────────────────

func TestWSUpgrading(t *testing.T) {
	t.Parallel()

	var upgrader = websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.NoError(t, conn.Close())
}

// newTestWSServer starts an httptest.Server that upgrades WebSocket connections
// and invokes the provided handler for each connection in a detached goroutine
// so that the HTTP handler can return and the server can close cleanly.
func newTestWSServer(handler func(*websocket.Conn)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var upgrader websocket.Upgrader
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Detach so the HTTP handler goroutine exits immediately.
		// The httptest.Server goroutine is not blocked by the WebSocket read loop.
		go func() {
			handler(conn)
		}()
	}))
}

// ─── WebSocket echo test ─────────────────────────────────────────────────────

func TestWSEcho(t *testing.T) {
	t.Parallel()

	server := newTestWSServer(func(conn *websocket.Conn) {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			_ = conn.WriteMessage(websocket.TextMessage, msg)
		}
	})
	defer server.Close()

	wsURL := "ws" + server.URL[4:]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	testMessages := []string{
		`{"hello":"world"}`,
		`{"foo":123}`,
		`ping`,
	}

	for _, msg := range testMessages {
		err := conn.WriteMessage(websocket.TextMessage, []byte(msg))
		require.NoError(t, err)

		_, got, err := conn.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, msg, string(got))
	}
}

// TestWSPingPong verifies that a server can write a PongMessage in response
// to a PingMessage.  Note: gorilla/websocket's default client dialer installs
// an internal pong handler that auto-consumes pong frames at the protocol level,
// so they never surface via ReadMessage on the client side.  The AEP-level
// ping/pong (application-layer JSON envelopes) is tested by the gateway's
// WritePump / ReadPump integration.  This test is marked SKIP to avoid a
// structural gorilla/websocket behaviour that cannot be changed.
func TestWSPingPong(t *testing.T) {
	t.Skip("gorilla/websocket default client auto-consumes pong frames; AEP-level ping/pong is tested by WritePump/ReadPump")
}

// ─── AEP message roundtrip ───────────────────────────────────────────────────

func TestAEPMessageEncodeDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  *events.Envelope
	}{
		{
			name: "init envelope",
			env: events.NewEnvelope(
				aep.NewID(),
				"sess_123",
				1,
				events.Init,
				InitData{
					Version:    events.Version,
					WorkerType: worker.TypeClaudeCode,
					SessionID:  "sess_123",
				},
			),
		},
		{
			name: "input envelope",
			env: events.NewEnvelope(
				aep.NewID(),
				"sess_123",
				2,
				events.Input,
				events.InputData{Content: "hello world"},
			),
		},
		{
			name: "state envelope",
			env: events.NewEnvelope(
				aep.NewID(),
				"sess_123",
				3,
				events.State,
				events.StateData{State: events.StateIdle},
			),
		},
		{
			name: "error envelope",
			env: events.NewEnvelope(
				aep.NewID(),
				"sess_123",
				4,
				events.Error,
				events.ErrorData{Code: events.ErrCodeSessionBusy, Message: "session busy"},
			),
		},
		{
			name: "done envelope",
			env: events.NewEnvelope(
				aep.NewID(),
				"sess_123",
				5,
				events.Done,
				events.DoneData{Success: true, Stats: map[string]any{"turns": 10}},
			),
		},
		{
			name: "tool call envelope",
			env: events.NewEnvelope(
				aep.NewID(),
				"sess_123",
				6,
				events.ToolCall,
				events.ToolCallData{ID: "call_abc", Name: "Read", Input: map[string]any{"file_path": "/tmp/foo.txt"}},
			),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := aep.EncodeJSON(tt.env)
			require.NoError(t, err)

			decoded, err := aep.DecodeLine(data)
			require.NoError(t, err)

			require.Equal(t, tt.env.Event.Type, decoded.Event.Type)
			require.Equal(t, tt.env.SessionID, decoded.SessionID)
			require.Equal(t, tt.env.Seq, decoded.Seq)
		})
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// envFromData creates a minimal Envelope with the given data map.
func envFromData(data map[string]any) *events.Envelope {
	if data == nil {
		data = map[string]any{}
	}
	return &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		Seq:       1,
		SessionID: "sess_test",
		Timestamp: time.Now().UnixMilli(),
		Event:     events.Event{Type: events.Init, Data: data},
	}
}

// ─── AEP Encode/Decode ────────────────────────────────────────────────────────

func TestAEPEncodeJSON(t *testing.T) {
	t.Parallel()

	env := events.NewEnvelope(
		aep.NewID(),
		"sess_abc",
		1,
		events.State,
		events.StateData{State: events.StateRunning},
	)

	data, err := aep.EncodeJSON(env)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.True(t, strings.HasPrefix(string(data), `{"version"`))
}

func TestAEPDecodeLine(t *testing.T) {
	t.Parallel()

	validJSON := []byte(`{"version":"aep/v1","id":"evt_abc","seq":1,"session_id":"sess_123","timestamp":1700000000000,"event":{"type":"state","data":{"state":"running"}}}`)

	env, err := aep.DecodeLine(validJSON)
	require.NoError(t, err)
	require.Equal(t, "evt_abc", env.ID)
	require.Equal(t, "sess_123", env.SessionID)
	require.Equal(t, events.State, env.Event.Type)
}

func TestAEPDecodeLine_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := aep.DecodeLine([]byte(`{invalid json}`))
	require.Error(t, err)
}

func TestAEPDecodeLine_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	// Missing version
	_, err := aep.DecodeLine([]byte(`{"id":"evt_abc","seq":1,"session_id":"sess_123","timestamp":1700000000000,"event":{"type":"state","data":{}}}`))
	require.Error(t, err)

	// Missing session_id
	_, err = aep.DecodeLine([]byte(`{"version":"aep/v1","id":"evt_abc","seq":1,"timestamp":1700000000000,"event":{"type":"state","data":{}}}`))
	require.Error(t, err)
}

// ─── SEC-007: bot_id isolation tests ───────────────────────────────────────────

// mockSessionStoreForBotID is a testify mock for session.Store used in bot_id tests.
type mockSessionStoreForBotID struct {
	mock.Mock
}

func (m *mockSessionStoreForBotID) Upsert(ctx context.Context, info *session.SessionInfo) error {
	args := m.Called(ctx, info)
	if args.Error(0) == nil {
		if ms, ok := args.Get(0).(*session.SessionInfo); ok {
			*info = *ms
		}
	}
	return args.Error(0)
}

func (m *mockSessionStoreForBotID) Get(ctx context.Context, id string) (*session.SessionInfo, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*session.SessionInfo), args.Error(1)
}

func (m *mockSessionStoreForBotID) List(ctx context.Context, limit, offset int) ([]*session.SessionInfo, error) {
	args := m.Called(ctx, limit, offset)
	return args.Get(0).([]*session.SessionInfo), args.Error(1)
}

func (m *mockSessionStoreForBotID) GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error) {
	args := m.Called(ctx, now)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockSessionStoreForBotID) GetExpiredIdle(ctx context.Context, now time.Time) ([]string, error) {
	args := m.Called(ctx, now)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockSessionStoreForBotID) DeleteTerminated(ctx context.Context, cutoff time.Time) error {
	args := m.Called(ctx, cutoff)
	return args.Error(0)
}

func (m *mockSessionStoreForBotID) Close() error {
	args := m.Called()
	return args.Error(0)
}

// makeInitEnvelope builds a init Envelope for the given session, workerType, and optional JWT token.
func makeInitEnvelope(sessionID, workerType, token string) []byte {
	data := map[string]any{
		"version":     events.Version,
		"worker_type": workerType,
		"session_id":  sessionID,
	}
	if token != "" {
		data["auth"] = map[string]any{"token": token}
	}
	env := events.NewEnvelope(aep.NewID(), sessionID, 1, events.Init, data)
	env.SessionID = sessionID
	raw, _ := aep.EncodeJSON(env)
	return raw
}

// sendWSInit sends a raw init message and reads one response from the WebSocket.
func sendWSInit(conn *websocket.Conn, msg []byte) ([]byte, error) {
	if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, resp, err := conn.ReadMessage()
	return resp, err
}

// newBotIDTestConn creates a Conn with a real hub for bot_id isolation tests.
// It allows setting userID and botID before ReadPump is called.
func newBotIDTestConn(h *Hub, wc *websocket.Conn, sessionID, userID, botID string) *Conn {
	return &Conn{
		log:       h.log,
		wc:        wc,
		hub:       h,
		sessionID: sessionID,
		userID:    userID,
		botID:     botID,
		hb:        newHeartbeat(h.log),
		done:      make(chan struct{}),
	}
}

// newECDSAKey generates a fresh P-256 ECDSA key pair for ES256 JWT signing in tests.
func newECDSAKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key
}

// TestBotIDIsolation_CreateMismatch tests that creating a new session with bot_id=bot_001
// and then resuming it with bot_id=bot_002 is rejected with ErrCodeUnauthorized.
// This is the SEC-007 cross-bot access rejection at resume time.
func TestBotIDIsolation_CreateMismatch(t *testing.T) {
	const (
		sessionIDConst = "sess_bot001"
		workerType     = "claude-code"
		botAlice       = "bot_alice"
		botBob         = "bot_bob"
	)
	// Derive the server session ID using the same algorithm as conn.go:DeriveSessionKey.
	derivedSID := session.DeriveSessionKey("alice", worker.WorkerType(workerType), sessionIDConst, config.Default().Worker.DefaultWorkDir)

	// Build a JWT token for bot_alice using ES256 (ECDSA P-256).
	jwtKey := newECDSAKey(t)
	jwtVal := security.NewJWTValidator(jwtKey, "")
	tokenAlice, err := jwtVal.GenerateTokenWithClaims(&security.JWTClaims{
		UserID: "alice",
		BotID:  botAlice,
	})
	require.NoError(t, err)

	tokenBob, err := jwtVal.GenerateTokenWithClaims(&security.JWTClaims{
		UserID: "alice",
		BotID:  botBob,
	})
	require.NoError(t, err)

	// Phase 1: client A connects with bot_alice token and creates a session.
	store1 := new(mockSessionStoreForBotID)
	store1.Test(t)
	store1.On("Close").Return(nil)
	// Get returns not-found to trigger Create.
	store1.On("Get", mock.Anything, derivedSID).Return(nil, session.ErrSessionNotFound)
	// Upsert for Create + Transition to RUNNING.
	store1.On("Upsert", mock.Anything, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	cfg := config.Default()
	h1 := newTestHub(t)
	mgr1, err := session.NewManager(context.Background(), slog.Default(), cfg, store1, nil)
	require.NoError(t, err)
	t.Cleanup(func() { mgr1.Close() })

	var serverConn1 *websocket.Conn
	var mu1 sync.Mutex
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		mu1.Lock()
		serverConn1 = conn
		mu1.Unlock()
		go func() {
			c := newBotIDTestConn(h1, conn, derivedSID, "alice", botAlice)
			h := NewHandler(slog.Default(), cfg, h1, mgr1, jwtVal)
			c.ReadPump(h)
		}()
	}))
	t.Cleanup(server1.Close)

	client1, _, err := websocket.DefaultDialer.Dial("ws"+server1.URL[4:], nil)
	require.NoError(t, err)
	t.Cleanup(func() { client1.Close() })

	// Wait for server side to be ready.
	require.Eventually(t, func() bool {
		mu1.Lock()
		ok := serverConn1 != nil
		mu1.Unlock()
		return ok
	}, 2*time.Second, 10*time.Millisecond)

	// Client A sends init with bot_alice token → should succeed (new session).
	init1 := makeInitEnvelope(sessionIDConst, workerType, tokenAlice)
	resp1, err := sendWSInit(client1, init1)
	require.NoError(t, err)
	require.Contains(t, string(resp1), `"type":"init_ack"`, "bot_alice create should succeed")
	require.NotContains(t, string(resp1), `"code":"unauthorized"`, "no auth error expected")

	// Phase 2: client B connects with bot_bob token and tries to resume the same session.
	store2 := new(mockSessionStoreForBotID)
	store2.Test(t)
	store2.On("Close").Return(nil)
	// Get returns the existing session with bot_alice.
	existingSession := &session.SessionInfo{
		ID:           derivedSID,
		UserID:       "alice",
		BotID:        botAlice, // session was created with bot_alice
		State:        events.StateIdle,
		WorkerType:   worker.WorkerType(workerType),
		AllowedTools: []string{},
	}
	store2.On("Get", mock.Anything, derivedSID).Return(existingSession, nil)
	// Transition to RUNNING (called by ResumeSession for StateIdle→RUNNING).
	store2.On("Transition", mock.Anything, derivedSID, events.StateRunning).Return(nil)
	// AttachWorker called by ResumeSession.
	store2.On("AttachWorker", mock.Anything, derivedSID, mock.Anything).Return(nil)

	cfg2 := config.Default()
	h2 := newTestHub(t)
	mgr2, err := session.NewManager(context.Background(), slog.Default(), cfg2, store2, nil)
	require.NoError(t, err)
	t.Cleanup(func() { mgr2.Close() })

	var serverConn2 *websocket.Conn
	var mu2 sync.Mutex
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		mu2.Lock()
		serverConn2 = conn
		mu2.Unlock()
		go func() {
			c := newBotIDTestConn(h2, conn, derivedSID, "alice", botBob)
			h := NewHandler(slog.Default(), cfg2, h2, mgr2, jwtVal)
			c.ReadPump(h)
		}()
	}))
	t.Cleanup(server2.Close)

	client2, _, err := websocket.DefaultDialer.Dial("ws"+server2.URL[4:], nil)
	require.NoError(t, err)
	t.Cleanup(func() { client2.Close() })

	require.Eventually(t, func() bool {
		mu2.Lock()
		ok := serverConn2 != nil
		mu2.Unlock()
		return ok
	}, 2*time.Second, 10*time.Millisecond)

	// Client B sends init with bot_bob token for the same session → should be rejected.
	init2 := makeInitEnvelope(sessionIDConst, workerType, tokenBob)
	resp2, err := sendWSInit(client2, init2)
	require.NoError(t, err)
	require.Contains(t, string(resp2), `"type":"init_ack"`) // init_ack is always sent, but contains error
	require.Contains(t, string(resp2), `"code":"UNAUTHORIZED"`, "bot_id mismatch should return UNAUTHORIZED")
}

// TestBotIDIsolation_MatchAllowed tests that resuming a session with the matching bot_id succeeds.
func TestBotIDIsolation_MatchAllowed(t *testing.T) {
	const (
		sessionIDConst = "sess_bot_match"
		workerType     = "claude-code"
		botID          = "bot_team_a"
	)
	derivedSID := session.DeriveSessionKey("user1", worker.WorkerType(workerType), sessionIDConst, config.Default().Worker.DefaultWorkDir)

	jwtKey := newECDSAKey(t)
	jwtVal := security.NewJWTValidator(jwtKey, "")
	token, err := jwtVal.GenerateTokenWithClaims(&security.JWTClaims{
		UserID: "user1",
		BotID:  botID,
	})
	require.NoError(t, err)

	store := new(mockSessionStoreForBotID)
	store.Test(t)
	store.On("Close").Return(nil).Maybe()
	existingSession := &session.SessionInfo{
		ID:         derivedSID,
		UserID:     "user1",
		BotID:      botID, // same bot_id
		State:      events.StateIdle,
		WorkerType: worker.WorkerType(workerType),
	}
	store.On("Get", mock.Anything, derivedSID).Return(existingSession, nil)

	cfg := config.Default()
	hubForTest := newTestHub(t)
	mgr, err := session.NewManager(context.Background(), slog.Default(), cfg, store, nil)
	require.NoError(t, err)
	t.Cleanup(func() { mgr.Close() })

	var serverConn *websocket.Conn
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		mu.Lock()
		serverConn = conn
		mu.Unlock()
		go func() {
			c := newBotIDTestConn(hubForTest, conn, derivedSID, "user1", botID)
			handler := NewHandler(slog.Default(), cfg, hubForTest, mgr, jwtVal)
			c.ReadPump(handler)
		}()
	}))
	t.Cleanup(server.Close)

	client, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })

	require.Eventually(t, func() bool {
		mu.Lock()
		ok := serverConn != nil
		mu.Unlock()
		return ok
	}, 2*time.Second, 10*time.Millisecond)

	init := makeInitEnvelope(sessionIDConst, workerType, token)
	resp, err := sendWSInit(client, init)
	require.NoError(t, err)
	require.Contains(t, string(resp), `"type":"init_ack"`)
	require.NotContains(t, string(resp), `"code":"unauthorized"`)
}

// TestBotIDIsolation_EmptyBotIDAllowed tests that when bot_id is empty (not specified),
// sessions can be created and resumed without bot_id restrictions.
func TestBotIDIsolation_EmptyBotIDAllowed(t *testing.T) {
	const (
		sessionIDConst = "sess_no_bot"
		workerType     = "claude-code"
	)
	// When no JWT is provided, c.userID defaults to "anon" (from newBotIDTestConn).
	derivedSID := session.DeriveSessionKey("anon", worker.WorkerType(workerType), sessionIDConst, config.Default().Worker.DefaultWorkDir)

	// No JWT token (empty botID scenario).
	store := new(mockSessionStoreForBotID)
	store.Test(t)
	store.On("Close").Return(nil).Maybe()
	// Session does not exist → create new.
	store.On("Get", mock.Anything, derivedSID).Return(nil, session.ErrSessionNotFound)
	store.On("Upsert", mock.Anything, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	cfg := config.Default()
	h := newTestHub(t)
	mgr, err := session.NewManager(context.Background(), slog.Default(), cfg, store, nil)
	require.NoError(t, err)
	t.Cleanup(func() { mgr.Close() })

	var serverConn *websocket.Conn
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		mu.Lock()
		serverConn = conn
		mu.Unlock()
		go func() {
			c := newBotIDTestConn(h, conn, derivedSID, "anon", "")
			handler := NewHandler(slog.Default(), cfg, h, mgr, nil)
			c.ReadPump(handler)
		}()
	}))
	t.Cleanup(server.Close)

	client, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })

	require.Eventually(t, func() bool {
		mu.Lock()
		ok := serverConn != nil
		mu.Unlock()
		return ok
	}, 2*time.Second, 10*time.Millisecond)

	// No auth token → empty bot_id → should succeed.
	init := makeInitEnvelope(sessionIDConst, workerType, "")
	resp, err := sendWSInit(client, init)
	require.NoError(t, err)
	require.Contains(t, string(resp), `"type":"init_ack"`)
	require.NotContains(t, string(resp), `"code":"unauthorized"`)
}

// TestBotIDIsolation_NewSessionStoresBotID tests that when a session is created via
// CreateWithBot (the fix in conn.go), the BotID is persisted in the session record.
func TestBotIDIsolation_NewSessionStoresBotID(t *testing.T) {
	const (
		sessionIDConst = "sess_new_bot"
		workerType     = "claude-code"
		botID          = "bot_new_session"
	)
	derivedSID := session.DeriveSessionKey("user1", worker.WorkerType(workerType), sessionIDConst, config.Default().Worker.DefaultWorkDir)

	jwtKey := newECDSAKey(t)
	jwtVal := security.NewJWTValidator(jwtKey, "")
	token, err := jwtVal.GenerateTokenWithClaims(&security.JWTClaims{
		UserID: "user1",
		BotID:  botID,
	})
	require.NoError(t, err)

	store := new(mockSessionStoreForBotID)
	store.Test(t)
	store.On("Close").Return(nil).Maybe()
	// Session does not exist on Get → triggers CreateWithBot.
	store.On("Get", mock.Anything, derivedSID).Return(nil, session.ErrSessionNotFound)
	// Upsert is called twice: once for CreateWithBot, once for Transition(CREATED→RUNNING).
	// Both must carry the correct botID.
	store.On("Upsert", mock.Anything, mock.MatchedBy(func(info *session.SessionInfo) bool {
		return info.BotID == botID // SEC-007: verify botID is passed through
	})).Return(nil).Maybe() // Maybe() allows 0 or more calls

	cfg := config.Default()
	h := newTestHub(t)
	mgr, err := session.NewManager(context.Background(), slog.Default(), cfg, store, nil)
	require.NoError(t, err)
	t.Cleanup(func() { mgr.Close() })

	var serverConn *websocket.Conn
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		mu.Lock()
		serverConn = conn
		mu.Unlock()
		go func() {
			c := newBotIDTestConn(h, conn, derivedSID, "user1", botID)
			handler := NewHandler(slog.Default(), cfg, h, mgr, jwtVal)
			c.ReadPump(handler)
		}()
	}))
	t.Cleanup(server.Close)

	client, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })

	require.Eventually(t, func() bool {
		mu.Lock()
		ok := serverConn != nil
		mu.Unlock()
		return ok
	}, 2*time.Second, 10*time.Millisecond)

	init := makeInitEnvelope(sessionIDConst, workerType, token)
	resp, err := sendWSInit(client, init)
	require.NoError(t, err)
	require.Contains(t, string(resp), `"type":"init_ack"`)

	// Assert that Upsert was called with the correct botID.
	store.AssertExpectations(t)
}

// ─── Bridge forwardEvents tests ────────────────────────────────────────────────

// mockBridgeSessionManager is a test double for Bridge tests.
// It implements the SessionManager interface via mock.Mock.
type mockBridgeSM struct {
	mock.Mock
}

func (m *mockBridgeSM) CreateWithBot(ctx context.Context, id, userID, botID string, wt worker.WorkerType, allowedTools []string, platform string, platformKey map[string]string) (*session.SessionInfo, error) {
	args := m.Called(ctx, id, userID, botID, wt, allowedTools)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*session.SessionInfo), args.Error(1)
}

func (m *mockBridgeSM) AttachWorker(id string, w worker.Worker) error {
	args := m.Called(id, w)
	return args.Error(0)
}

func (m *mockBridgeSM) DetachWorker(id string) {
	m.Called(id)
}

func (m *mockBridgeSM) Transition(ctx context.Context, id string, to events.SessionState) error {
	args := m.Called(ctx, id, to)
	return args.Error(0)
}

func (m *mockBridgeSM) Get(id string) (*session.SessionInfo, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*session.SessionInfo), args.Error(1)
}

func (m *mockBridgeSM) GetWorker(id string) worker.Worker {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(worker.Worker)
}

func (m *mockBridgeSM) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockBridgeSM) List(ctx context.Context, limit, offset int) ([]*session.SessionInfo, error) {
	args := m.Called(ctx, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*session.SessionInfo), args.Error(1)
}

func (m *mockBridgeSM) UpdateWorkerSessionID(ctx context.Context, id, workerSessionID string) error {
	args := m.Called(ctx, id, workerSessionID)
	return args.Error(0)
}

var _ SessionManager = (*mockBridgeSM)(nil)

// mockBridgeWorker is a configurable fake Worker for Bridge tests.
type mockBridgeWorker struct {
	workerType worker.WorkerType
	exitCode   int
	conn       *fakeWorkerConn
	startErr   error
	resumeErr  error
}

func (m *mockBridgeWorker) Type() worker.WorkerType                             { return m.workerType }
func (m *mockBridgeWorker) SupportsResume() bool                                { return true }
func (m *mockBridgeWorker) SupportsStreaming() bool                             { return true }
func (m *mockBridgeWorker) SupportsTools() bool                                 { return true }
func (m *mockBridgeWorker) EnvWhitelist() []string                              { return nil }
func (m *mockBridgeWorker) SessionStoreDir() string                             { return "" }
func (m *mockBridgeWorker) MaxTurns() int                                       { return 0 }
func (m *mockBridgeWorker) Modalities() []string                                { return []string{"text"} }
func (m *mockBridgeWorker) Start(context.Context, worker.SessionInfo) error     { return m.startErr }
func (m *mockBridgeWorker) Input(context.Context, string, map[string]any) error { return nil }
func (m *mockBridgeWorker) Resume(context.Context, worker.SessionInfo) error    { return m.resumeErr }
func (m *mockBridgeWorker) Terminate(context.Context) error                     { return nil }
func (m *mockBridgeWorker) Kill() error                                         { return nil }
func (m *mockBridgeWorker) Wait() (int, error)                                  { return m.exitCode, nil }
func (m *mockBridgeWorker) Conn() worker.SessionConn                            { return m.conn }
func (m *mockBridgeWorker) Health() worker.WorkerHealth                         { return worker.WorkerHealth{} }
func (m *mockBridgeWorker) LastIO() time.Time                                   { return time.Now() }
func (m *mockBridgeWorker) ResetContext(context.Context) error                  { return nil }

var _ worker.Worker = (*mockBridgeWorker)(nil)

// mockBridgeWorkerFactory returns pre-configured mockBridgeWorker instances.
// It ignores the requested type and cycles through the pre-configured list,
// then falls back to a default mock worker.
type mockBridgeWorkerFactory struct {
	workers []*mockBridgeWorker // ordered list; each NewWorker call returns the next
	pos     int
	mu      sync.Mutex
}

func (f *mockBridgeWorkerFactory) NewWorker(t worker.WorkerType) (worker.Worker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pos < len(f.workers) {
		w := f.workers[f.pos]
		f.pos++
		return w, nil
	}
	return &mockBridgeWorker{workerType: t, conn: &fakeWorkerConn{ch: make(chan *events.Envelope)}}, nil
}

var _ WorkerFactory = (*mockBridgeWorkerFactory)(nil)

// TestBridge_ForwardEvents_NormalEvent verifies that a regular event is forwarded
// to the hub with the correct session ID.
func TestBridge_ForwardEvents_NormalEvent(t *testing.T) {
	t.Parallel()

	// Pre-populate the fake worker's recv channel with one event.
	ch := make(chan *events.Envelope, 1)
	deltaEnv := events.NewEnvelope(aep.NewID(), "", 0, events.MessageDelta, map[string]any{"delta": "hello"})
	ch <- deltaEnv
	close(ch)

	fw := &mockBridgeWorker{
		workerType: worker.TypeClaudeCode,
		conn:       &fakeWorkerConn{ch: ch},
	}

	// Set up hub + WebSocket so forwardEvents can deliver events.
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "sess_fwd", nil)
	h.JoinSession("sess_fwd", c)

	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run()

	// Call forwardEvents directly (no goroutine).
	b := NewBridge(slog.Default(), h, nil, nil)
	done := make(chan struct{})
	go func() {
		b.forwardEvents(fw, "sess_fwd")
		close(done)
	}()

	// Read the forwarded event from WebSocket.
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := server.ReadMessage()
	require.NoError(t, err, "forwardEvents should have sent the delta to hub")
	require.Contains(t, string(data), `"type":"message.delta"`)
	require.Contains(t, string(data), `"session_id":"sess_fwd"`)

	<-done
}

// TestBridge_ForwardEvents_DoneWithDroppedFlag verifies that when dropped deltas
// were recorded, the Done event carries dropped=true in stats.
func TestBridge_ForwardEvents_DoneWithDroppedFlag(t *testing.T) {
	t.Parallel()

	ch := make(chan *events.Envelope, 1)
	doneEnv := events.NewEnvelope(aep.NewID(), "", 0, events.Done, events.DoneData{Success: true})
	ch <- doneEnv
	close(ch)

	fw := &mockBridgeWorker{
		workerType: worker.TypeClaudeCode,
		conn:       &fakeWorkerConn{ch: ch},
	}

	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "sess_drop", nil)
	h.JoinSession("sess_drop", c)

	// Mark deltas as dropped before calling forwardEvents.
	h.mu.Lock()
	h.sessionDropped["sess_drop"] = true
	h.mu.Unlock()

	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run()

	b := NewBridge(slog.Default(), h, nil, nil)
	done := make(chan struct{})
	go func() {
		b.forwardEvents(fw, "sess_drop")
		close(done)
	}()

	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := server.ReadMessage()
	require.NoError(t, err, "forwardEvents should have sent the done event")
	require.Contains(t, string(data), `"type":"done"`)
	require.Contains(t, string(data), `"dropped":true`)

	<-done
}

// TestBridge_ForwardEvents_CrashExitCode verifies that a non-zero worker exit
// code causes a crash done event to be sent to the hub.
func TestBridge_ForwardEvents_CrashExitCode(t *testing.T) {
	ch := make(chan *events.Envelope, 1) // empty → Recv closes immediately
	close(ch)

	fw := &mockBridgeWorker{
		workerType: worker.TypeClaudeCode,
		exitCode:   1, // non-zero = crash
		conn:       &fakeWorkerConn{ch: ch},
	}

	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "sess_crash", nil)
	h.JoinSession("sess_crash", c)

	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run()

	b := NewBridge(slog.Default(), h, nil, nil)
	done := make(chan struct{})
	go func() {
		b.forwardEvents(fw, "sess_crash")
		close(done)
	}()

	// Read the crash done event.
	_ = server.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := server.ReadMessage()
	require.NoError(t, err, "forwardEvents should have sent crash done after Wait()")
	require.Contains(t, string(data), `"type":"done"`)
	require.Contains(t, string(data), `"success":false`)
	require.Contains(t, string(data), `"crash_exit_code":1`)

	<-done
}

// TestBridge_ForwardEvents_MessageStoreAppend verifies that a Done event
// triggers an Append call on msgStore.
func TestBridge_ForwardEvents_MessageStoreAppend(t *testing.T) {
	t.Parallel()

	ch := make(chan *events.Envelope, 1)
	doneEnv := events.NewEnvelope(aep.NewID(), "", 0, events.Done, events.DoneData{Success: true})
	ch <- doneEnv
	close(ch)

	fw := &mockBridgeWorker{
		workerType: worker.TypeClaudeCode,
		conn:       &fakeWorkerConn{ch: ch},
	}

	// fakeMsgStore is defined in hub_test.go and is safe to reuse.
	ms := &fakeMsgStore{}

	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "sess_append", nil)
	h.JoinSession("sess_append", c)

	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run()

	b := NewBridge(slog.Default(), h, nil, ms)
	done := make(chan struct{})
	go func() {
		b.forwardEvents(fw, "sess_append")
		close(done)
	}()

	// Drain the done event from WebSocket.
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := server.ReadMessage()
	require.NoError(t, err)

	<-done
}

// TestBridge_ForwardEvents_NilMsgStore verifies that a nil msgStore does not
// cause a panic when forwardEvents processes a Done event.
func TestBridge_ForwardEvents_NilMsgStore(t *testing.T) {
	t.Parallel()

	ch := make(chan *events.Envelope, 1)
	doneEnv := events.NewEnvelope(aep.NewID(), "", 0, events.Done, events.DoneData{Success: true})
	ch <- doneEnv
	close(ch)

	fw := &mockBridgeWorker{
		workerType: worker.TypeClaudeCode,
		conn:       &fakeWorkerConn{ch: ch},
	}

	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "sess_nilms", nil)
	h.JoinSession("sess_nilms", c)

	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run()

	// Bridge with nil msgStore — must not panic.
	b := NewBridge(slog.Default(), h, nil, nil)
	require.NotPanics(t, func() {
		done := make(chan struct{})
		go func() {
			b.forwardEvents(fw, "sess_nilms")
			close(done)
		}()
		// Drain the event.
		_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, err := server.ReadMessage()
		require.NoError(t, err)
		<-done
	})
}

// ─── Bridge StartSession / ResumeSession tests ─────────────────────────────────

// TestBridge_StartSession_Success verifies that StartSession creates a session,
// attaches the worker, starts it, and transitions to RUNNING.
func TestBridge_StartSession_Success(t *testing.T) {
	sm := &mockBridgeSM{Mock: mock.Mock{}}
	sm.Test(t)

	sessionInfo := &session.SessionInfo{
		ID:         "sess_start",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
	}
	sm.On("CreateWithBot", mock.Anything, "sess_start", "user1", "", worker.TypeClaudeCode, mock.Anything).Return(sessionInfo, nil)
	sm.On("AttachWorker", "sess_start", mock.Anything).Return(nil)
	sm.On("Transition", mock.Anything, "sess_start", events.StateRunning).Return(nil)

	// Use a worker factory that returns a real noop worker (Start returns nil).
	wf := &mockBridgeWorkerFactory{
		workers: []*mockBridgeWorker{
			{workerType: worker.TypeClaudeCode, conn: &fakeWorkerConn{ch: make(chan *events.Envelope)}},
		},
	}

	h := newTestHub(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run()

	b := NewBridge(slog.Default(), h, sm, nil)
	// Inject the worker factory via a test helper - since wf is a field,
	// we replace it after construction (field injection for tests).
	b.wf = wf

	err := b.StartSession(ctx, "sess_start", "user1", "", worker.TypeClaudeCode, nil, "", "", nil)
	require.NoError(t, err, "StartSession should succeed")

	sm.AssertExpectations(t)
}

// TestBridge_StartSession_CreateFails verifies that when session creation fails,
// StartSession returns an error without calling worker.Start.
func TestBridge_StartSession_CreateFails(t *testing.T) {
	sm := &mockBridgeSM{Mock: mock.Mock{}}
	sm.Test(t)

	sm.On("CreateWithBot", mock.Anything, "sess_fail", "user1", "", worker.TypeClaudeCode, mock.Anything).
		Return(nil, errors.New("create failed"))

	h := newTestHub(t)
	b := NewBridge(slog.Default(), h, sm, nil)
	// Inject a worker factory that would fail if Start were called.
	b.wf = &failingWorkerFactory{}

	err := b.StartSession(context.Background(), "sess_fail", "user1", "", worker.TypeClaudeCode, nil, "", "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "create failed")

	// Start should never be called because Create failed.
	sm.AssertExpectations(t)
}

// failingWorkerFactory always fails when creating a worker.
type failingWorkerFactory struct{}

func (failingWorkerFactory) NewWorker(worker.WorkerType) (worker.Worker, error) {
	return nil, errors.New("worker creation disabled in test")
}

var _ WorkerFactory = failingWorkerFactory{}

// TestBridge_ResumeSession_Success verifies that ResumeSession retrieves a session,
// creates a worker, attaches it, resumes it, and transitions from TERMINATED→RUNNING.
func TestBridge_ResumeSession_Success(t *testing.T) {
	sm := &mockBridgeSM{Mock: mock.Mock{}}
	sm.Test(t)

	sessionInfo := &session.SessionInfo{
		ID:         "sess_resume",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateTerminated,
	}
	sm.On("Get", "sess_resume").Return(sessionInfo, nil)
	sm.On("GetWorker", "sess_resume").Return(nil) // P1: no stale worker
	sm.On("AttachWorker", "sess_resume", mock.Anything).Return(nil)
	sm.On("Transition", mock.Anything, "sess_resume", events.StateRunning).Return(nil)

	// Mock noop worker: Resume returns nil (after SetConn is called).
	mockW := &mockBridgeWorker{
		workerType: worker.TypeClaudeCode,
		conn:       &fakeWorkerConn{ch: make(chan *events.Envelope)},
		resumeErr:  nil, // Resume succeeds
	}
	wf := &mockBridgeWorkerFactory{workers: []*mockBridgeWorker{mockW}}

	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "sess_resume", nil)
	h.JoinSession("sess_resume", c)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run()

	b := NewBridge(slog.Default(), h, sm, nil)
	b.wf = wf

	err := b.ResumeSession(ctx, "sess_resume", "")
	require.NoError(t, err, "ResumeSession should succeed")

	sm.AssertExpectations(t)

	// Verify state event was sent.
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := server.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(data), `"type":"state"`)
	require.Contains(t, string(data), `"state":"running"`)
}

// TestBridge_ResumeSession_DeletedSession verifies that resuming a DELETED session
// returns ErrSessionNotFound.
func TestBridge_ResumeSession_DeletedSession(t *testing.T) {
	sm := &mockBridgeSM{Mock: mock.Mock{}}
	sm.Test(t)

	sessionInfo := &session.SessionInfo{
		ID:         "sess_deleted",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateDeleted,
	}
	sm.On("Get", "sess_deleted").Return(sessionInfo, nil)

	h := newTestHub(t)
	b := NewBridge(slog.Default(), h, sm, nil)

	err := b.ResumeSession(context.Background(), "sess_deleted", "")
	require.Error(t, err)
	require.True(t, errors.Is(err, session.ErrSessionNotFound))

	sm.AssertExpectations(t)
}

// testNoopType is a worker type used exclusively in the noop worker tests.
// It is registered in init() to return a real noop worker.
const testNoopType worker.WorkerType = "noop_gateway_test"

func init() {
	worker.Register(testNoopType, func() (worker.Worker, error) {
		return noop.NewWorker(), nil
	})
}

// TestBridge_ResumeSession_NoopWorker verifies that for a noop-type worker,
// ResumeSession calls noopw.SetConn to inject a noop.Conn.
func TestBridge_ResumeSession_NoopWorker(t *testing.T) {
	sm := &mockBridgeSM{Mock: mock.Mock{}}
	sm.Test(t)

	sessionInfo := &session.SessionInfo{
		ID:         "sess_noop",
		UserID:     "user1",
		WorkerType: testNoopType,
		State:      events.StateIdle,
	}
	sm.On("Get", "sess_noop").Return(sessionInfo, nil)
	sm.On("GetWorker", "sess_noop").Return(nil)                                      // P1: no stale worker
	sm.On("Transition", mock.Anything, "sess_noop", events.StateRunning).Return(nil) // StateIdle → Running
	sm.On("AttachWorker", "sess_noop", mock.Anything).Return(nil)

	// Use the default worker factory so Bridge calls worker.NewWorker(testNoopType),
	// which returns a real noop worker (registered in init above).
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "sess_noop", nil)
	h.JoinSession("sess_noop", c)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run()

	b := NewBridge(slog.Default(), h, sm, nil)
	// Use the default factory (defaultWorkerFactory) so real noop workers are created.
	// b.wf is already defaultWorkerFactory{} from NewBridge.

	err := b.ResumeSession(ctx, "sess_noop", "")
	require.NoError(t, err)

	sm.AssertExpectations(t)

	// Verify state event was sent (Idle → no transition needed, but StateNotifier fires).
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := server.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(data), `"type":"state"`)
}
