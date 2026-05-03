package messaging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

func newTestBridge() *Bridge {
	return &Bridge{
		log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		workerType: string(worker.TypeClaudeCode),
		workDir:    "/tmp/test",
	}
}

// PBAC-023: MakeEnvelope includes bot_id in metadata map.
func TestBridge_MakeEnvelope_BotIDInMetadata(t *testing.T) {
	t.Parallel()

	b := newTestBridge()

	t.Run("with bot_id", func(t *testing.T) {
		t.Parallel()
		env := b.MakeEnvelope("user1", "hello", session.PlatformContext{
			Platform:  "slack",
			BotID:     "U_BOT_123",
			TeamID:    "T001",
			ChannelID: "C100",
		})
		md := env.Event.Data.(map[string]any)["metadata"].(map[string]any)
		require.Equal(t, "U_BOT_123", md["bot_id"])
		require.Equal(t, "slack", md["platform"])
	})

	t.Run("without bot_id", func(t *testing.T) {
		t.Parallel()
		env := b.MakeEnvelope("user1", "hello", session.PlatformContext{
			Platform:  "slack",
			TeamID:    "T001",
			ChannelID: "C100",
		})
		md := env.Event.Data.(map[string]any)["metadata"].(map[string]any)
		_, ok := md["bot_id"]
		require.False(t, ok, "bot_id should not be present when empty")
	})
}

// PBAC-011: MakeSlackEnvelope includes botID in PlatformContext.
func TestBridge_MakeSlackEnvelope_BotID(t *testing.T) {
	t.Parallel()

	b := newTestBridge()
	env := b.MakeSlackEnvelope("T001", "C100", "123.456", "U001", "hello", "", "U_BOT_X")

	md := env.Event.Data.(map[string]any)["metadata"].(map[string]any)
	require.Equal(t, "U_BOT_X", md["bot_id"])
	require.Equal(t, "T001", md["team_id"])
	require.Equal(t, "C100", md["channel_id"])
}

// PBAC-022: MakeFeishuEnvelope includes botID in PlatformContext and metadata.
func TestBridge_MakeFeishuEnvelope_BotID(t *testing.T) {
	t.Parallel()

	b := newTestBridge()
	env := b.MakeFeishuEnvelope("oc_chat1", "msg_001", "ou_user1", "hello", "", "ou_bot_Y")

	md := env.Event.Data.(map[string]any)["metadata"].(map[string]any)
	require.Equal(t, "ou_bot_Y", md["bot_id"])
	require.Equal(t, "oc_chat1", md["chat_id"])
}

// PBAC-025: Bridge.Handle() extracts botID from adapter via GetBotID(), passes to StartPlatformSession.
func TestBridge_Handle_ExtractsBotID(t *testing.T) {
	t.Parallel()

	b := newTestBridge()
	b.adapter.Store(&mockBotIDAdapter{botID: "U_ADAPTER_BOT"})
	b.handler = &mockHandler{}

	var capturedBotID string
	b.starter = &mockStarter{
		startFn: func(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ map[string]string, botID string) error {
			capturedBotID = botID
			return nil
		},
	}

	env := &events.Envelope{
		SessionID: "sid1",
		OwnerID:   "owner1",
		Event: events.Event{
			Type: events.Input,
			Data: map[string]any{"content": "test"},
		},
	}
	_ = b.Handle(context.Background(), env, nil)
	require.Equal(t, "U_ADAPTER_BOT", capturedBotID, "Handle must pass adapter's botID to StartPlatformSession")
}

// mockBotIDAdapter implements PlatformAdapterInterface for testing GetBotID (PBAC-009, PBAC-010).
type mockBotIDAdapter struct {
	botID string
}

func (m *mockBotIDAdapter) Platform() PlatformType        { return PlatformSlack }
func (m *mockBotIDAdapter) Start(_ context.Context) error { return nil }
func (m *mockBotIDAdapter) HandleTextMessage(_ context.Context, _, _, _, _, _, _ string) error {
	return nil
}
func (m *mockBotIDAdapter) Close(_ context.Context) error       { return nil }
func (m *mockBotIDAdapter) ConfigureWith(_ AdapterConfig) error { return nil }
func (m *mockBotIDAdapter) GetBotID() string                    { return m.botID }

// mockHandler implements HandlerInterface.
type mockHandler struct{}

func (m *mockHandler) Handle(_ context.Context, _ *events.Envelope) error { return nil }

// mockStarter captures botID passed to StartPlatformSession.
type mockStarter struct {
	startFn func(ctx context.Context, sessionID, ownerID, workerType, workDir, platform string, platformKey map[string]string, botID string) error
}

func (s *mockStarter) StartPlatformSession(ctx context.Context, sessionID, ownerID, workerType, workDir, platform string, platformKey map[string]string, botID string) error {
	if s.startFn != nil {
		return s.startFn(ctx, sessionID, ownerID, workerType, workDir, platform, platformKey, botID)
	}
	return nil
}

// I5: nil adapter passes empty botID to StartPlatformSession.
func TestBridge_Handle_NilAdapter_EmptyBotID(t *testing.T) {
	t.Parallel()

	b := newTestBridge()
	b.handler = &mockHandler{}

	var capturedBotID string
	b.starter = &mockStarter{
		startFn: func(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ map[string]string, botID string) error {
			capturedBotID = botID
			return nil
		},
	}

	env := &events.Envelope{
		SessionID: "sid1",
		OwnerID:   "owner1",
		Event: events.Event{
			Type: events.Input,
			Data: map[string]any{"content": "test"},
		},
	}
	_ = b.Handle(context.Background(), env, nil)
	require.Equal(t, "", capturedBotID, "nil adapter must pass empty botID")
}

// C2: Handle() returns error when StartPlatformSession fails.
func TestBridge_Handle_StartError(t *testing.T) {
	t.Parallel()

	b := newTestBridge()
	b.handler = &mockHandler{}
	b.starter = &mockStarter{
		startFn: func(_ context.Context, _ string, _ string, _ string, _ string, _ string, _ map[string]string, _ string) error {
			return fmt.Errorf("pool exhausted")
		},
	}

	env := &events.Envelope{
		SessionID: "sid1",
		OwnerID:   "owner1",
		Event: events.Event{
			Type: events.Input,
			Data: map[string]any{"content": "test"},
		},
	}
	err := b.Handle(context.Background(), env, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "session start failed")
}
