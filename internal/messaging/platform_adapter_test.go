package messaging

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlatformType_ExtractPlatformKeys_Feishu(t *testing.T) {
	t.Parallel()

	pk := PlatformFeishu.ExtractPlatformKeys(map[string]any{
		"chat_id":   "oc_123",
		"thread_ts": "msg_456",
		"user_id":   "ou_789",
		"ignored":   "value",
	})
	require.Equal(t, map[string]string{
		"chat_id":   "oc_123",
		"thread_ts": "msg_456",
		"user_id":   "ou_789",
	}, pk)
}

func TestPlatformType_ExtractPlatformKeys_Slack(t *testing.T) {
	t.Parallel()

	pk := PlatformSlack.ExtractPlatformKeys(map[string]any{
		"team_id":    "T123",
		"channel_id": "C456",
		"thread_ts":  "123.456",
		"user_id":    "U789",
	})
	require.Equal(t, map[string]string{
		"team_id":    "T123",
		"channel_id": "C456",
		"thread_ts":  "123.456",
		"user_id":    "U789",
	}, pk)
}

func TestPlatformType_ExtractPlatformKeys_EmptyValues(t *testing.T) {
	t.Parallel()

	pk := PlatformSlack.ExtractPlatformKeys(map[string]any{
		"team_id":    "",
		"channel_id": "C1",
	})
	require.Equal(t, map[string]string{
		"channel_id": "C1",
	}, pk)
}

func TestPlatformAdapter_StartGuard(t *testing.T) {
	t.Parallel()

	a := &PlatformAdapter{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	require.True(t, a.StartGuard(), "first call should return true")
	require.False(t, a.StartGuard(), "subsequent calls should return false")
}

func TestPlatformAdapter_MarkClosed(t *testing.T) {
	t.Parallel()

	a := &PlatformAdapter{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	require.False(t, a.IsClosed())
	a.MarkClosed()
	require.True(t, a.IsClosed())
}

func TestPlatformAdapter_ConfigureShared(t *testing.T) {
	t.Parallel()

	a := &PlatformAdapter{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	a.ConfigureShared(AdapterConfig{
		Gate: NewGate("open", "open", false, nil, nil, nil),
		Extras: map[string]any{
			"reconnect_base_delay": 2 * time.Second,
			"reconnect_max_delay":  30 * time.Second,
		},
	})
	require.NotNil(t, a.Gate)
	require.Equal(t, 2*time.Second, a.BackoffBaseDelay)
	require.Equal(t, 30*time.Second, a.BackoffMaxDelay)
}

func TestPlatformAdapter_InitCloseSharedState(t *testing.T) {
	t.Parallel()

	a := &PlatformAdapter{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	a.InitSharedState()
	require.NotNil(t, a.Dedup)
	require.NotNil(t, a.Interactions)

	a.CloseSharedState()
	require.Nil(t, a.Dedup)

	// Second close is safe.
	a.CloseSharedState()
}

func TestPlatformAdapter_Bridge(t *testing.T) {
	t.Parallel()

	a := &PlatformAdapter{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	require.Nil(t, a.Bridge())
}
