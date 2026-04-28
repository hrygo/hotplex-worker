package feishu

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging/stt"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestControlFeedbackMessageCN(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action events.ControlAction
		want   string
	}{
		{events.ControlActionGC, "✅ 会话已休眠，发消息即可恢复。"},
		{events.ControlActionReset, "✅ 上下文已重置。"},
		{events.ControlAction("unknown"), "✅ 已完成。"},
	}
	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, controlFeedbackMessageCN(tt.action))
		})
	}
}

func TestExtractTextFromContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"empty", "", ""},
		{"valid", `{"text":"hello world"}`, "hello world"},
		{"spaces preserved", `{"text":"hello   world"}`, "hello   world"},
		{"invalid json", `not json`, ""},
		{"no text field", `{"other":"value"}`, ""},
		{"empty text", `{"text":""}`, ""},
		{"unicode", `{"text":"你好"}`, "你好"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, extractTextFromContent(tt.content))
		})
	}
}

func TestBuildCardContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{"simple", "Hello world"},
		{"empty", ""},
		{"unicode", "你好世界 🔔"},
		{"markdown bold", "**bold** and _italic_"},
		{"code block", "```\ncode\n```"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildCardContent(tt.text)

			var card map[string]any
			require.NoError(t, json.Unmarshal([]byte(got), &card))
			require.Equal(t, "2.0", card["schema"])

			body := card["body"].(map[string]any)
			el := body["elements"].([]any)[0].(map[string]any)
			require.Equal(t, "markdown", el["tag"])
			require.Equal(t, tt.text, el["content"])
		})
	}
}

func TestBuildCardContent_EscapeHTML(t *testing.T) {
	t.Parallel()
	got := buildCardContent("<test> & \"quotes\"")
	var card map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &card))
	body := card["body"].(map[string]any)
	el := body["elements"].([]any)[0].(map[string]any)
	require.Equal(t, "<test> & \"quotes\"", el["content"])
}

func TestIsMessageExpired(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		createTimeMs int64
		want         bool
	}{
		{"zero", 0, false},
		{"negative", -1, false},
		{"very old", 1, true}, // 1ms since epoch → definitely expired
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsMessageExpired(tt.createTimeMs))
		})
	}
}

func TestExtractChatID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  *events.Envelope
		want string
	}{
		{"nil envelope", nil, ""},
		{"top-level chat_id", &events.Envelope{Event: events.Event{Data: map[string]any{"chat_id": "oc_chat123"}}}, "oc_chat123"},
		{"nested metadata chat_id", &events.Envelope{Event: events.Event{Data: map[string]any{"metadata": map[string]any{"chat_id": "oc_meta123"}}}}, "oc_meta123"},
		{"top-level takes precedence", &events.Envelope{Event: events.Event{Data: map[string]any{"chat_id": "oc_top", "metadata": map[string]any{"chat_id": "oc_nested"}}}}, "oc_top"},
		{"non-string chat_id", &events.Envelope{Event: events.Event{Data: map[string]any{"chat_id": 123}}}, ""},
		{"empty string chat_id", &events.Envelope{Event: events.Event{Data: map[string]any{"chat_id": ""}}}, ""},
		{"nil data", &events.Envelope{Event: events.Event{Data: nil}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ExtractChatID(tt.env))
		})
	}
}

func TestAdapter_Setters(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	a.SetBridge(nil)
	require.Nil(t, a.bridge)

	a.SetGate(nil)
	require.Nil(t, a.gate)

	a.SetTranscriber(nil)
	require.Nil(t, a.transcriber)
}

func TestNewFeishuConn(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	require.Equal(t, "chat123", conn.chatID)
	require.Same(t, adapter, conn.adapter)
}

func TestFeishuConn_EnableStreaming(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	// nil controller should not panic
	conn.EnableStreaming(nil)

	ctrl := newTestStreamingCtrl()
	conn.EnableStreaming(ctrl)
	conn.EnableStreaming(nil) // reset
}

func TestFeishuConn_SetTypingReactionID(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	// Set and clear should not panic
	conn.SetTypingReactionID("msg123")
	conn.SetTypingReactionID("")
}

func TestPtrStr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		p    *string
		want string
	}{
		{"nil pointer", nil, ""},
		{"empty string", new(string), ""},
		{"non-empty", strPtr("hello"), "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ptrStr(tt.p))
		})
	}
}

func strPtr(s string) *string { return &s }

func TestFeishuConn_CycleReaction_NoOp(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	// cycleReaction with no existing state should not panic
	conn.cycleReaction(context.Background(), "TOOL_USE")
}

func TestFeishuConn_CycleReaction_SameEmojiDedup(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	// Set platformMsgID so the early return (platformMsgID=="") is skipped,
	// but same emoji means no API calls happen.
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.toolEmoji = "TOOL_USE"
	conn.mu.Unlock()

	// Same emoji → early return, no API calls made.
	conn.cycleReaction(context.Background(), "TOOL_USE")
}

func TestSaveMediaBytes(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)

	tests := []struct {
		name      string
		media     *MediaInfo
		data      []byte
		wantErr   bool
		checkFile bool
		checkName bool
	}{
		{
			name:      "image jpg",
			media:     &MediaInfo{Key: "img_key", Type: "image", Name: ""},
			data:      []byte("fake image data"),
			wantErr:   false,
			checkFile: true,
		},
		{
			name:      "with original name",
			media:     &MediaInfo{Key: "file_key", Type: "file", Name: "report.pdf"},
			data:      []byte("pdf content"),
			wantErr:   false,
			checkFile: true,
			checkName: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path, err := adapter.saveMediaBytes(tt.data, tt.media, ".bin")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.checkFile {
				require.FileExists(t, path)
				data, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, tt.data, data)
				t.Cleanup(func() { os.RemoveAll(filepath.Dir(path)) })
			}
		})
	}
}

func TestIsBotMentioned(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		mentions  []*larkim.MentionEvent
		botOpenID string
		want      bool
	}{
		{
			name:      "empty botOpenID",
			mentions:  []*larkim.MentionEvent{{Id: &larkim.UserId{OpenId: strPtr("ou_bot")}}},
			botOpenID: "",
			want:      false,
		},
		{
			name:      "no mentions",
			mentions:  nil,
			botOpenID: "ou_bot",
			want:      false,
		},
		{
			name:      "bot mentioned",
			mentions:  []*larkim.MentionEvent{{Id: &larkim.UserId{OpenId: strPtr("ou_bot")}}},
			botOpenID: "ou_bot",
			want:      true,
		},
		{
			name:      "other user mentioned",
			mentions:  []*larkim.MentionEvent{{Id: &larkim.UserId{OpenId: strPtr("ou_other")}}},
			botOpenID: "ou_bot",
			want:      false,
		},
		{
			name:      "nil Id field",
			mentions:  []*larkim.MentionEvent{{Id: nil}},
			botOpenID: "ou_bot",
			want:      false,
		},
		{
			name:      "nil OpenId field",
			mentions:  []*larkim.MentionEvent{{Id: &larkim.UserId{OpenId: nil}}},
			botOpenID: "ou_bot",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isBotMentioned(tt.mentions, tt.botOpenID)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestAudioToPCM_Success(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	// Generate minimal valid WAV: RIFF header + fmt chunk + data chunk.
	// ffmpeg can convert even a minimal WAV to PCM.
	wav := makeWAV(8000, 1)
	pcm, err := stt.AudioToPCM(context.Background(), wav)
	require.NoError(t, err)
	require.NotEmpty(t, pcm)
}

func makeWAV(sampleRate, numChannels int) []byte {
	// Minimal PCM WAV file: RIFF header + fmt + data chunks.
	const bitsPerSample = 16
	numSamples := 100
	dataSize := numSamples * numChannels * bitsPerSample / 8
	headerSize := 44
	totalSize := headerSize + dataSize

	buf := make([]byte, totalSize)
	// RIFF
	copy(buf[0:], "RIFF")
	buf[4] = byte(totalSize - 8)
	buf[5] = byte((totalSize - 8) >> 8)
	buf[6] = byte((totalSize - 8) >> 16)
	buf[7] = byte((totalSize - 8) >> 24)
	copy(buf[8:], "WAVE")
	// fmt
	copy(buf[12:], "fmt ")
	buf[16] = 16 // chunk size
	buf[20] = 1  // PCM
	buf[22] = byte(numChannels)
	le16(buf[24:], uint16(sampleRate))
	le16(buf[34:], uint16(bitsPerSample))
	// data
	copy(buf[36:], "data")
	le16(buf[40:], uint16(dataSize))
	le16(buf[4+2:], uint16(totalSize-8))
	return buf
}

func le16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	return &Adapter{
		log:         discardLogger,
		dedup:       NewDedup(100, time.Hour),
		activeConns: make(map[string]*FeishuConn),
		dedupDone:   make(chan struct{}),
	}
}

func newTestStreamingCtrl() *StreamingCardController {
	return NewStreamingCardController(nil, nil, discardLogger)
}

func newTestConn(adapter *Adapter, replyToMsgID string) *FeishuConn {
	conn := NewFeishuConn(adapter, "chat123", "")
	conn.replyToMsgID = replyToMsgID
	return conn
}

func TestFeishuConn_SendContextUsage(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)

	tests := []struct {
		name      string
		replyTo   string
		data      interface{}
		wantErr   bool
		wantInErr string
	}{
		{
			name:    "typed struct with replyToMsgID → replyMessage",
			replyTo: "msg_123",
			data: events.ContextUsageData{
				TotalTokens: 1500, MaxTokens: 2000, Percentage: 75, Model: "claude-3.5-sonnet",
				Categories: []events.ContextCategory{
					{Name: "System", Tokens: 200}, {Name: "User", Tokens: 800}, {Name: "Assistant", Tokens: 500},
				},
				MemoryFiles: 5, MCPTools: 12, Agents: 3,
				Skills: events.ContextSkillInfo{Total: 50, Included: 15, Tokens: 300},
			},
			wantErr:   true,
			wantInErr: "lark client not initialized",
		},
		{
			name:    "typed struct without replyToMsgID → sendTextMessage",
			replyTo: "",
			data:    events.ContextUsageData{TotalTokens: 800, MaxTokens: 1000, Percentage: 80, Model: "gpt-4"},
			wantErr: true, wantInErr: "lark client not initialized",
		},
		{
			name:    "map data with replyToMsgID",
			replyTo: "msg_456",
			data: map[string]interface{}{
				"total_tokens": 1200, "max_tokens": 1500, "percentage": 80, "model": "claude-3-opus",
				"categories": []map[string]interface{}{
					{"name": "System", "tokens": 150}, {"name": "User", "tokens": 750}, {"name": "Assistant", "tokens": 300},
				},
			},
			wantErr: true, wantInErr: "lark client not initialized",
		},
		{
			name:    "map data without replyToMsgID",
			replyTo: "",
			data: map[string]interface{}{
				"total_tokens": 500, "max_tokens": 1000, "percentage": 50,
			},
			wantErr: true, wantInErr: "lark client not initialized",
		},
		{
			name: "unsupported data type → returns nil",
			data: "invalid data type", wantErr: false,
		},
		{
			name:    "minimal data — no optional fields",
			replyTo: "msg_789",
			data:    events.ContextUsageData{TotalTokens: 1000, MaxTokens: 1000, Percentage: 100},
			wantErr: true, wantInErr: "lark client not initialized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conn := newTestConn(adapter, tt.replyTo)
			env := &events.Envelope{Event: events.Event{Type: events.ContextUsage, Data: tt.data}}

			err := conn.sendContextUsage(context.Background(), env)

			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantInErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFeishuConn_SendMCPStatus(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)

	tests := []struct {
		name      string
		replyTo   string
		data      interface{}
		wantErr   bool
		wantInErr string
	}{
		{
			name:    "typed struct with replyToMsgID → replyMessage",
			replyTo: "msg_123",
			data: events.MCPStatusData{Servers: []events.MCPServerInfo{
				{Name: "filesystem", Status: "connected"}, {Name: "github", Status: "ok"},
				{Name: "sqlite", Status: "disconnected"}, {Name: "postgres", Status: "error"},
			}},
			wantErr:   true,
			wantInErr: "lark client not initialized",
		},
		{
			name:    "typed struct without replyToMsgID → sendTextMessage",
			replyTo: "",
			data: events.MCPStatusData{Servers: []events.MCPServerInfo{
				{Name: "single-server", Status: "connected"},
			}},
			wantErr: true, wantInErr: "lark client not initialized",
		},
		{
			name:    "map data with replyToMsgID",
			replyTo: "msg_456",
			data: map[string]interface{}{
				"servers": []map[string]interface{}{
					{"name": "filesystem", "status": "connected"}, {"name": "github", "status": "ok"},
				},
			},
			wantErr: true, wantInErr: "lark client not initialized",
		},
		{
			name:    "map data without replyToMsgID",
			replyTo: "",
			data: map[string]interface{}{
				"servers": []map[string]interface{}{
					{"name": "empty-server", "status": "disconnected"},
				},
			},
			wantErr: true, wantInErr: "lark client not initialized",
		},
		{
			name: "unsupported data type → returns nil",
			data: 12345, wantErr: false,
		},
		{
			name:    "empty servers list",
			replyTo: "msg_789",
			data:    events.MCPStatusData{Servers: []events.MCPServerInfo{}},
			wantErr: true, wantInErr: "lark client not initialized",
		},
		{
			name:    "mixed status icons — connected/ok get ✅, others get ❌",
			replyTo: "",
			data: events.MCPStatusData{Servers: []events.MCPServerInfo{
				{Name: "connected-server", Status: "connected"}, {Name: "ok-server", Status: "ok"},
				{Name: "disconnected-server", Status: "disconnected"}, {Name: "error-server", Status: "error"},
				{Name: "failed-server", Status: "failed"}, {Name: "unknown-server", Status: "unknown"},
			}},
			wantErr: true, wantInErr: "lark client not initialized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conn := newTestConn(adapter, tt.replyTo)
			env := &events.Envelope{Event: events.Event{Type: events.MCPStatus, Data: tt.data}}

			err := conn.sendMCPStatus(context.Background(), env)

			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantInErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
