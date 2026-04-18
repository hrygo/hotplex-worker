package messaging

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"sync"
	"testing"

	"github.com/hotplex/hotplex-worker/internal/session"
	"github.com/hotplex/hotplex-worker/pkg/events"
	"github.com/stretchr/testify/require"
)

// mockPlatformConn is a test double for PlatformConn.
type mockPlatformConn struct {
	mu      sync.Mutex
	written []*events.Envelope
	closed  bool
}

func (m *mockPlatformConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.written = append(m.written, env)
	return nil
}

func (m *mockPlatformConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func TestPlatformConn_WriteAndClose(t *testing.T) {
	t.Parallel()

	conn := &mockPlatformConn{}
	ctx := context.Background()

	env := &events.Envelope{
		SessionID: "test-session",
		Event:     events.Event{Type: "text", Data: "hello"},
	}

	require.NoError(t, conn.WriteCtx(ctx, env))
	require.NoError(t, conn.Close())

	conn.mu.Lock()
	defer conn.mu.Unlock()
	require.Len(t, conn.written, 1)
	require.Equal(t, "hello", conn.written[0].Event.Data)
	require.True(t, conn.closed)
}

func TestPlatformConn_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	conn := &mockPlatformConn{}
	ctx := context.Background()
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			env := &events.Envelope{
				SessionID: "session-1",
				Event:     events.Event{Type: "text", Data: n},
			}
			_ = conn.WriteCtx(ctx, env)
		}(i)
	}

	wg.Wait()
	conn.mu.Lock()
	defer conn.mu.Unlock()
	require.Len(t, conn.written, 100)
}

func TestRegistry_NewUnknown(t *testing.T) {
	t.Parallel()

	_, err := New("unknown-platform", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown platform")
}

func TestAdapter_BaseMethods(t *testing.T) {
	t.Parallel()

	a := &PlatformAdapter{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// Verify setters don't panic
	hub := &mockHub{}
	a.SetHub(hub)
	a.SetSessionManager(nil)
	a.SetHandler(nil)
	a.SetBridge(nil)
}

type mockHub struct{}

func (m *mockHub) JoinPlatformSession(sessionID string, pc PlatformConn) {}

var uuidV5Regex = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)

func TestBridge_MakeSlackEnvelope(t *testing.T) {
	t.Parallel()

	teamID := "T123"
	channelID := "C456"
	threadTS := "1234567890.123456"
	userID := "U789"
	text := "hello"

	br := NewBridge(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		PlatformSlack,
		&mockHub{},
		nil,
		nil,
		nil,
		"claude_code",
		"",
	)

	env := br.MakeSlackEnvelope(teamID, channelID, threadTS, userID, text)
	require.NotNil(t, env)

	// Session ID is now a UUIDv5 derived from platform context.
	require.Regexp(t, uuidV5Regex, env.SessionID)
	require.Equal(t, userID, env.OwnerID)

	// Deterministic: same inputs produce the same UUIDv5.
	env2 := br.MakeSlackEnvelope(teamID, channelID, threadTS, userID, text)
	require.Equal(t, env.SessionID, env2.SessionID)

	// Matches the underlying derivation function.
	expected := session.DerivePlatformSessionKey(userID, "claude_code", session.PlatformContext{
		Platform:  "slack",
		TeamID:    teamID,
		ChannelID: channelID,
		ThreadTS:  threadTS,
		UserID:    userID,
	})
	require.Equal(t, expected, env.SessionID)

	// Event.Data is a map with content and metadata
	data, ok := env.Event.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, text, data["content"])
}

func TestBridge_MakeFeishuEnvelope(t *testing.T) {
	t.Parallel()

	chatID := "oc_abc123"
	threadTS := "msg_456"
	userID := "ou_789"
	text := "飞书消息"

	br := NewBridge(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		PlatformFeishu,
		&mockHub{},
		nil,
		nil,
		nil,
		"claude_code",
		"",
	)

	env := br.MakeFeishuEnvelope(chatID, threadTS, userID, text)
	require.NotNil(t, env)

	// Session ID is now a UUIDv5 derived from platform context.
	require.Regexp(t, uuidV5Regex, env.SessionID)

	// Deterministic: same inputs produce the same UUIDv5.
	env2 := br.MakeFeishuEnvelope(chatID, threadTS, userID, text)
	require.Equal(t, env.SessionID, env2.SessionID)

	// Matches the underlying derivation function.
	expected := session.DerivePlatformSessionKey(userID, "claude_code", session.PlatformContext{
		Platform: "feishu",
		ChatID:   chatID,
		ThreadTS: threadTS,
		UserID:   userID,
	})
	require.Equal(t, expected, env.SessionID)

	data, ok := env.Event.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, text, data["content"])
}
