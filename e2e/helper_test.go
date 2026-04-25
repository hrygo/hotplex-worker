// Package e2e provides end-to-end tests for the HotPlex Worker Gateway
// using the Go client SDK. It spins up a full gateway stack with simulated
// workers to validate the complete AEP v1 protocol flow for all worker types.
package e2e_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	client "github.com/hrygo/hotplex/client"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/gateway"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── Simulated Worker ──────────────────────────────────────────────────────

// simulatedConn produces realistic AEP events in response to input.
type simulatedConn struct {
	sessionID string
	userID    string
	recvCh    chan *events.Envelope
	closed    bool
}

func newSimulatedConn(sessionID, userID string) *simulatedConn {
	return &simulatedConn{
		sessionID: sessionID,
		userID:    userID,
		recvCh:    make(chan *events.Envelope, 64),
	}
}

func (c *simulatedConn) Send(_ context.Context, _ *events.Envelope) error {
	return nil
}

func (c *simulatedConn) Recv() <-chan *events.Envelope {
	return c.recvCh
}

func (c *simulatedConn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.recvCh)
	return nil
}

func (c *simulatedConn) UserID() string    { return c.userID }
func (c *simulatedConn) SessionID() string { return c.sessionID }

// emitEvents sends message.start → delta → end → done through recvCh.
func (c *simulatedConn) emitEvents(content string) {
	msgID := "msg_" + uuid.NewString()

	c.recvCh <- events.NewEnvelope(aep.NewID(), c.sessionID, 0, events.MessageStart, events.MessageStartData{
		ID:          msgID,
		Role:        "assistant",
		ContentType: "text",
	})

	chunks := splitContent(content, 20)
	for _, chunk := range chunks {
		c.recvCh <- events.NewEnvelope(aep.NewID(), c.sessionID, 0, events.MessageDelta, events.MessageDeltaData{
			MessageID: msgID,
			Content:   chunk,
		})
	}

	c.recvCh <- events.NewEnvelope(aep.NewID(), c.sessionID, 0, events.MessageEnd, events.MessageEndData{
		MessageID: msgID,
	})

	c.recvCh <- events.NewEnvelope(aep.NewID(), c.sessionID, 0, events.Done, events.DoneData{
		Success: true,
		Stats:   map[string]any{"input_tokens": 10, "output_tokens": len(content) / 4},
	})
}

func splitContent(s string, chunkSize int) []string {
	if len(s) <= chunkSize {
		return []string{s}
	}
	var chunks []string
	for i := 0; i < len(s); i += chunkSize {
		end := i + chunkSize
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[i:end])
	}
	return chunks
}

// simulatedWorker accepts input and emits realistic AEP events via a simulatedConn.
type simulatedWorker struct {
	mu         sync.Mutex
	workerType worker.WorkerType
	conn       *simulatedConn
	started    bool
	killed     bool
}

var _ worker.Worker = (*simulatedWorker)(nil)

func newSimulatedWorker(wt worker.WorkerType) *simulatedWorker {
	return &simulatedWorker{workerType: wt}
}

func (w *simulatedWorker) Type() worker.WorkerType { return w.workerType }
func (w *simulatedWorker) SupportsResume() bool    { return true }
func (w *simulatedWorker) SupportsStreaming() bool { return true }
func (w *simulatedWorker) SupportsTools() bool     { return true }
func (w *simulatedWorker) EnvWhitelist() []string  { return nil }
func (w *simulatedWorker) SessionStoreDir() string { return "" }
func (w *simulatedWorker) MaxTurns() int           { return 0 }
func (w *simulatedWorker) Modalities() []string    { return []string{"text"} }

func (w *simulatedWorker) Start(_ context.Context, info worker.SessionInfo) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn = newSimulatedConn(info.SessionID, info.UserID)
	w.started = true
	return nil
}

func (w *simulatedWorker) Input(_ context.Context, content string, _ map[string]any) error {
	w.mu.Lock()
	conn := w.conn
	w.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("worker not started")
	}
	go conn.emitEvents(content)
	return nil
}

func (w *simulatedWorker) Resume(_ context.Context, info worker.SessionInfo) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn = newSimulatedConn(info.SessionID, info.UserID)
	w.started = true
	return nil
}

func (w *simulatedWorker) Terminate(_ context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn != nil {
		_ = w.conn.Close()
	}
	return nil
}

func (w *simulatedWorker) Kill() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.killed = true
	if w.conn != nil {
		_ = w.conn.Close()
	}
	return nil
}

func (w *simulatedWorker) Wait() (int, error) {
	return 0, io.EOF
}

func (w *simulatedWorker) Conn() worker.SessionConn {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn
}

func (w *simulatedWorker) Health() worker.WorkerHealth {
	return worker.WorkerHealth{
		Type:    w.workerType,
		Healthy: true,
		Running: true,
		Uptime:  "1s",
	}
}

func (w *simulatedWorker) LastIO() time.Time {
	return time.Now()
}

func (w *simulatedWorker) ResetContext(_ context.Context) error {
	return nil
}

// testWorkerFactory creates simulated workers for tests.
type testWorkerFactory struct{}

func (testWorkerFactory) NewWorker(t worker.WorkerType) (worker.Worker, error) {
	return newSimulatedWorker(t), nil
}

// ─── Mock Store ─────────────────────────────────────────────────────────────

type mockStore struct {
	mock.Mock
}

func (m *mockStore) Upsert(ctx context.Context, info *session.SessionInfo) error {
	args := m.Called(ctx, info)
	return args.Error(0)
}

func (m *mockStore) Get(ctx context.Context, id string) (*session.SessionInfo, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*session.SessionInfo), args.Error(1)
}

func (m *mockStore) List(ctx context.Context, userID, platform string, limit, offset int) ([]*session.SessionInfo, error) {
	args := m.Called(ctx, userID, platform, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*session.SessionInfo), args.Error(1)
}

func (m *mockStore) GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error) {
	args := m.Called(ctx, now)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockStore) GetExpiredIdle(ctx context.Context, now time.Time) ([]string, error) {
	args := m.Called(ctx, now)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockStore) DeleteTerminated(ctx context.Context, cutoff time.Time) error {
	args := m.Called(ctx, cutoff)
	return args.Error(0)
}

func (m *mockStore) DeletePhysical(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockStore) Close() error {
	args := m.Called()
	return args.Error(0)
}

// ─── Test Gateway Setup ─────────────────────────────────────────────────────

type testGateway struct {
	server   *httptest.Server
	hub      *gateway.Hub
	sm       *session.Manager
	bridge   *gateway.Bridge
	cfg      *config.Config
	store    *mockStore
	log      *slog.Logger
	cancel   context.CancelFunc
	jwtKey   *ecdsa.PrivateKey
	jwtValid *security.JWTValidator
}

func setupTestGateway(t *testing.T) *testGateway {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	log := slog.Default()

	cfg := config.Default()
	cfg.Security.APIKeys = nil // dev mode: allow all
	cfg.Security.AllowedOrigins = []string{"*"}
	cfg.Gateway.BroadcastQueueSize = 64
	cfg.Worker.DefaultWorkDir = "/tmp"
	cfg.Pool.MaxSize = 20
	cfg.Pool.MaxIdlePerUser = 10
	cfg.Pool.MaxMemoryPerUser = 0

	// Generate ES256 key for JWT testing.
	jwtKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	jwtValidator := security.NewJWTValidator(jwtKey, "")

	store := new(mockStore)
	store.Test(t)

	// Allow any Upsert (session creation).
	store.On("Upsert", mock.Anything, mock.AnythingOfType("*session.SessionInfo")).Return(nil)
	store.On("Close").Return(nil)
	store.On("GetExpiredMaxLifetime", mock.Anything, mock.AnythingOfType("time.Time")).Return([]string{}, nil)
	store.On("GetExpiredIdle", mock.Anything, mock.AnythingOfType("time.Time")).Return([]string{}, nil)
	store.On("DeleteTerminated", mock.Anything, mock.AnythingOfType("time.Time")).Return(nil)
	store.On("List", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string"), mock.AnythingOfType("int"), mock.AnythingOfType("int")).Return([]*session.SessionInfo{}, nil)
	// Get falls back to store when session is not in Manager's in-memory map.
	// Return not-found for all store lookups (Manager holds sessions in memory after Create).
	store.On("Get", mock.Anything, mock.AnythingOfType("string")).Return(nil, session.ErrSessionNotFound)

	sm, err := session.NewManager(ctx, log, cfg, nil, store, nil)
	require.NoError(t, err)

	hub := gateway.NewHub(log, config.NewConfigStore(cfg, nil))

	sm.StateNotifier = func(ctx context.Context, sessionID string, state events.SessionState, message string) {
		env := events.NewEnvelope(aep.NewID(), sessionID, hub.NextSeq(sessionID), events.State, events.StateData{
			State:   state,
			Message: message,
		})
		_ = hub.SendToSession(ctx, env)
	}

	handler := gateway.NewHandler(log, hub, sm, jwtValidator)
	bridge := gateway.NewBridge(log, hub, sm, nil)
	bridge.SetWorkerFactory(testWorkerFactory{})

	auth := security.NewAuthenticator(&cfg.Security, jwtValidator)

	mux := http.NewServeMux()
	mux.Handle("/ws", hub.HandleHTTP(auth, sm, handler, bridge))

	server := httptest.NewServer(mux)

	tg := &testGateway{
		server:   server,
		hub:      hub,
		sm:       sm,
		bridge:   bridge,
		cfg:      cfg,
		store:    store,
		log:      log,
		cancel:   cancel,
		jwtKey:   jwtKey,
		jwtValid: jwtValidator,
	}

	t.Cleanup(func() {
		cancel()
		_ = hub.Shutdown(context.Background())
		_ = sm.Close()
		server.Close()
	})

	return tg
}

func (tg *testGateway) wsURL() string {
	return "ws" + strings.TrimPrefix(tg.server.URL, "http") + "/ws"
}

func (tg *testGateway) generateToken(subject string, ttl time.Duration) string {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":    "hotplex",
		"sub":    subject,
		"aud":    "gateway",
		"exp":    now.Add(ttl).Unix(),
		"iat":    now.Unix(),
		"nbf":    now.Unix(),
		"jti":    uuid.NewString(),
		"scopes": []string{"session:write"},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	s, err := token.SignedString(tg.jwtKey)
	if err != nil {
		panic(err)
	}
	return s
}

func connectClient(t *testing.T, tg *testGateway, workerType string) *client.Client {
	t.Helper()
	token := tg.generateToken("test-user", 5*time.Minute)
	c, err := client.New(context.Background(),
		client.URL(tg.wsURL()),
		client.WorkerType(workerType),
		client.AuthToken(token),
		client.APIKey("test-key"),
	)
	require.NoError(t, err)
	return c
}

func collectEvents(t *testing.T, ch <-chan client.Event, timeout time.Duration) []client.Event {
	t.Helper()
	var evts []client.Event
	deadline := time.After(timeout)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return evts
			}
			evts = append(evts, evt)
			if evt.Type == client.EventDone || evt.Type == client.EventError {
				return evts
			}
		case <-deadline:
			return evts
		}
	}
}

func eventTypes(evts []client.Event) []string {
	types := make([]string, len(evts))
	for i, evt := range evts {
		types[i] = evt.Type
	}
	return types
}

func hasEventType(evts []client.Event, typ string) bool {
	for _, evt := range evts {
		if evt.Type == typ {
			return true
		}
	}
	return false
}

func findEvent(evts []client.Event, typ string) *client.Event {
	for i := range evts {
		if evts[i].Type == typ {
			return &evts[i]
		}
	}
	return nil
}

// ─── Worker types for table-driven tests ────────────────────────────────────

var allWorkerTypes = []struct {
	name       string
	workerType string
}{
	{"claude_code", string(worker.TypeClaudeCode)},
	{"opencode_server", string(worker.TypeOpenCodeSrv)},
	{"acpx", string(worker.TypeACPX)},
	{"pi_mono", string(worker.TypePimon)},
}
