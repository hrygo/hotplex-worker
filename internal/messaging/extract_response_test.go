package messaging

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestExtractResponseText_MessageDeltaData(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "hello world"},
		},
	}
	text, ok := ExtractResponseText(env)
	require.True(t, ok)
	require.Equal(t, "hello world", text)
}

func TestExtractResponseText_MessageDeltaEmpty(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: ""},
		},
	}
	text, ok := ExtractResponseText(env)
	require.False(t, ok)
	require.Equal(t, "", text)
}

func TestExtractResponseText_MapWithContent(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.MessageDelta,
			Data: map[string]any{"content": "from map"},
		},
	}
	text, ok := ExtractResponseText(env)
	require.True(t, ok)
	require.Equal(t, "from map", text)
}

func TestExtractResponseText_MapWithText(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.MessageDelta,
			Data: map[string]any{"text": "from text key"},
		},
	}
	text, ok := ExtractResponseText(env)
	require.True(t, ok)
	require.Equal(t, "from text key", text)
}

func TestExtractResponseText_StringData(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: "text",
			Data: "raw string",
		},
	}
	text, ok := ExtractResponseText(env)
	require.True(t, ok)
	require.Equal(t, "raw string", text)
}

func TestExtractResponseText_RawData(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: "raw",
			Data: events.RawData{Raw: map[string]any{"text": "from raw"}},
		},
	}
	text, ok := ExtractResponseText(env)
	require.True(t, ok)
	require.Equal(t, "from raw", text)
}

func TestExtractResponseText_Done(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{Type: "done"},
	}
	text, ok := ExtractResponseText(env)
	require.False(t, ok)
	require.Equal(t, "", text)
}

func TestExtractResponseText_Nil(t *testing.T) {
	t.Parallel()

	text, ok := ExtractResponseText(nil)
	require.False(t, ok)
	require.Equal(t, "", text)
}

func TestExtractErrorMessage_ErrorData(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Data: events.ErrorData{Message: "something failed"},
		},
	}
	require.Equal(t, "something failed", ExtractErrorMessage(env))
}

func TestExtractErrorMessage_MapFallback(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Data: map[string]any{"message": "map error"},
		},
	}
	require.Equal(t, "map error", ExtractErrorMessage(env))
}

func TestExtractErrorMessage_Empty(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{Event: events.Event{Data: "string"}}
	require.Equal(t, "", ExtractErrorMessage(env))
}
