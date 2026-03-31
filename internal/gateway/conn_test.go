package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"hotplex-worker/internal/aep"
	"hotplex-worker/internal/worker"
	"hotplex-worker/pkg/events"
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
					"allowed_tools":  []any{"Read", "Bash"},
					"max_turns":     50.0,
					"work_dir":      "/tmp/project",
				}
			}),
			wantNil: true,
		},
		{
			name: "missing version",
			data: map[string]any{"worker_type": "claude-code"},
			wantNil:  false,
			wantCode: events.ErrCodeInvalidMessage,
		},
		{
			name: "wrong version",
			data: map[string]any{"version": "aep/v0", "worker_type": "claude-code"},
			wantNil:  false,
			wantCode: events.ErrCodeVersionMismatch,
		},
		{
			name: "missing worker_type",
			data: map[string]any{"version": events.Version},
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
		{6, 60 * time.Second, 64 * time.Second}, // capped at 60s
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
				Init,
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
		Event:     events.Event{Type: Init, Data: data},
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
