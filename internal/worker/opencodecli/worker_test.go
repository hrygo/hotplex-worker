package opencodecli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/worker"
)

func TestOpenCodeCLIWorker_Capabilities(t *testing.T) {
	t.Parallel()
	w := New()

	require.Equal(t, worker.TypeOpenCodeCLI, w.Type())
	require.True(t, w.SupportsResume())
	require.True(t, w.SupportsStreaming())
	require.True(t, w.SupportsTools())
	require.NotNil(t, w.EnvWhitelist())
	require.Equal(t, ".opencode/sessions", w.SessionStoreDir())
	require.Zero(t, w.MaxTurns())
	require.Equal(t, []string{"text", "code"}, w.Modalities())
}

func TestOpenCodeCLIWorker_EnvWhitelist(t *testing.T) {
	t.Parallel()
	w := New()

	wl := w.EnvWhitelist()
	require.Contains(t, wl, "HOME")
	require.Contains(t, wl, "PATH")
	require.Contains(t, wl, "OPENAI_API_KEY")
	require.Contains(t, wl, "OPENCODE_API_KEY")
	require.Contains(t, wl, "OTEL_")
}

func TestOpenCodeCLIWorker_ConnBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	require.Nil(t, w.Conn())
}

func TestOpenCodeCLIWorker_HealthBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()

	h := w.Health()
	require.Equal(t, worker.TypeOpenCodeCLI, h.Type)
	require.False(t, h.Running)
	require.True(t, h.Healthy)
	require.Empty(t, h.SessionID)
}

func TestOpenCodeCLIWorker_LastIOBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	require.True(t, w.LastIO().IsZero())
}

func TestOpenCodeCLIWorker_TerminateWithoutStart(t *testing.T) {
	t.Parallel()
	w := New()
	err := w.Terminate(context.Background())
	require.NoError(t, err)
}

func TestOpenCodeCLIWorker_BuildCLIArgs(t *testing.T) {
	tests := []struct {
		name    string
		session worker.SessionInfo
		openSID string
		want    []string
	}{
		{
			name:    "base args",
			session: worker.SessionInfo{},
			openSID: "",
			want:    []string{"run", "--format", "json"},
		},
		{
			name: "with session continuation",
			session: worker.SessionInfo{
				SessionID: "sess_abc123",
			},
			openSID: "ses_def456",
			want:    []string{"run", "--format", "json", "--session", "ses_def456"},
		},
		{
			name: "with continue flag",
			session: worker.SessionInfo{
				ContinueSession: true,
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--continue"},
		},
		{
			name: "with MCP config",
			session: worker.SessionInfo{
				MCPConfig: "/path/to/mcp.json",
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--mcp-config", "/path/to/mcp.json"},
		},
		{
			name: "with MCP config and strict",
			session: worker.SessionInfo{
				MCPConfig:       "/path/to/mcp.json",
				StrictMCPConfig: true,
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--mcp-config", "/path/to/mcp.json", "--strict-mcp-config"},
		},
		{
			name: "with max turns",
			session: worker.SessionInfo{
				MaxTurns: 5,
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--max-turns", "5"},
		},
		{
			name: "with bare mode",
			session: worker.SessionInfo{
				Bare: true,
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--bare"},
		},
		{
			name: "with skip permissions",
			session: worker.SessionInfo{
				SkipPermissions: true,
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--dangerously-skip-permissions"},
		},
		{
			name: "with permission mode",
			session: worker.SessionInfo{
				PermissionMode: "auto-accept",
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--permission-mode", "auto-accept"},
		},
		{
			name: "with system prompt replace",
			session: worker.SessionInfo{
				SystemPromptReplace: "You are a helpful assistant.",
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--system-prompt", "You are a helpful assistant."},
		},
		{
			name: "with append system prompt",
			session: worker.SessionInfo{
				SystemPrompt: "Remember to be concise.",
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--append-system-prompt", "Remember to be concise."},
		},
		{
			name: "with max budget",
			session: worker.SessionInfo{
				MaxBudgetUSD: 1.5,
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--max-budget-usd", "1.500000"},
		},
		{
			name: "with allowed dirs",
			session: worker.SessionInfo{
				AllowedDirs: []string{"/tmp", "/home/user"},
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--add-dir", "/tmp", "--add-dir", "/home/user"},
		},
		{
			name: "with json schema",
			session: worker.SessionInfo{
				JSONSchema: "/tmp/schema.json",
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--json-schema", "/tmp/schema.json"},
		},
		{
			name: "with include hook events",
			session: worker.SessionInfo{
				IncludeHookEvents: true,
			},
			openSID: "",
			want:    []string{"run", "--format", "json", "--include-hook-events"},
		},
		{
			name: "session ID takes precedence over continue",
			session: worker.SessionInfo{
				ContinueSession: true,
				SessionID:       "sess_abc123",
			},
			openSID: "ses_def456",
			want:    []string{"run", "--format", "json", "--session", "ses_def456"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := New()
			got := w.buildCLIArgs(tt.session, tt.openSID)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRecvOnlyConn(t *testing.T) {
	t.Parallel()

	conn := newRecvOnlyConn("user1", "session1", nil)
	require.Equal(t, "user1", conn.UserID())
	require.Equal(t, "session1", conn.SessionID())

	// SetSessionID updates under lock
	conn.SetSessionID("session2")
	require.Equal(t, "session2", conn.SessionID())

	// TrySend returns true when channel has capacity
	// Note: Recv() returns the channel so tests can drain it
	_ = conn.Recv()

	err := conn.Close()
	require.NoError(t, err)
}
