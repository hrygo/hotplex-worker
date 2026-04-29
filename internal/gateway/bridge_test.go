package gateway

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── Test NewBridge ───────────────────────────────────────────────────────────

func TestNewBridge(t *testing.T) {
	log := slog.Default()
	hub := newTestHub(t)
	sm := new(mockBridgeSM)
	b := NewBridge(log, hub, sm)

	require.NotNil(t, b)
	assert.Same(t, sm, b.sm)
	assert.Equal(t, hub, b.hub)
}

// ─── Test Bridge setters ─────────────────────────────────────────────────────

func TestBridge_SetWorkerFactory(t *testing.T) {
	log := slog.Default()
	sm := new(mockBridgeSM)
	b := &Bridge{log: log, sm: sm, wf: defaultWorkerFactory{}}

	wf := &mockBridgeWorkerFactory{}
	b.SetWorkerFactory(wf)
	assert.Same(t, wf, b.wf)
}

func TestBridge_SetRetryController(t *testing.T) {
	log := slog.Default()
	sm := new(mockBridgeSM)
	b := &Bridge{log: log, sm: sm}

	cfg := config.AutoRetryConfig{Enabled: false}
	ctrl := NewLLMRetryController(cfg, log)
	b.SetRetryController(ctrl)

	assert.Same(t, ctrl, b.retryCtrl)
}

func TestBridge_SetConvStore(t *testing.T) {
	log := slog.Default()
	sm := new(mockBridgeSM)
	b := &Bridge{log: log, sm: sm}

	fcs := &fakeConvStoreForBridge{}
	b.SetConvStore(fcs)

	assert.Same(t, fcs, b.convStore)
}

// fakeConvStoreForBridge implements session.ConversationStore.
type fakeConvStoreForBridge struct{}

func (*fakeConvStoreForBridge) Append(ctx context.Context, rec *session.ConversationRecord) error {
	return nil
}
func (*fakeConvStoreForBridge) GetBySession(ctx context.Context, sessionID string, limit, offset int) ([]*session.ConversationRecord, error) {
	return nil, nil
}
func (*fakeConvStoreForBridge) DeleteBySession(ctx context.Context, sessionID string) error {
	return nil
}
func (*fakeConvStoreForBridge) DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	return 0, nil
}
func (*fakeConvStoreForBridge) SessionStats(ctx context.Context, sessionID string) (*session.ConversationSessionStats, error) {
	return nil, nil
}
func (*fakeConvStoreForBridge) GetBySessionBefore(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]*session.ConversationRecord, error) {
	return nil, nil
}
func (*fakeConvStoreForBridge) Close() error { return nil }

func TestBridge_SetAgentConfigDir(t *testing.T) {
	log := slog.Default()
	sm := new(mockBridgeSM)
	b := &Bridge{log: log, sm: sm}

	b.SetAgentConfigDir("/tmp/test-config")
	assert.Equal(t, "/tmp/test-config", b.agentConfigDir)

	b.SetAgentConfigDir("")
	assert.Equal(t, "", b.agentConfigDir)
}

func TestBridge_SetTurnTimeout(t *testing.T) {
	log := slog.Default()
	sm := new(mockBridgeSM)
	b := &Bridge{log: log, sm: sm}

	b.SetTurnTimeout(5 * time.Minute)
	assert.Equal(t, 5*time.Minute, b.turnTimeout)

	b.SetTurnTimeout(0)
	assert.Equal(t, time.Duration(0), b.turnTimeout)
}

// ─── Test Shutdown ────────────────────────────────────────────────────────────

func TestBridge_Shutdown(t *testing.T) {
	log := slog.Default()
	hub := newTestHub(t)
	sm := new(mockBridgeSM)
	b := NewBridge(log, hub, sm)

	b.Shutdown()
	assert.True(t, b.closed.Load())

	// Idempotent.
	b.Shutdown()
	assert.True(t, b.closed.Load())
}

func TestBridge_Shutdown_RejectNewSession(t *testing.T) {
	log := slog.Default()
	hub := newTestHub(t)
	sm := new(mockBridgeSM)
	b := NewBridge(log, hub, sm)

	b.Shutdown()

	// After shutdown, StartSession should be rejected.
	err := b.StartSession(context.Background(), "sess-closed", "u", "b",
		worker.TypeClaudeCode, nil, "", "", nil, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shutdown")
}

// ─── Test buildNotifyEnvelope ────────────────────────────────────────────────

func TestBuildNotifyEnvelope(t *testing.T) {
	env := buildNotifyEnvelope("sess-1", "hello world", 42)

	require.NotNil(t, env)
	assert.NotEmpty(t, env.ID)
	assert.Equal(t, "sess-1", env.SessionID)
	assert.Equal(t, int64(42), env.Seq)
	assert.Equal(t, events.Message, env.Event.Type)

	data, ok := env.Event.Data.(map[string]any)
	require.True(t, ok, "expected map[string]any, got %T", env.Event.Data)
	assert.Equal(t, "hello world", data["content"])
}

func TestBuildNotifyEnvelope_EmptyMessage(t *testing.T) {
	env := buildNotifyEnvelope("sid", "", 1)
	assert.NotNil(t, env)
	assert.Equal(t, "sid", env.SessionID)
}

// ─── Test sanitizeLastInput ──────────────────────────────────────────────────

func TestSanitizeLastInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"plain text preserved", "Hello, how are you?", "Hello, how are you?"},
		{"control command /gc removed", "/gc", ""},
		{"control command /reset removed", "/reset", ""},
		{"control command /park removed", "/park", ""},
		{"control command /new removed", "/new", ""},
		{"dollar-prefix $gc removed", "$gc", ""},
		{"dollar-prefix $reset removed", "$reset", ""},
		{"mixed control line removed", "Hello\n/gc\nWorld", "Hello\nWorld"},
		{"all control lines removed", "/reset\n/park\n/new", ""},
		{"multiline user input preserved", "Here is my code:\nfunc main() {}\nPlease review", "Here is my code:\nfunc main() {}\nPlease review"},
		{"mixed control and user content", "$gc\nActual question: how do I fix this?", "Actual question: how do I fix this?"},
		{"leading control removed", "/gc\nImportant message", "Important message"},
		{"trailing control removed", "Important message\n/gc", "Important message"},
		{"whitespace line preserved (not a control cmd)", "/gc\n   \n/park", "   "},
		{"cd command removed", "/cd /tmp/project", ""},
		{"dollar cd removed", "$cd /tmp", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeLastInput(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ─── Test sessionAccumulator helpers (not covered by session_stats_test.go) ───

func TestSessionAccumulator_ComputePerTurnDeltas(t *testing.T) {
	acc := &sessionAccumulator{
		StartedAt:     time.Now(),
		TotalInput:    100,
		TotalOutput:   50,
		TotalCostUSD:  0.01,
		PrevTotalIn:   0,
		PrevTotalOut:  0,
		PrevTotalCost: 0,
	}
	acc.computePerTurnDeltas()

	assert.Equal(t, int64(100), acc.PerTurnInput)
	assert.Equal(t, int64(50), acc.PerTurnOutput)
	assert.Equal(t, 0.01, acc.PerTurnCost)

	// Second call without resetPerTurn: deltas remain the same (no baseline advance).
	acc.computePerTurnDeltas()
	assert.Equal(t, int64(100), acc.PerTurnInput)
}

func TestSessionAccumulator_ResetPerTurn(t *testing.T) {
	acc := &sessionAccumulator{
		StartedAt:     time.Now(),
		PrevTotalIn:   100,
		PrevTotalOut:  50,
		PrevTotalCost: 0.01,
		ToolNames:     map[string]int{"Read": 2},
		ToolCallCount: 5,
		PerTurnInput:  100,
		PerTurnOutput: 50,
		PerTurnCost:   0.01,
	}

	acc.resetPerTurn()

	assert.Equal(t, int64(0), acc.PerTurnInput)
	assert.Equal(t, int64(0), acc.PerTurnOutput)
	assert.Equal(t, 0.0, acc.PerTurnCost)
	assert.Nil(t, acc.ToolNames)
	assert.Equal(t, 0, acc.ToolCallCount)
}

func TestSessionAccumulator_NegativeDeltasClamped(t *testing.T) {
	// If TotalInput drops below PrevTotalIn (shouldn't happen but guard against it).
	acc := &sessionAccumulator{
		TotalInput:  10,
		PrevTotalIn: 100,
	}
	acc.computePerTurnDeltas()
	assert.Equal(t, int64(0), acc.PerTurnInput)
}

func TestComputeContextPct_ZeroWindow(t *testing.T) {
	acc := &sessionAccumulator{ContextWindow: 0, TotalInput: 50000}
	assert.Equal(t, 0.0, acc.computeContextPct())
}

// ─── Test getOrInitAccum ─────────────────────────────────────────────────────

func TestGetOrInitAccum(t *testing.T) {
	log := slog.Default()
	sm := new(mockBridgeSM)
	b := &Bridge{
		log:     log,
		sm:      sm,
		accum:   make(map[string]*sessionAccumulator),
		accumMu: sync.Mutex{},
	}

	acc1 := b.getOrInitAccum("sess-1")
	require.NotNil(t, acc1)

	acc2 := b.getOrInitAccum("sess-1")
	assert.Same(t, acc1, acc2)

	acc3 := b.getOrInitAccum("sess-2")
	assert.NotSame(t, acc1, acc3)
}

// ─── Test injectSessionStats ─────────────────────────────────────────────────

func TestInjectSessionStats(t *testing.T) {
	log := slog.Default()
	sm := new(mockBridgeSM)
	hub := newTestHub(t)
	b := NewBridge(log, hub, sm)

	acc := b.getOrInitAccum("sess-1")
	acc.ToolCallCount = 4

	env := &events.Envelope{
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{Success: true},
		},
	}

	b.injectSessionStats(env, acc)

	dd, ok := env.Event.Data.(events.DoneData)
	require.True(t, ok)
	require.NotNil(t, dd.Stats)
	_, ok = dd.Stats["_session"]
	assert.True(t, ok)
}

func TestInjectSessionStats_NonDoneData(t *testing.T) {
	log := slog.Default()
	sm := new(mockBridgeSM)
	hub := newTestHub(t)
	b := NewBridge(log, hub, sm)

	acc := b.getOrInitAccum("sess-1")
	env := &events.Envelope{
		Event: events.Event{
			Type: events.Message,
			Data: "hello",
		},
	}

	// Should be a no-op — no panic, data unchanged.
	b.injectSessionStats(env, acc)
	assert.Equal(t, "hello", env.Event.Data)
}
