package feishu

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── messaging.ExtractResponseText: raw type ───────────────────────────────────────────

func TestExtractResponseText_RawType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data events.RawData
		want string
		ok   bool
	}{
		{
			name: "raw with text field",
			data: events.RawData{Raw: map[string]any{"text": "hello from raw"}},
			want: "hello from raw",
			ok:   true,
		},
		{
			name: "raw with non-map raw",
			data: events.RawData{Raw: "string raw"},
			want: "",
			ok:   false,
		},
		{
			name: "raw with no text field",
			data: events.RawData{Raw: map[string]any{"other": "field"}},
			want: "",
			ok:   false,
		},
		{
			name: "raw with empty text",
			data: events.RawData{Raw: map[string]any{"text": ""}},
			want: "",
			ok:   true, // empty string is still returned (Content != "" is checked)
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := &events.Envelope{Event: events.Event{Type: "raw", Data: tt.data}}
			got, ok := messaging.ExtractResponseText(env)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── messaging.ExtractResponseText: done type ─────────────────────────────────────────

func TestExtractResponseText_DoneType(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{Event: events.Event{Type: "done", Data: nil}}
	got, ok := messaging.ExtractResponseText(env)
	require.False(t, ok)
	require.Equal(t, "", got)
}

// ─── messaging.ExtractResponseText: message delta with map data ────────────────────────

func TestExtractResponseText_MessageDeltaMap(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{Event: events.Event{
		Type: "message.delta",
		Data: map[string]any{"content": "delta from map"},
	}}
	got, ok := messaging.ExtractResponseText(env)
	require.True(t, ok)
	require.Equal(t, "delta from map", got)
}

// ─── messaging.ExtractResponseText: text type with string data ─────────────────────────

func TestExtractResponseText_TextStringData(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{Event: events.Event{
		Type: "text",
		Data: "plain string text",
	}}
	got, ok := messaging.ExtractResponseText(env)
	require.True(t, ok)
	require.Equal(t, "plain string text", got)
}

// ─── messaging.ExtractResponseText: unknown type ─────────────────────────────────────

func TestExtractResponseText_UnknownType(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{Event: events.Event{
		Type: "unknown_event",
		Data: map[string]any{"content": "should not extract"},
	}}
	got, ok := messaging.ExtractResponseText(env)
	require.False(t, ok)
	require.Equal(t, "", got)
}
