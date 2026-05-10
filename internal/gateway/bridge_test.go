package gateway

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── Test NewBridge ───────────────────────────────────────────────────────────

func TestNewBridge(t *testing.T) {
	log := slog.Default()
	hub := newTestHub(t)
	sm := new(mockBridgeSM)
	b := NewBridge(BridgeDeps{Log: log, Hub: hub, SM: sm})

	require.NotNil(t, b)
	assert.Same(t, sm, b.sm)
	assert.Equal(t, hub, b.hub)
}

// ─── Test Bridge SetWorkerFactory ─────────────────────────────────────────────

func TestBridge_SetWorkerFactory(t *testing.T) {
	log := slog.Default()
	sm := new(mockBridgeSM)
	b := &Bridge{log: log, sm: sm, wf: defaultWorkerFactory{}}

	wf := &mockBridgeWorkerFactory{}
	b.SetWorkerFactory(wf)
	assert.Same(t, wf, b.wf)
}

// ─── Test Shutdown ────────────────────────────────────────────────────────────

func TestBridge_Shutdown(t *testing.T) {
	log := slog.Default()
	hub := newTestHub(t)
	sm := new(mockBridgeSM)
	b := NewBridge(BridgeDeps{Log: log, Hub: hub, SM: sm})

	b.Shutdown(context.Background())
	assert.True(t, b.closed.Load())

	// Idempotent.
	b.Shutdown(context.Background())
	assert.True(t, b.closed.Load())
}

func TestBridge_Shutdown_RejectNewSession(t *testing.T) {
	log := slog.Default()
	hub := newTestHub(t)
	sm := new(mockBridgeSM)
	b := NewBridge(BridgeDeps{Log: log, Hub: hub, SM: sm})

	b.Shutdown(context.Background())

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

	acc1 := b.getOrInitAccum("sess-1", "", time.Now())
	require.NotNil(t, acc1)

	acc2 := b.getOrInitAccum("sess-1", "", time.Now())
	assert.Same(t, acc1, acc2)

	acc3 := b.getOrInitAccum("sess-2", "", time.Now())
	assert.NotSame(t, acc1, acc3)
}

func TestGetOrInitAccum_LazyUpdate(t *testing.T) {
	t.Parallel()
	log := slog.Default()
	b := &Bridge{
		log:     log,
		sm:      new(mockBridgeSM),
		accum:   make(map[string]*sessionAccumulator),
		accumMu: sync.Mutex{},
	}

	// First call creates accumulator with empty workDir.
	acc := b.getOrInitAccum("sess-1", "", time.Now())
	require.NotNil(t, acc)
	assert.Equal(t, "", acc.WorkDir)
	assert.Equal(t, "", acc.GitBranch)

	// Second call with workDir lazily updates the existing accumulator.
	same := b.getOrInitAccum("sess-1", "/tmp/project", time.Now())
	assert.Same(t, acc, same)
	assert.Equal(t, "/tmp/project", acc.WorkDir)

	// Third call with different workDir does NOT overwrite (already set).
	b.getOrInitAccum("sess-1", "/other", time.Now())
	assert.Equal(t, "/tmp/project", acc.WorkDir)
}

func TestGetOrInitAccum_EmptyWorkDirNoOp(t *testing.T) {
	t.Parallel()
	log := slog.Default()
	b := &Bridge{
		log:     log,
		sm:      new(mockBridgeSM),
		accum:   make(map[string]*sessionAccumulator),
		accumMu: sync.Mutex{},
	}

	acc := b.getOrInitAccum("sess-1", "", time.Now())
	require.NotNil(t, acc)

	// Calling again with empty workDir should not change anything.
	same := b.getOrInitAccum("sess-1", "", time.Now())
	assert.Same(t, acc, same)
	assert.Equal(t, "", acc.WorkDir)
}

// ─── Test injectSessionStats ─────────────────────────────────────────────────

func TestInjectSessionStats(t *testing.T) {
	log := slog.Default()
	sm := new(mockBridgeSM)
	hub := newTestHub(t)
	b := NewBridge(BridgeDeps{Log: log, Hub: hub, SM: sm})

	acc := b.getOrInitAccum("sess-1", "", time.Now())
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
	b := NewBridge(BridgeDeps{Log: log, Hub: hub, SM: sm})

	acc := b.getOrInitAccum("sess-1", "", time.Now())
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

// ─── PBAC-015: injectAgentConfig BotID Resolution ─────────────────────────────

func writeAgentConfigFile(t *testing.T, dir, name, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
}

func TestBridge_InjectAgentConfig_BotIDResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(t *testing.T) string // returns config dir
		platform    string
		botID       string
		wantContain string
		wantEmpty   bool
	}{
		{
			name: "bot-level overrides platform",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeAgentConfigFile(t, dir, "webchat/SOUL.md", "Platform soul.")
				writeAgentConfigFile(t, dir, "webchat/my-bot/SOUL.md", "Bot soul.")
				return dir
			},
			platform:    "webchat",
			botID:       "my-bot",
			wantContain: "Bot soul.",
		},
		{
			name: "empty bot uses platform",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeAgentConfigFile(t, dir, "SOUL.md", "Global soul.")
				writeAgentConfigFile(t, dir, "webchat/SOUL.md", "Platform soul.")
				return dir
			},
			platform:    "webchat",
			botID:       "",
			wantContain: "Platform soul.",
		},
		{
			name: "falls to global",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeAgentConfigFile(t, dir, "SOUL.md", "Global soul.")
				return dir
			},
			platform:    "webchat",
			botID:       "some-bot",
			wantContain: "Global soul.",
		},
		{
			name: "disabled when empty dir",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			platform:  "webchat",
			botID:     "bot",
			wantEmpty: true,
		},
		{
			name: "path traversal rejected",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeAgentConfigFile(t, dir, "webchat/SOUL.md", "Platform soul.")
				return dir
			},
			platform:  "webchat",
			botID:     "../etc",
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := tt.setup(t)
			hub := newTestHub(t)
			sm := new(mockBridgeSM)
			b := NewBridge(BridgeDeps{
				Log:            slog.Default(),
				Hub:            hub,
				SM:             sm,
				AgentConfigDir: dir,
			})

			info := &worker.SessionInfo{}
			b.injectAgentConfig(info, tt.platform, tt.botID)

			if tt.wantEmpty {
				assert.Empty(t, info.SystemPrompt)
			} else {
				assert.Contains(t, info.SystemPrompt, tt.wantContain)
			}
		})
	}
}

// ─── Test injectGatewayContext ────────────────────────────────────────────────

func TestInjectGatewayContext(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		platform    string
		botID       string
		userID      string
		platformKey map[string]string
		sessionID   string
		workDir     string
		want        map[string]string
	}{
		{
			name:     "slack full fields",
			platform: "slack",
			botID:    "B123",
			userID:   "U456",
			platformKey: map[string]string{
				"channel_id": "C789",
				"thread_ts":  "1234.56",
				"team_id":    "T999",
			},
			sessionID: "sess-abc",
			workDir:   "/tmp/work",
			want: map[string]string{
				"GATEWAY_PLATFORM":   "slack",
				"GATEWAY_BOT_ID":     "B123",
				"GATEWAY_USER_ID":    "U456",
				"GATEWAY_CHANNEL_ID": "C789",
				"GATEWAY_THREAD_ID":  "1234.56",
				"GATEWAY_TEAM_ID":    "T999",
				"GATEWAY_SESSION_ID": "sess-abc",
				"GATEWAY_WORK_DIR":   "/tmp/work",
			},
		},
		{
			name:     "feishu maps chat_id to channel_id",
			platform: "feishu",
			botID:    "ou_bot123",
			userID:   "ou_user456",
			platformKey: map[string]string{
				"chat_id":    "oc_chat789",
				"message_id": "om_msg001",
			},
			sessionID: "sess-def",
			workDir:   "/tmp/feishu",
			want: map[string]string{
				"GATEWAY_PLATFORM":   "feishu",
				"GATEWAY_BOT_ID":     "ou_bot123",
				"GATEWAY_USER_ID":    "ou_user456",
				"GATEWAY_CHANNEL_ID": "oc_chat789",
				"GATEWAY_THREAD_ID":  "om_msg001",
				"GATEWAY_SESSION_ID": "sess-def",
				"GATEWAY_WORK_DIR":   "/tmp/feishu",
			},
		},
		{
			name:        "nil env gets initialized",
			env:         nil,
			platform:    "slack",
			botID:       "B1",
			userID:      "U1",
			platformKey: nil,
			sessionID:   "sess-nil",
			workDir:     "/tmp",
			want: map[string]string{
				"GATEWAY_PLATFORM":   "slack",
				"GATEWAY_BOT_ID":     "B1",
				"GATEWAY_USER_ID":    "U1",
				"GATEWAY_SESSION_ID": "sess-nil",
				"GATEWAY_WORK_DIR":   "/tmp",
			},
		},
		{
			name:        "empty fields omitted",
			env:         map[string]string{},
			platform:    "slack",
			botID:       "B1",
			userID:      "U1",
			platformKey: map[string]string{},
			sessionID:   "sess-empty",
			workDir:     "",
			want: map[string]string{
				"GATEWAY_PLATFORM":   "slack",
				"GATEWAY_BOT_ID":     "B1",
				"GATEWAY_USER_ID":    "U1",
				"GATEWAY_SESSION_ID": "sess-empty",
			},
		},
		{
			name:     "preserves existing env",
			env:      map[string]string{"EXISTING": "kept"},
			platform: "slack",
			botID:    "B1",
			userID:   "U1",
			platformKey: map[string]string{
				"channel_id": "C1",
			},
			sessionID: "sess-preserve",
			workDir:   "/tmp",
			want: map[string]string{
				"EXISTING":           "kept",
				"GATEWAY_PLATFORM":   "slack",
				"GATEWAY_BOT_ID":     "B1",
				"GATEWAY_USER_ID":    "U1",
				"GATEWAY_CHANNEL_ID": "C1",
				"GATEWAY_SESSION_ID": "sess-preserve",
				"GATEWAY_WORK_DIR":   "/tmp",
			},
		},
		{
			name:     "channel_id takes priority over chat_id",
			platform: "slack",
			botID:    "B1",
			userID:   "U1",
			platformKey: map[string]string{
				"channel_id": "C_PRIORITY",
				"chat_id":    "oc_lower",
			},
			sessionID: "sess-pri",
			workDir:   "/tmp",
			want: map[string]string{
				"GATEWAY_CHANNEL_ID": "C_PRIORITY",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Nil env test: function should initialize the map.
			if tt.env == nil {
				tt.env = injectGatewayContext(tt.env, tt.platform, tt.botID, tt.userID, tt.platformKey, tt.sessionID, tt.workDir)
				require.NotNil(t, tt.env, "env should be initialized")
				for k, v := range tt.want {
					assert.Equal(t, v, tt.env[k], "env[%q]", k)
				}
				return
			}

			tt.env = injectGatewayContext(tt.env, tt.platform, tt.botID, tt.userID, tt.platformKey, tt.sessionID, tt.workDir)

			for k, v := range tt.want {
				assert.Equal(t, v, tt.env[k], "env[%q]", k)
			}
			// Verify omitted fields are absent.
			if _, ok := tt.want["GATEWAY_WORK_DIR"]; !ok {
				_, exists := tt.env["GATEWAY_WORK_DIR"]
				assert.False(t, exists, "GATEWAY_WORK_DIR should not be set")
			}
			if _, ok := tt.want["GATEWAY_TEAM_ID"]; !ok {
				_, exists := tt.env["GATEWAY_TEAM_ID"]
				assert.False(t, exists, "GATEWAY_TEAM_ID should not be set")
			}
			if _, ok := tt.want["GATEWAY_CHANNEL_ID"]; !ok {
				_, exists := tt.env["GATEWAY_CHANNEL_ID"]
				assert.False(t, exists, "GATEWAY_CHANNEL_ID should not be set")
			}
			if _, ok := tt.want["GATEWAY_THREAD_ID"]; !ok {
				_, exists := tt.env["GATEWAY_THREAD_ID"]
				assert.False(t, exists, "GATEWAY_THREAD_ID should not be set")
			}
		})
	}
}

// ─── Test buildWorkerInfo MCP Injection ────────────────────────────────────────

func TestBuildWorkerInfo_MCPInjection(t *testing.T) {
	t.Parallel()

	mcpJSON := `{"mcpServers":{"test":{"command":"echo"}}}`

	tests := []struct {
		name          string
		mcpConfigJSON string
		platform      string
		wantMCP       string
		wantStrict    bool
	}{
		{
			name:          "cron platform always suppresses MCP",
			mcpConfigJSON: mcpJSON,
			platform:      "cron",
			wantMCP:       `{"mcpServers":{}}`,
			wantStrict:    true,
		},
		{
			name:          "configured MCP with non-cron platform",
			mcpConfigJSON: mcpJSON,
			platform:      "slack",
			wantMCP:       mcpJSON,
			wantStrict:    true,
		},
		{
			name:          "empty platform with configured MCP",
			mcpConfigJSON: mcpJSON,
			platform:      "",
			wantMCP:       mcpJSON,
			wantStrict:    true,
		},
		{
			name:          "no config and non-cron platform uses default discovery",
			mcpConfigJSON: "",
			platform:      "slack",
			wantMCP:       "",
			wantStrict:    false,
		},
		{
			name:          "cron platform wins over empty config",
			mcpConfigJSON: "",
			platform:      "cron",
			wantMCP:       `{"mcpServers":{}}`,
			wantStrict:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := slog.Default()
			hub := newTestHub(t)
			sm := new(mockBridgeSM)
			b := NewBridge(BridgeDeps{
				Log:           log,
				Hub:           hub,
				SM:            sm,
				MCPConfigJSON: tt.mcpConfigJSON,
			})

			si := &session.SessionInfo{
				Platform: tt.platform,
			}
			info := b.buildWorkerInfo("session-1", "user-1", "/tmp", si)
			assert.Equal(t, tt.wantMCP, info.MCPConfig, "MCPConfig mismatch")
			assert.Equal(t, tt.wantStrict, info.StrictMCPConfig, "StrictMCPConfig mismatch")
		})
	}
}
