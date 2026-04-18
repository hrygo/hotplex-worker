package feishu

import (
	"context"
	"io"
	"log/slog"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/messaging"
	"github.com/hotplex/hotplex-worker/pkg/events"
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
			Type: events.MessageDelta,
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
	adapter := &Adapter{log: slog.New(slog.NewTextHandler(io.Discard, nil)), dedup: NewDedup(100, 12*60*60*1e9), activeConns: make(map[string]*FeishuConn), dedupDone: make(chan struct{})}
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
		dedup:       NewDedup(100, 12*60*60*1e9),
		activeConns: make(map[string]*FeishuConn),
		dedupDone:   make(chan struct{}),
	}
	require.NoError(t, a.Close(context.Background()))
	require.Nil(t, a.activeConns)
	require.Nil(t, a.dedup)
}

func TestDedup_TryRecord(t *testing.T) {
	t.Parallel()
	d := NewDedup(100, 12*60*60*1e9)
	require.True(t, d.TryRecord("msg1"))
	require.False(t, d.TryRecord("msg1"))
	require.True(t, d.TryRecord("msg2"))
}

func TestDedup_FIFOEviction(t *testing.T) {
	t.Parallel()
	d := NewDedup(2, 12*60*60*1e9)
	require.True(t, d.TryRecord("a"))
	require.True(t, d.TryRecord("b"))
	require.True(t, d.TryRecord("c"))  // evicts "a"
	require.True(t, d.TryRecord("a"))  // re-accepted after eviction
	require.False(t, d.TryRecord("a")) // duplicate
}

func TestIsAbortCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{"stop", true},
		{"Stop", true},
		{"Stop.", true},
		{"stop!", true},
		{"取消", true},
		{"please stop", true},
		{"hello", false},
		{"stopping", false},
		{"stopped", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.want, IsAbortCommand(tt.input))
		})
	}
}

func TestResolveMentions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		mentions []*mentionStub
		botID    string
		want     string
	}{
		{
			name:  "no mentions",
			text:  "hello world",
			botID: "",
			want:  "hello world",
		},
		{
			name:     "replace user mention",
			text:     "hello @_user_1 there",
			mentions: []*mentionStub{{key: "@_user_1", openID: "ou_123", name: "Alice"}},
			botID:    "",
			want:     "hello @Alice there",
		},
		{
			name:     "strip bot self-mention",
			text:     "@_user_1 @_user_2",
			mentions: []*mentionStub{{key: "@_user_1", openID: "bot_abc", name: "Bot"}, {key: "@_user_2", openID: "ou_456", name: "Bob"}},
			botID:    "bot_abc",
			want:     "@Bob",
		},
		{
			name:     "strip bot self-mention preserves surrounding text",
			text:     "hey @_user_1 @_user_2",
			mentions: []*mentionStub{{key: "@_user_1", openID: "bot_abc", name: "Bot"}, {key: "@_user_2", openID: "ou_456", name: "Bob"}},
			botID:    "bot_abc",
			want:     "hey @Bob",
		},
		{
			name:     "preserve @_all",
			text:     "@_all hello",
			mentions: []*mentionStub{},
			botID:    "",
			want:     "@_all hello",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var larkMentions []*larkim.MentionEvent
			for _, m := range tt.mentions {
				larkMentions = append(larkMentions, &larkim.MentionEvent{
					Key:  &m.key,
					Id:   &larkim.UserId{OpenId: &m.openID},
					Name: &m.name,
				})
			}
			got := ResolveMentions(tt.text, larkMentions, tt.botID)
			require.Equal(t, tt.want, got)
		})
	}
}

type mentionStub struct {
	key    string
	openID string
	name   string
}
