package claudecode

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/worker"
)

func hasClaudeBinary() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

func TestClaudeCodeWorker_Capabilities(t *testing.T) {
	t.Parallel()
	w := New()

	require.Equal(t, worker.TypeClaudeCode, w.Type())
	require.True(t, w.SupportsResume())
	require.True(t, w.SupportsStreaming())
	require.True(t, w.SupportsTools())
	require.NotNil(t, w.EnvWhitelist())
	require.Equal(t, ".claude/projects", w.SessionStoreDir())
	require.Zero(t, w.MaxTurns())
	require.Equal(t, []string{"text", "code", "image"}, w.Modalities())
}

func TestClaudeCodeWorker_EnvWhitelist(t *testing.T) {
	t.Parallel()
	w := New()

	wl := w.EnvWhitelist()
	require.Contains(t, wl, "CLAUDE_API_KEY")
	require.Contains(t, wl, "CLAUDE_MODEL")
	require.Contains(t, wl, "CLAUDE_BASE_URL")
	require.Contains(t, wl, "HOME")
	require.Contains(t, wl, "PATH")
}

func TestClaudeCodeWorker_ConnBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	require.Nil(t, w.Conn())
}

func TestClaudeCodeWorker_HealthBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()

	h := w.Health()
	require.Equal(t, worker.TypeClaudeCode, h.Type)
	require.False(t, h.Running)
	require.True(t, h.Healthy)
	require.Empty(t, h.SessionID)
}

func TestClaudeCodeWorker_LastIOBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	require.True(t, w.LastIO().IsZero())
}

func TestClaudeCodeWorker_TerminateWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	err := w.Terminate(ctx)
	require.NoError(t, err)
}

func TestClaudeCodeWorker_KillWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	err := w.Kill()
	require.NoError(t, err)
}

func TestClaudeCodeWorker_WaitWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	_, err := w.Wait()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")
}

func TestClaudeCodeWorker_Input_WithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()
	err := w.Input(ctx, "hello", nil)
	require.Error(t, err)
}

func TestClaudeCodeWorker_Start_WithBinary(t *testing.T) {
	if !hasClaudeBinary() {
		t.Skip("claude binary not found, skipping integration test")
	}

	w := New()
	ctx := context.Background()
	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	err := w.Start(ctx, session)
	require.NoError(t, err)

	conn := w.Conn()
	require.NotNil(t, conn)
	require.Equal(t, "test-session", conn.SessionID())
	require.Equal(t, "test-user", conn.UserID())

	h := w.Health()
	require.Equal(t, worker.TypeClaudeCode, h.Type)
	require.True(t, h.Running)

	_ = w.Kill()
}

func TestClaudeCodeWorker_Resume_WithBinary(t *testing.T) {
	if !hasClaudeBinary() {
		t.Skip("claude binary not found, skipping integration test")
	}

	w := New()
	ctx := context.Background()
	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	err := w.Resume(ctx, session)
	require.NoError(t, err)

	conn := w.Conn()
	require.NotNil(t, conn)

	_ = w.Kill()
}

func TestClaudeCodeWorker_DoubleStart(t *testing.T) {
	if !hasClaudeBinary() {
		t.Skip("claude binary not found, skipping integration test")
	}

	w := New()
	ctx := context.Background()
	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	_ = w.Start(ctx, session)
	err := w.Start(ctx, session)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already started")

	_ = w.Kill()
}

func TestBuildCLIArgs_AllOptions(t *testing.T) {
	t.Parallel()

	w := New()
	session := worker.SessionInfo{
		SessionID:              "test-session",
		UserID:                 "test-user",
		ProjectDir:             "/tmp",
		AllowedModels:          []string{"claude-sonnet-4-6"},
		AllowedTools:           []string{"Read", "Write", "Bash"},
		DisallowedTools:        []string{"WebSearch", "Edit"},
		PermissionMode:         "plan",
		SkipPermissions:        false,
		SystemPrompt:           "You are a helpful assistant.",
		SystemPromptReplace:    "",
		MCPConfig:              "/path/to/mcp.json",
		StrictMCPConfig:        true,
		MaxTurns:               10,
		Bare:                   true,
		AllowedDirs:            []string{"/extra/dir"},
		MaxBudgetUSD:           0.05,
		JSONSchema:             "/schemas/output.json",
		IncludeHookEvents:      true,
		IncludePartialMessages: true,
	}

	args := w.buildCLIArgs(session, false)

	require.Contains(t, args, "--print")
	require.Contains(t, args, "--verbose")
	require.Contains(t, args, "--output-format", "stream-json")
	require.Contains(t, args, "--input-format", "stream-json")
	// resume=false → --session-id
	require.Contains(t, args, "--session-id", "test-session")
	require.NotContains(t, args, "--resume")
	require.Contains(t, args, "--permission-mode", "plan")
	require.Contains(t, args, "--disallowed-tools", "WebSearch,Edit")
	require.Contains(t, args, "--model", "claude-sonnet-4-6")
	require.Contains(t, args, "--allowed-tools", "Read,Write,Bash")
	require.Contains(t, args, "--append-system-prompt", "You are a helpful assistant.")
	require.Contains(t, args, "--mcp-config", "/path/to/mcp.json")
	require.Contains(t, args, "--strict-mcp-config")
	require.Contains(t, args, "--max-turns", "10")
	require.Contains(t, args, "--bare")
	require.Contains(t, args, "--add-dir", "/extra/dir")
	require.Contains(t, args, "--max-budget-usd", "0.050000")
	require.Contains(t, args, "--json-schema", "/schemas/output.json")
	require.Contains(t, args, "--include-hook-events")
	require.Contains(t, args, "--include-partial-messages")
	require.Contains(t, args, "--dangerously-skip-permissions")
	require.NotContains(t, args, "--system-prompt") // replace mode not set
}

func TestBuildCLIArgs_SystemPromptReplace(t *testing.T) {
	t.Parallel()

	w := New()
	session := worker.SessionInfo{
		SessionID:           "test-session",
		UserID:              "test-user",
		ProjectDir:          "/tmp",
		SystemPrompt:        "old prompt",
		SystemPromptReplace: "completely new system prompt",
	}

	args := w.buildCLIArgs(session, false)
	require.Contains(t, args, "--system-prompt", "completely new system prompt")
	require.NotContains(t, args, "--append-system-prompt")
}

func TestBuildCLIArgs_Resume(t *testing.T) {
	t.Parallel()

	w := New()
	session := worker.SessionInfo{
		SessionID:  "resume-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	args := w.buildCLIArgs(session, true)
	// resume=true → --resume <session-id>
	require.Contains(t, args, "--resume")
	require.Contains(t, args, "resume-session")
	require.NotContains(t, args, "--session-id")
}

// TestBuildCLIArgs_MaxTurns, TestBuildCLIArgs_MCPConfig follow below.

func TestBuildCLIArgs_MaxTurns(t *testing.T) {
	t.Parallel()

	w := New()
	session := worker.SessionInfo{
		SessionID:  "max-turns-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
		MaxTurns:   5,
	}

	args := w.buildCLIArgs(session, false)
	require.Contains(t, args, "--max-turns", "5")
}

func TestBuildCLIArgs_MCPConfig(t *testing.T) {
	t.Parallel()

	w := New()
	session := worker.SessionInfo{
		SessionID:       "mcp-session",
		UserID:          "test-user",
		ProjectDir:      "/tmp",
		MCPConfig:       "/etc/mcp.json",
		StrictMCPConfig: true,
	}

	args := w.buildCLIArgs(session, false)
	require.Contains(t, args, "--mcp-config", "/etc/mcp.json")
	require.Contains(t, args, "--strict-mcp-config")
}

// TestBuildCLIArgs_Minimal follows below.

func TestBuildCLIArgs_SkipPermissions(t *testing.T) {
	t.Parallel()

	w := New()
	session := worker.SessionInfo{
		SessionID:       "skip-perm-session",
		UserID:          "test-user",
		ProjectDir:      "/tmp",
		SkipPermissions: true,
	}

	args := w.buildCLIArgs(session, false)
	require.Contains(t, args, "--dangerously-skip-permissions")
	require.NotContains(t, args, "--permission-mode")
}

func TestBuildCLIArgs_Minimal(t *testing.T) {
	t.Parallel()

	w := New()
	session := worker.SessionInfo{
		SessionID:  "minimal-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	args := w.buildCLIArgs(session, false)
	// resume=false → --session-id minimal-session, so 9 tokens total:
	// --print --verbose --output-format stream-json --input-format stream-json
	// --dangerously-skip-permissions --session-id minimal-session
	require.Len(t, args, 9)
	require.Contains(t, args, "--print")
	require.Contains(t, args, "--verbose")
	require.Contains(t, args, "--session-id", "minimal-session")
	require.NotContains(t, args, "--resume")
}

func TestBuildCLIArgs_Bare(t *testing.T) {
	t.Parallel()

	w := New()
	session := worker.SessionInfo{
		SessionID:  "bare-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
		Bare:       true,
	}

	args := w.buildCLIArgs(session, false)
	require.Contains(t, args, "--bare")
}

func TestBuildCLIArgs_AllowedDirs(t *testing.T) {
	t.Parallel()

	w := New()
	session := worker.SessionInfo{
		SessionID:   "dirs-session",
		UserID:      "test-user",
		ProjectDir:  "/tmp",
		AllowedDirs: []string{"/project/src", "/project/lib"},
	}

	args := w.buildCLIArgs(session, false)
	require.Contains(t, args, "--add-dir", "/project/src")
	require.Contains(t, args, "--add-dir", "/project/lib")
}

func TestBuildCLIArgs_MaxBudgetUSD(t *testing.T) {
	t.Parallel()

	w := New()
	session := worker.SessionInfo{
		SessionID:    "budget-session",
		UserID:       "test-user",
		ProjectDir:   "/tmp",
		MaxBudgetUSD: 0.05,
	}

	args := w.buildCLIArgs(session, false)
	require.Contains(t, args, "--max-budget-usd", "0.050000")
}

func TestBuildCLIArgs_JSONSchema(t *testing.T) {
	t.Parallel()

	w := New()
	session := worker.SessionInfo{
		SessionID:  "schema-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
		JSONSchema: "/schemas/output.json",
	}

	args := w.buildCLIArgs(session, false)
	require.Contains(t, args, "--json-schema", "/schemas/output.json")
}

// ─── Mock-based integration tests ──────────────────────────────────────────────

func TestStatusToSessionState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		wantOk bool
	}{
		{"idle maps to StateIdle", "idle", true},
		{"processing maps to StateRunning", "processing", true},
		{"unknown returns ok=false", "unknown_status", false},
		{"empty returns ok=false", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := statusToSessionState(tt.input)
			require.Equal(t, tt.wantOk, ok)
			if ok {
				require.NotEmpty(t, got)
			}
		})
	}
}

func TestMapper_Map_UnknownStatus(t *testing.T) {
	t.Parallel()

	log := newTestLogger()
	mapper := NewMapper(log, "session_123", func() int64 { return 1 })

	t.Run("mapSystem unknown status returns nil", func(t *testing.T) {
		evt := &WorkerEvent{Type: EventSystem, Payload: "unknown_status"}
		envs, err := mapper.Map(evt)
		require.NoError(t, err)
		require.Nil(t, envs)
	})

	t.Run("mapSessionState unknown returns nil", func(t *testing.T) {
		evt := &WorkerEvent{Type: EventSessionState, Payload: "unknown_status"}
		envs, err := mapper.Map(evt)
		require.NoError(t, err)
		require.Nil(t, envs)
	})
}
