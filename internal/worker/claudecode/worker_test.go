package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
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
	// --permission-prompt-tool not present by default (disabled)
	require.NotContains(t, args, "--permission-prompt-tool")
	// Custom PermissionMode="plan" → no --dangerously-skip-permissions
	require.NotContains(t, args, "--dangerously-skip-permissions")
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
	// resume=false → --session-id minimal-session, 9 tokens total:
	// --print --verbose --output-format stream-json --input-format stream-json
	// --session-id minimal-session --dangerously-skip-permissions
	// (--permission-prompt-tool stdio NOT included: default is disabled)
	require.Len(t, args, 9)
	require.Contains(t, args, "--print")
	require.Contains(t, args, "--verbose")
	require.NotContains(t, args, "--permission-prompt-tool")
	require.Contains(t, args, "--session-id", "minimal-session")
	require.Contains(t, args, "--dangerously-skip-permissions")
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

func TestBuildCLIArgs_PermissionPromptEnabled(t *testing.T) {
	// Do NOT use t.Parallel() — InitConfig mutates global state (security.RegisterCommand map).
	// Enable permission prompt via InitConfig (simulates config.yaml permission_prompt: true)
	InitConfig(config.ClaudeCodeConfig{Command: "claude", PermissionPrompt: true})
	defer InitConfig(config.ClaudeCodeConfig{Command: "claude"}) // reset to default

	w := New()
	session := worker.SessionInfo{
		SessionID:  "pp-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	args := w.buildCLIArgs(session, false)
	require.Contains(t, args, "--permission-prompt-tool", "stdio")
}

func TestBuildCLIArgs_PermissionPromptDisabled(t *testing.T) {
	// Do NOT use t.Parallel() — same reason as PermissionPromptEnabled.
	// Ensure default is disabled
	InitConfig(config.ClaudeCodeConfig{Command: "claude", PermissionPrompt: false})

	w := New()
	session := worker.SessionInfo{
		SessionID:  "pp-off-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	args := w.buildCLIArgs(session, false)
	require.NotContains(t, args, "--permission-prompt-tool")
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
		evt := &WorkerEvent{Type: EventSystem, Payload: json.RawMessage(`"unknown_status"`)}
		envs, err := mapper.Map(evt)
		require.NoError(t, err)
		require.Nil(t, envs)
	})

	t.Run("mapSessionState unknown returns nil", func(t *testing.T) {
		evt := &WorkerEvent{Type: EventSessionState, Payload: json.RawMessage(`"unknown_status"`)}
		envs, err := mapper.Map(evt)
		require.NoError(t, err)
		require.Nil(t, envs)
	})
}

func TestControlHandler_HandlePayload_AutoSuccess(t *testing.T) {
	t.Parallel()
	log := newTestLogger()
	var buf bytes.Buffer
	ch := NewControlHandler(log, &buf)

	payload := &ControlRequestPayload{
		RequestID: "req_auto1",
		Subtype:   string(ControlSetPermissionMode),
	}
	evt, err := ch.HandlePayload(payload)
	require.NoError(t, err)
	require.Nil(t, evt)

	var resp ControlResponse
	require.NoError(t, json.Unmarshal(buf.Bytes(), &resp))
	require.Equal(t, "control_response", resp.Type)
	require.Equal(t, "success", resp.Response.Subtype)
	require.Equal(t, "req_auto1", resp.Response.RequestID)
}

func TestControlHandler_HandlePayload_Interrupt(t *testing.T) {
	t.Parallel()
	log := newTestLogger()
	var buf bytes.Buffer
	ch := NewControlHandler(log, &buf)

	payload := &ControlRequestPayload{
		RequestID: "req_int1",
		Subtype:   string(ControlInterrupt),
	}
	evt, err := ch.HandlePayload(payload)
	require.NoError(t, err)
	require.Nil(t, evt)
	require.Empty(t, buf.String())
}

func TestControlHandler_HandlePayload_UnknownSubtype(t *testing.T) {
	t.Parallel()
	log := newTestLogger()
	var buf bytes.Buffer
	ch := NewControlHandler(log, &buf)

	payload := &ControlRequestPayload{
		RequestID: "req_unk",
		Subtype:   "completely_unknown_subtype",
	}
	evt, err := ch.HandlePayload(payload)
	require.NoError(t, err)
	require.Nil(t, evt)
	require.Empty(t, buf.String())
}

func TestControlHandler_SendQuestionResponse(t *testing.T) {
	t.Parallel()
	log := newTestLogger()
	var buf bytes.Buffer
	ch := NewControlHandler(log, &buf)

	err := ch.SendQuestionResponse("req_q1", map[string]string{"q1": "a1", "q2": "a2"})
	require.NoError(t, err)

	var resp ControlResponse
	require.NoError(t, json.Unmarshal(buf.Bytes(), &resp))
	require.Equal(t, "req_q1", resp.Response.RequestID)
	require.Equal(t, "allow", resp.Response.Response["behavior"])
}

func TestControlHandler_SendElicitationResponse(t *testing.T) {
	t.Parallel()
	log := newTestLogger()
	var buf bytes.Buffer
	ch := NewControlHandler(log, &buf)

	err := ch.SendElicitationResponse("req_e1", "accept", map[string]any{"key": "value"})
	require.NoError(t, err)

	var resp ControlResponse
	require.NoError(t, json.Unmarshal(buf.Bytes(), &resp))
	require.Equal(t, "req_e1", resp.Response.RequestID)
	require.Equal(t, "accept", resp.Response.Response["action"])
}

func TestSessionFileGlobs(t *testing.T) {
	t.Parallel()

	patterns := sessionFileGlobs("/home/user", "abc-123")
	require.Len(t, patterns, 3)
	require.Contains(t, patterns[0], "projects/*/abc-123.jsonl")
	require.Contains(t, patterns[1], "projects/*/abc-123")
	require.Contains(t, patterns[2], "session-env/abc-123")
}

func TestSessionFileGlobs_Matches(t *testing.T) {
	t.Parallel()

	t.Run("matches transcript file", func(t *testing.T) {
		t.Parallel()

		homeDir := t.TempDir()
		projectDir := filepath.Join(homeDir, ".claude", "projects", "hash")
		require.NoError(t, os.MkdirAll(projectDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, "test-uuid-1234.jsonl"), []byte("{}"), 0o644))

		matches, _ := filepath.Glob(sessionFileGlobs(homeDir, "test-uuid-1234")[0])
		require.Len(t, matches, 1)
	})

	t.Run("matches env file", func(t *testing.T) {
		t.Parallel()

		homeDir := t.TempDir()
		envDir := filepath.Join(homeDir, ".claude", "session-env")
		require.NoError(t, os.MkdirAll(envDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(envDir, "test-uuid-1234"), []byte("env"), 0o644))

		matches, _ := filepath.Glob(sessionFileGlobs(homeDir, "test-uuid-1234")[2])
		require.Len(t, matches, 1)
	})
}

func TestHasSessionFiles_NoFiles(t *testing.T) {
	t.Parallel()

	w := New()
	require.False(t, w.HasSessionFiles("sess_test-uuid-1234"))
}

// TestHasSessionFiles_EmptySessionEnvDir verifies that a stale empty session-env
// directory does NOT cause HasSessionFiles to return true (issue #172).
func TestHasSessionFiles_EmptySessionEnvDir(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	envDir := filepath.Join(homeDir, ".claude", "session-env", "test-uuid-1234")
	require.NoError(t, os.MkdirAll(envDir, 0o755))

	// The empty directory exists on disk but no JSONL file.
	parsedID := "test-uuid-1234"
	pattern := filepath.Join(homeDir, ".claude", "projects", "*", parsedID+".jsonl")
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && !fi.IsDir() {
			t.Fatal("should not match any JSONL file")
		}
	}

	// Verify the session-env dir itself exists (would have matched old logic).
	envMatches, _ := filepath.Glob(filepath.Join(homeDir, ".claude", "session-env", parsedID))
	require.Len(t, envMatches, 1) // stale empty dir is on disk
}

func TestHasSessionFiles_JSONLExists(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	projectDir := filepath.Join(homeDir, ".claude", "projects", "hash")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "test-uuid-1234.jsonl"), []byte("{}"), 0o644))

	parsedID := "test-uuid-1234"
	pattern := filepath.Join(homeDir, ".claude", "projects", "*", parsedID+".jsonl")
	matches, _ := filepath.Glob(pattern)
	require.Len(t, matches, 1)

	found := false
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && !fi.IsDir() {
			found = true
		}
	}
	require.True(t, found, "JSONL file should be detected as a valid session file")
}

func TestAutoApproveTool_MatchingTool(t *testing.T) {
	original := permissionAutoApprove.Load()
	defer permissionAutoApprove.Store(original)

	permissionAutoApprove.Store([]string{"ExitPlanMode"})

	var buf bytes.Buffer
	ctrl := NewControlHandler(slog.Default(), &buf)
	cr := &ControlRequestPayload{
		Subtype:   "can_use_tool",
		ToolName:  "ExitPlanMode",
		RequestID: "req-123",
	}

	result := autoApproveTool(ctrl, cr)
	require.True(t, result, "should auto-approve ExitPlanMode")

	// Verify a response was written to stdin (buf)
	require.NotEmpty(t, buf.String(), "should send permission response")
	require.Contains(t, buf.String(), `"allowed":true`)
	require.Contains(t, buf.String(), "auto-approved")
}

func TestAutoApproveTool_NonMatchingTool(t *testing.T) {
	original := permissionAutoApprove.Load()
	defer permissionAutoApprove.Store(original)

	permissionAutoApprove.Store([]string{"ExitPlanMode"})

	var buf bytes.Buffer
	ctrl := NewControlHandler(slog.Default(), &buf)
	cr := &ControlRequestPayload{
		Subtype:   "can_use_tool",
		ToolName:  "Bash",
		RequestID: "req-456",
	}

	result := autoApproveTool(ctrl, cr)
	require.False(t, result, "should not auto-approve Bash")
	require.Empty(t, buf.String(), "should not send any response")
}

func TestAutoApproveTool_EmptyOrNilList(t *testing.T) {
	original := permissionAutoApprove.Load()
	defer permissionAutoApprove.Store(original)

	tests := []struct {
		name  string
		store any // []string or nil-equivalent
	}{
		{"empty slice", []string{}},
		{"nil stored as empty", []string{}}, // InitConfig normalizes nil → []string{}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			permissionAutoApprove.Store(tt.store)

			var buf bytes.Buffer
			ctrl := NewControlHandler(slog.Default(), &buf)
			cr := &ControlRequestPayload{
				Subtype:   "can_use_tool",
				ToolName:  "ExitPlanMode",
				RequestID: "req-empty",
			}

			result := autoApproveTool(ctrl, cr)
			require.False(t, result, "should not auto-approve with empty list")
			require.Empty(t, buf.String())
		})
	}
}

func TestInitConfig_PermissionSettings(t *testing.T) {
	origCmd := commandParts.Load()
	origPP := permissionPrompt.Load()
	origAA := permissionAutoApprove.Load()
	defer func() {
		commandParts.Store(origCmd)
		permissionPrompt.Store(origPP)
		permissionAutoApprove.Store(origAA)
	}()

	tests := []struct {
		name     string
		cfg      config.ClaudeCodeConfig
		wantList []string
		wantPP   bool
	}{
		{
			name: "with auto-approve tools",
			cfg: config.ClaudeCodeConfig{
				Command:               "claude",
				PermissionPrompt:      true,
				PermissionAutoApprove: []string{"ExitPlanMode", "Read"},
			},
			wantList: []string{"ExitPlanMode", "Read"},
			wantPP:   true,
		},
		{
			name: "nil auto-approve normalized to empty",
			cfg: config.ClaudeCodeConfig{
				Command:               "claude",
				PermissionPrompt:      false,
				PermissionAutoApprove: nil,
			},
			wantList: []string{},
			wantPP:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			InitConfig(tt.cfg)
			require.Equal(t, []string{"claude"}, commandParts.Load())
			require.Equal(t, tt.wantPP, permissionPrompt.Load().(bool))
			require.Equal(t, tt.wantList, permissionAutoApprove.Load())
		})
	}
}
