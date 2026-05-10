package cron

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
)

// mockBridge implements BridgeStarter for testing.
type mockBridge struct {
	startErr error
}

func (m *mockBridge) StartSession(_ context.Context, _, _, _ string, _ worker.WorkerType, _ []string, _, _ string, _ map[string]string, _ string) error {
	return m.startErr
}

// mockSessionStateChecker implements SessionStateChecker for testing.
type mockSessionStateChecker struct {
	sessions       map[string]*session.SessionInfo
	workers        map[string]worker.Worker
	defaultWorker  worker.Worker
	defaultSession *session.SessionInfo
}

func (m *mockSessionStateChecker) Get(_ context.Context, id string) (*session.SessionInfo, error) {
	if si, ok := m.sessions[id]; ok {
		return si, nil
	}
	if m.defaultSession != nil {
		return m.defaultSession, nil
	}
	return nil, errTestNotFound
}

func (m *mockSessionStateChecker) GetWorker(id string) worker.Worker {
	if w, ok := m.workers[id]; ok {
		return w
	}
	return m.defaultWorker
}

// mockWorker implements worker.Worker for testing with minimal stubs.
type mockWorker struct {
	inputErr error
}

func (m *mockWorker) Type() worker.WorkerType                             { return worker.TypeClaudeCode }
func (m *mockWorker) SupportsResume() bool                                { return false }
func (m *mockWorker) SupportsStreaming() bool                             { return true }
func (m *mockWorker) SupportsTools() bool                                 { return true }
func (m *mockWorker) EnvBlocklist() []string                              { return nil }
func (m *mockWorker) SessionStoreDir() string                             { return "" }
func (m *mockWorker) MaxTurns() int                                       { return 0 }
func (m *mockWorker) Modalities() []string                                { return []string{"text"} }
func (m *mockWorker) Start(_ context.Context, _ worker.SessionInfo) error { return nil }
func (m *mockWorker) Input(_ context.Context, _ string, _ map[string]any) error {
	return m.inputErr
}
func (m *mockWorker) Resume(_ context.Context, _ worker.SessionInfo) error { return nil }
func (m *mockWorker) Terminate(_ context.Context) error                    { return nil }
func (m *mockWorker) Kill() error                                          { return nil }
func (m *mockWorker) Wait() (int, error)                                   { return 0, nil }
func (m *mockWorker) Conn() worker.SessionConn                             { return nil }
func (m *mockWorker) Health() worker.WorkerHealth                          { return worker.WorkerHealth{} }
func (m *mockWorker) LastIO() time.Time                                    { return time.Time{} }
func (m *mockWorker) ResetContext(_ context.Context) error                 { return nil }

var errTestNotFound = context.DeadlineExceeded

func testJob() *CronJob {
	return &CronJob{
		ID:      "cron_test",
		Name:    "test",
		OwnerID: "user1",
		BotID:   "bot1",
		WorkDir: "/tmp",
		Payload: CronPayload{Kind: PayloadAgentTurn, Message: "hello"},
	}
}

func TestExecutor_Execute_StartFails(t *testing.T) {
	t.Parallel()

	bridge := &mockBridge{startErr: errTestNotFound}
	sm := &mockSessionStateChecker{workers: map[string]worker.Worker{}}

	e := NewExecutor(slog.Default(), bridge, sm)
	_, err := e.Execute(context.Background(), testJob(), 5*time.Minute)
	require.Error(t, err)
	require.Contains(t, err.Error(), "start cron session")
}

func TestExecutor_Execute_WorkerNotFound(t *testing.T) {
	t.Parallel()

	bridge := &mockBridge{}
	sm := &mockSessionStateChecker{workers: map[string]worker.Worker{}}

	e := NewExecutor(slog.Default(), bridge, sm)
	_, err := e.Execute(context.Background(), testJob(), 5*time.Minute)
	require.Error(t, err)
	require.Contains(t, err.Error(), "worker not found")
}

func TestExecutor_Execute_InputFails(t *testing.T) {
	t.Parallel()

	bridge := &mockBridge{}
	sm := &mockSessionStateChecker{
		defaultWorker: &mockWorker{inputErr: errTestNotFound},
	}

	e := NewExecutor(slog.Default(), bridge, sm)
	_, err := e.Execute(context.Background(), testJob(), 5*time.Minute)
	require.Error(t, err)
	require.Contains(t, err.Error(), "input prompt")
}

func TestExecutor_Execute_TimeoutWaiting(t *testing.T) {
	t.Parallel()

	bridge := &mockBridge{}
	sm := &mockSessionStateChecker{
		defaultSession: &session.SessionInfo{State: "running"},
		defaultWorker:  &mockWorker{},
	}

	e := NewExecutor(slog.Default(), bridge, sm)
	_, err := e.Execute(context.Background(), testJob(), 100*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeout")
}

func TestExecutor_Execute_Success(t *testing.T) {
	t.Parallel()

	bridge := &mockBridge{}
	// Session already completed (terminated) — waitForCompletion returns immediately.
	sm := &mockSessionStateChecker{
		defaultSession: &session.SessionInfo{State: "terminated"},
		defaultWorker:  &mockWorker{},
	}

	e := NewExecutor(slog.Default(), bridge, sm)

	gotKey, err := e.Execute(context.Background(), testJob(), 5*time.Second)
	require.NoError(t, err)
	require.NotEmpty(t, gotKey)
}
