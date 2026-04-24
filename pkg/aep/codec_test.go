package aep

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestNewID(t *testing.T) {
	t.Parallel()

	id := NewID()
	require.True(t, strings.HasPrefix(id, "evt_"))
	require.Len(t, id, len("evt_")+36) // uuid is 36 chars
}

func TestNewSessionID(t *testing.T) {
	t.Parallel()

	id := NewSessionID()
	require.True(t, strings.HasPrefix(id, "sess_"))
	require.Len(t, id, len("sess_")+36)
}

func TestNewID_Uniqueness(t *testing.T) {
	t.Parallel()

	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := NewID()
		require.False(t, ids[id], "duplicate ID generated: %s", id)
		ids[id] = true
	}
}

func TestEncodeDecode(t *testing.T) {
	t.Parallel()

	env := NewEnvelope(
		NewID(),
		"sess_test",
		42,
		events.State,
		events.StateData{State: events.StateRunning},
	)

	var sb strings.Builder
	err := Encode(&sb, env)
	require.NoError(t, err)

	decoded, err := Decode(strings.NewReader(sb.String()))
	require.NoError(t, err)
	require.Equal(t, env.ID, decoded.ID)
	require.Equal(t, env.Seq, decoded.Seq)
	require.Equal(t, env.Event.Type, decoded.Event.Type)
}

func TestEncodeChunk(t *testing.T) {
	t.Parallel()

	env := NewEnvelope(
		NewID(),
		"sess_chunk",
		1,
		events.Input,
		events.InputData{Content: "hello"},
	)

	var sb strings.Builder
	err := EncodeChunk(&sb, env)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(sb.String(), "\n"))

	// Decode and verify
	decoded, err := Decode(strings.NewReader(sb.String()))
	require.NoError(t, err)
	require.Equal(t, env.SessionID, decoded.SessionID)
}

func TestDecode_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := Decode(strings.NewReader(`{invalid}`))
	require.Error(t, err)
}

func TestDecodeLine_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := DecodeLine([]byte(`not json`))
	require.Error(t, err)
}

func TestValidate_MissingVersion(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		ID:        NewID(),
		Seq:       1,
		SessionID: "sess_123",
		Timestamp: 1700000000000,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "version")
}

func TestValidate_MissingID(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		Seq:       1,
		SessionID: "sess_123",
		Timestamp: 1700000000000,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "id")
}

func TestValidate_MissingSessionID(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		ID:        NewID(),
		Seq:       1,
		Timestamp: 1700000000000,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "session_id")
}

func TestValidate_NonPositiveSeq(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		ID:        NewID(),
		Seq:       0,
		SessionID: "sess_123",
		Timestamp: 1700000000000,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "seq")
}

func TestValidate_NonPositiveTimestamp(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		ID:        NewID(),
		Seq:       1,
		SessionID: "sess_123",
		Timestamp: 0,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timestamp")
}

func TestValidate_MissingEventType(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		ID:        NewID(),
		Seq:       1,
		SessionID: "sess_123",
		Timestamp: 1700000000000,
		Event:     events.Event{Type: "", Data: nil},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "event.kind")
}

func TestValidate_ValidEnvelope(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		ID:        NewID(),
		Seq:       1,
		SessionID: "sess_123",
		Timestamp: 1700000000000,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.NoError(t, err)
}

func TestEncodeJSON(t *testing.T) {
	t.Parallel()

	env := NewEnvelope(
		NewID(),
		"sess_json",
		1,
		events.Done,
		events.DoneData{Success: true},
	)

	data, err := EncodeJSON(env)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.True(t, strings.HasPrefix(string(data), `{"version":"aep/v1"`))
}

func TestMustMarshal(t *testing.T) {
	t.Parallel()

	env := NewEnvelope(NewID(), "sess_must", 1, events.Input, events.InputData{Content: "test"})
	data := MustMarshal(env)
	require.NotEmpty(t, data)
}

func TestIsSessionBusy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  *events.Envelope
		want bool
	}{
		{
			name: "session busy error",
			env: NewEnvelope(
				NewID(), "sess_123", 1, events.Error,
				map[string]any{"code": string(events.ErrCodeSessionBusy), "message": "busy"},
			),
			want: true,
		},
		{
			name: "other error",
			env: NewEnvelope(
				NewID(), "sess_123", 1, events.Error,
				map[string]any{"code": string(events.ErrCodeInternalError), "message": "internal"},
			),
			want: false,
		},
		{
			name: "not an error event",
			env: NewEnvelope(
				NewID(), "sess_123", 1, events.Input,
				events.InputData{Content: "hello"},
			),
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsSessionBusy(tt.env)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIsTerminalEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind     events.Kind
		terminal bool
	}{
		{events.Done, true},
		{events.Error, true},
		{events.Input, false},
		{events.State, false},
		{events.ToolCall, false},
		{events.Ping, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.terminal, IsTerminalEvent(tt.kind))
		})
	}
}

func TestParseSessionID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"sess_abc123", "abc123"},
		{"abc123", "abc123"},
		{"sess_", ""},
		{"", ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := ParseSessionID(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSeqKey(t *testing.T) {
	t.Parallel()

	key := SeqKey("sess_123", "evt_abc")
	require.Equal(t, "sess_123:evt_abc", key)
}

func TestEscapeJSTerminators(t *testing.T) {
	t.Parallel()

	// Use rune literals (\u2028 / \u2029) so Go creates actual Unicode codepoints.
	tests := []struct {
		name  string
		input string // Go rune literals produce real U+2028/U+2029 bytes
		want  string
	}{
		{
			name:  "no terminators",
			input: `{"text":"hello world"}`,
			want:  `{"text":"hello world"}`,
		},
		{
			name:  "line separator U+2028",
			input: "text\u2028end", // actual U+2028 byte
			want:  "text\\u2028end",
		},
		{
			name:  "paragraph separator U+2029",
			input: "text\u2029end", // actual U+2029 byte
			want:  "text\\u2029end",
		},
		{
			name:  "both terminators",
			input: "a\u2028b\u2029c",
			want:  "a\\u2028b\\u2029c",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := string(escapeJSTerminators([]byte(tt.input)))
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEncode_NDJSONSafe(t *testing.T) {
	t.Parallel()

	// Content containing U+2028 (JS line separator) in the message.
	// Claude Code might emit this in thinking/rendering content.
	env := NewEnvelope(
		NewID(),
		"sess_ndjson",
		1,
		events.MessageDelta,
		map[string]any{"content": "line1\u2028line2"}, // U+2028 embedded
	)

	var sb strings.Builder
	err := Encode(&sb, env)
	require.NoError(t, err)

	// Output must NOT contain raw U+2028 bytes — they must be \u2028 escaped.
	output := sb.String()
	require.NotContains(t, output, "\xe2\x80\xa8", "raw U+2028 must not appear in output")
	require.Contains(t, output, `\u2028`, "U+2028 must be escaped as \\u2028")

	// Must still decode correctly.
	decoded, err := Decode(strings.NewReader(output))
	require.NoError(t, err)
	require.Equal(t, env.SessionID, decoded.SessionID)
}

func TestEncodeChunk_NDJSONSafe(t *testing.T) {
	t.Parallel()

	env := NewEnvelope(
		NewID(),
		"sess_chunk_ndjson",
		1,
		events.MessageDelta,
		map[string]any{"content": "para1\u2029para2"}, // U+2029 embedded
	)

	var sb strings.Builder
	err := EncodeChunk(&sb, env)
	require.NoError(t, err)

	output := sb.String()
	require.NotContains(t, output, "\xe2\x80\xa9", "raw U+2029 must not appear in output")
	require.Contains(t, output, `\u2029`, "U+2029 must be escaped as \\u2029")

	decoded, err := Decode(strings.NewReader(output))
	require.NoError(t, err)
	require.Equal(t, env.SessionID, decoded.SessionID)
}

func TestEncodeJSON_NDJSONSafe(t *testing.T) {
	t.Parallel()

	// Use actual Unicode codepoints (rune literals) so json.Marshal processes them.
	env := NewEnvelope(
		NewID(),
		"sess_json_ndjson",
		1,
		events.Input,
		events.InputData{Content: "test\u2028with\u2029separators"},
	)

	data, err := EncodeJSON(env)
	require.NoError(t, err)

	// Verify: raw UTF-8 bytes for U+2028/U+2029 must not appear.
	require.NotContains(t, data, []byte{0xE2, 0x80, 0xA8})
	require.NotContains(t, data, []byte{0xE2, 0x80, 0xA9})

	// Verify: output is valid JSON and content round-trips correctly.
	var decoded struct {
		Version string `json:"version"`
		Event   struct {
			Type string `json:"type"`
			Data struct {
				Content string `json:"content"`
			} `json:"data"`
		} `json:"event"`
	}
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, "input", decoded.Event.Type)
	// The U+2028 and U+2029 codepoints round-trip through JSON marshaling.
	require.Contains(t, decoded.Event.Data.Content, "\u2028")
	require.Contains(t, decoded.Event.Data.Content, "\u2029")
}
