package session

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/hotplex/hotplex-worker/internal/worker"
)

func TestDeriveSessionKey_Deterministic(t *testing.T) {
	t.Parallel()

	key := DeriveSessionKey("u1", worker.TypeClaudeCode, "s1", "/tmp/hotplex/workspace")
	for i := 0; i < 1000; i++ {
		got := DeriveSessionKey("u1", worker.TypeClaudeCode, "s1", "/tmp/hotplex/workspace")
		require.Equal(t, key, got, "DeriveSessionKey must be deterministic across 1000 calls")
	}
}

func TestDeriveSessionKey_DifferentTuples(t *testing.T) {
	t.Parallel()

	key1 := DeriveSessionKey("u1", worker.TypeClaudeCode, "s1", "/tmp/hotplex/workspace")
	key2 := DeriveSessionKey("u2", worker.TypeClaudeCode, "s1", "/tmp/hotplex/workspace")
	key3 := DeriveSessionKey("u1", worker.TypeOpenCodeCLI, "s1", "/tmp/hotplex/workspace")
	key4 := DeriveSessionKey("u1", worker.TypeClaudeCode, "s2", "/tmp/hotplex/workspace")
	key5 := DeriveSessionKey("u1", worker.TypeClaudeCode, "s1", "/tmp/hotplex/projects/foo")

	require.NotEqual(t, key1, key2, "different ownerID → different key")
	require.NotEqual(t, key1, key3, "different workerType → different key")
	require.NotEqual(t, key1, key4, "different clientSessionID → different key")
	require.NotEqual(t, key1, key5, "different workDir → different key")
}

func TestDeriveSessionKey_UUIDv5Format(t *testing.T) {
	t.Parallel()

	// UUIDv5 format: xxxxxxxx-xxxx-5xxx-yxxx-xxxxxxxxxxxx
	// y is one of [8, 9, a, b]
	uuidV5Regex := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)

	tests := []struct {
		ownerID    string
		workerType worker.WorkerType
		sessionID  string
		workDir    string
	}{
		{"u1", worker.TypeClaudeCode, "s1", "/tmp/hotplex/workspace"},
		{"user_long_id", worker.TypeOpenCodeCLI, "my-session-123", "/tmp/hotplex/projects/app"},
		{"", worker.TypePimon, "", ""},
		{"owner", worker.TypeOpenCodeSrv, "session-with-dashes", "/var/hotplex/projects"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.workerType), func(t *testing.T) {
			t.Parallel()
			key := DeriveSessionKey(tt.ownerID, tt.workerType, tt.sessionID, tt.workDir)
			require.Regexp(t, uuidV5Regex, key, "output must be valid UUIDv5 format")
		})
	}
}

func TestDeriveSessionKey_EmptyString(t *testing.T) {
	t.Parallel()

	// Empty client_session_id still produces a valid UUIDv5
	key := DeriveSessionKey("u1", worker.TypeClaudeCode, "", "")
	require.NotEmpty(t, key)
	uuidV5Regex := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)
	require.Regexp(t, uuidV5Regex, key)
}

func TestDeriveSessionKey_AllWorkerTypes(t *testing.T) {
	t.Parallel()

	sessionID := "test-session"
	for _, wt := range []worker.WorkerType{
		worker.TypeClaudeCode,
		worker.TypeOpenCodeCLI,
		worker.TypeOpenCodeSrv,
		worker.TypePimon,
		worker.TypeUnknown,
	} {
		wt := wt
		t.Run(string(wt), func(t *testing.T) {
			t.Parallel()
			key := DeriveSessionKey("owner1", wt, sessionID, "")
			require.NotEmpty(t, key)
			// Same tuple must produce same key
			key2 := DeriveSessionKey("owner1", wt, sessionID, "")
			require.Equal(t, key, key2)
		})
	}
}
