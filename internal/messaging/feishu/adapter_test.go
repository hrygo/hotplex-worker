package feishu

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hotplex/hotplex-worker/internal/messaging"
	"github.com/hotplex/hotplex-worker/pkg/events"
	"github.com/stretchr/testify/require"
)

func TestExtractResponseText_NilEnvelope(t *testing.T) {
	t.Parallel()
	_, ok := extractResponseText(nil)
	require.False(t, ok)
}

func TestExtractResponseText_StringData(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{
		Event: events.Event{
			Type: "text",
			Data: "hello world",
		},
	}
	text, ok := extractResponseText(env)
	require.True(t, ok)
	require.Equal(t, "hello world", text)
}

func TestExtractResponseText_MessageDeltaData(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{
		Event: events.Event{
			Type: "message_delta",
			Data: events.MessageDeltaData{
				Content: "streaming content",
			},
		},
	}
	text, ok := extractResponseText(env)
	require.True(t, ok)
	require.Equal(t, "streaming content", text)
}

func TestExtractResponseText_MapData(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{
		Event: events.Event{
			Type: "text",
			Data: map[string]any{
				"content": "map content",
			},
		},
	}
	text, ok := extractResponseText(env)
	require.True(t, ok)
	require.Equal(t, "map content", text)
}

func TestExtractResponseText_DoneEvent(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{
		Event: events.Event{
			Type: "done",
			Data: events.DoneData{Success: true},
		},
	}
	_, ok := extractResponseText(env)
	require.False(t, ok)
}

func TestExtractResponseText_RawData(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{
		Event: events.Event{
			Type: "raw",
			Data: events.RawData{
				Raw: map[string]any{
					"text": "raw text",
				},
			},
		},
	}
	text, ok := extractResponseText(env)
	require.True(t, ok)
	require.Equal(t, "raw text", text)
}

func TestFeishuConn_WriteCtx_NilEnvelope(t *testing.T) {
	t.Parallel()
	adapter := &Adapter{log: slog.New(slog.NewTextHandler(io.Discard, nil)), dedup: make(map[string]time.Time), activeConns: make(map[string]*FeishuConn), dedupDone: make(chan struct{})}
	conn := NewFeishuConn(adapter, "test_chat")

	err := conn.WriteCtx(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil envelope")
}

func TestAdapter_Configure(t *testing.T) {
	t.Parallel()
	a := &Adapter{log: nil}
	a.Configure("app123", "secret456", nil)

	require.Equal(t, "app123", a.appID)
	require.Equal(t, "secret456", a.appSecret)
	require.Equal(t, messaging.PlatformFeishu, a.Platform())
}

func TestAdapter_Start_MissingCredentials(t *testing.T) {
	t.Parallel()
	a := &Adapter{log: nil}
	err := a.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "appID and appSecret required")
}

func TestAdapter_Close(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		dedup:       make(map[string]time.Time),
		activeConns: make(map[string]*FeishuConn),
		dedupDone:   make(chan struct{}),
	}
	// Don't add a conn — just test that Close handles empty maps
	require.NoError(t, a.Close(context.Background()))
	require.Nil(t, a.activeConns)
	require.Nil(t, a.dedup)
}
