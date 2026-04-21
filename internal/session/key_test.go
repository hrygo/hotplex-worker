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
	key3 := DeriveSessionKey("u1", worker.TypeOpenCodeSrv, "s1", "/tmp/hotplex/workspace")
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
		{"user_long_id", worker.TypeOpenCodeSrv, "my-session-123", "/tmp/hotplex/projects/app"},
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

func TestDerivePlatformSessionKey_Deterministic(t *testing.T) {
	t.Parallel()

	ctx := PlatformContext{
		Platform: "feishu", ChatID: "oc12345", ThreadTS: "thread001", UserID: "ou_abc",
	}
	key := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, ctx)
	for i := 0; i < 1000; i++ {
		got := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, ctx)
		require.Equal(t, key, got, "DerivePlatformSessionKey must be deterministic across 1000 calls")
	}
}

func TestDerivePlatformSessionKey_DifferentTuples(t *testing.T) {
	t.Parallel()

	ctx1 := PlatformContext{Platform: "feishu", ChatID: "oc1", UserID: "u1"}
	ctx2 := PlatformContext{Platform: "feishu", ChatID: "oc2", UserID: "u1"}
	ctx3 := PlatformContext{Platform: "slack", ChannelID: "oc1", UserID: "u1"} // same raw ID as ctx2 but different platform
	ctx4 := PlatformContext{Platform: "feishu", ChatID: "oc1", UserID: "u2"}
	ctx5 := PlatformContext{Platform: "feishu", ChatID: "oc1", UserID: "u1", WorkDir: "/tmp/hotplex/workspace"}

	key1 := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, ctx1)
	key2 := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, ctx2)
	key3 := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, ctx3)
	key4 := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, ctx4)
	key5 := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, ctx5)

	require.NotEqual(t, key1, key2, "different ChatID → different key")
	require.NotEqual(t, key2, key3, "different Platform → different key (cross-platform isolation)")
	require.NotEqual(t, key1, key4, "different UserID → different key")
	require.NotEqual(t, key1, key5, "different WorkDir → different key")
}

func TestDerivePlatformSessionKey_CrossPlatformIsolation(t *testing.T) {
	t.Parallel()

	// Same raw ID used as both Feishu ChatID and Slack ChannelID must NOT collide.
	rawID := "oc_shared123"
	feishuCtx := PlatformContext{Platform: "feishu", ChatID: rawID, UserID: "u1"}
	slackCtx := PlatformContext{Platform: "slack", ChannelID: rawID, UserID: "u1"}

	feishuKey := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, feishuCtx)
	slackKey := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, slackCtx)

	require.NotEqual(t, feishuKey, slackKey, "same raw ID on different platforms must produce different keys")
}

func TestDerivePlatformSessionKey_ThreadTSDifferentiation(t *testing.T) {
	t.Parallel()

	// ThreadTS presence MUST produce different keys — conversation in a thread ≠ DM.
	ctxWithThread := PlatformContext{Platform: "feishu", ChatID: "oc1", ThreadTS: "thread001", UserID: "u1"}
	ctxWithoutThread := PlatformContext{Platform: "feishu", ChatID: "oc1", ThreadTS: "", UserID: "u1"}

	keyWith := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, ctxWithThread)
	keyWithout := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, ctxWithoutThread)

	require.NotEqual(t, keyWith, keyWithout, "ThreadTS presence MUST produce different keys")
}

func TestDerivePlatformSessionKey_SlackDMThreadDifferentiation(t *testing.T) {
	t.Parallel()

	// DM with no thread vs DM with thread → MUST produce different keys.
	// This covers the Slack DM-in-thread scenario (e.g., user replies to DM in a thread).
	dmNoThread := PlatformContext{Platform: "slack", TeamID: "T001", ChannelID: "D9876543210", ThreadTS: "", UserID: "U111"}
	dmWithThread := PlatformContext{Platform: "slack", TeamID: "T001", ChannelID: "D9876543210", ThreadTS: "1234567890.111222", UserID: "U111"}

	keyNoThread := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, dmNoThread)
	keyWithThread := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, dmWithThread)

	require.NotEqual(t, keyNoThread, keyWithThread,
		"Slack DM with threadTS must map to different session than DM without thread")

	// Same threadTS in same DM → same key (deterministic).
	keyWithThread2 := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, dmWithThread)
	require.Equal(t, keyWithThread, keyWithThread2)

	// Same DM, different threadTS → different key.
	dmDifferentThread := PlatformContext{Platform: "slack", TeamID: "T001", ChannelID: "D9876543210", ThreadTS: "1234567890.333444", UserID: "U111"}
	keyDiffThread := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, dmDifferentThread)
	require.NotEqual(t, keyWithThread, keyDiffThread, "different ThreadTS in same DM → different session")

	// DM vs channel in same workspace with same thread → different keys (D vs C prefix).
	channelWithThread := PlatformContext{Platform: "slack", TeamID: "T001", ChannelID: "C0123456789", ThreadTS: "1234567890.111222", UserID: "U111"}
	keyChannelThread := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, channelWithThread)
	require.NotEqual(t, keyWithThread, keyChannelThread, "DM thread vs channel thread → different sessions")
}

func TestDerivePlatformSessionKey_UUIDv5Format(t *testing.T) {
	t.Parallel()

	uuidV5Regex := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)

	tests := []struct {
		name string
		ctx  PlatformContext
	}{
		{"feishu_full", PlatformContext{Platform: "feishu", ChatID: "oc123", ThreadTS: "t001", UserID: "u1"}},
		{"feishu_dm", PlatformContext{Platform: "feishu", ChatID: "oc456", ThreadTS: "", UserID: "u2"}},
		{"slack_full", PlatformContext{Platform: "slack", TeamID: "T001", ChannelID: "C001", ThreadTS: "ts001", UserID: "U001"}},
		{"slack_dm", PlatformContext{Platform: "slack", TeamID: "T001", ChannelID: "D001", ThreadTS: "", UserID: "U002"}},
		{"slack_dm_thread", PlatformContext{Platform: "slack", TeamID: "T001", ChannelID: "D001", ThreadTS: "1234567890.111222", UserID: "U002"}},
		{"slack_channel_only", PlatformContext{Platform: "slack", TeamID: "", ChannelID: "C002", ThreadTS: "", UserID: "U003"}},
		{"empty", PlatformContext{}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key := DerivePlatformSessionKey("owner1", worker.TypeClaudeCode, tt.ctx)
			require.NotEmpty(t, key)
			require.Regexp(t, uuidV5Regex, key, "output must be valid UUIDv5 format")
		})
	}
}

func TestDerivePlatformSessionKey_AllWorkerTypes(t *testing.T) {
	t.Parallel()

	ctx := PlatformContext{Platform: "feishu", ChatID: "oc1", UserID: "u1"}
	for _, wt := range []worker.WorkerType{
		worker.TypeClaudeCode,
		worker.TypeOpenCodeSrv,
		worker.TypePimon,
		worker.TypeUnknown,
	} {
		wt := wt
		t.Run(string(wt), func(t *testing.T) {
			t.Parallel()
			key := DerivePlatformSessionKey("owner1", wt, ctx)
			require.NotEmpty(t, key)
			key2 := DerivePlatformSessionKey("owner1", wt, ctx)
			require.Equal(t, key, key2, "same tuple must produce same key")
		})
	}
}
