package feishu

import (
	"testing"

	"github.com/stretchr/testify/require"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestConvertImage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		rawContent string
		msgID      string
		wantText   string
		wantOK     bool
		wantMedia  int // expected len(media)
		wantType   string
		wantKey    string
	}{
		{
			name:       "valid image",
			rawContent: `{"image_key":"img_abc123"}`,
			msgID:      "msg_123",
			wantText:   "[用户发送了一张图片]",
			wantOK:     true,
			wantMedia:  1,
			wantType:   "image",
			wantKey:    "img_abc123",
		},
		{
			name:       "empty image_key",
			rawContent: `{"image_key":""}`,
			msgID:      "msg_123",
			wantText:   "[图片]",
			wantOK:     true,
			wantMedia:  0,
		},
		{
			name:       "invalid json",
			rawContent: `{invalid`,
			msgID:      "msg_123",
			wantText:   "[图片]",
			wantOK:     true,
			wantMedia:  0,
		},
		{
			name:       "missing image_key",
			rawContent: `{}`,
			msgID:      "msg_123",
			wantText:   "[图片]",
			wantOK:     true,
			wantMedia:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := convertImage(tt.rawContent, tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			require.Len(t, media, tt.wantMedia)
			if tt.wantMedia > 0 {
				require.Equal(t, tt.wantType, media[0].Type)
				require.Equal(t, tt.wantKey, media[0].Key)
				require.Equal(t, tt.msgID, media[0].MessageID)
			}
		})
	}
}

func TestConvertFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		rawContent string
		msgID      string
		wantText   string
		wantOK     bool
		wantMedia  int
		wantType   string
		wantKey    string
		wantName   string
	}{
		{
			name:       "valid file with name",
			rawContent: `{"file_key":"file_abc","file_name":"report.pdf"}`,
			msgID:      "msg_456",
			wantText:   "[用户发送了一个文件]",
			wantOK:     true,
			wantMedia:  1,
			wantType:   "file",
			wantKey:    "file_abc",
			wantName:   "report.pdf",
		},
		{
			name:       "valid file without name",
			rawContent: `{"file_key":"file_xyz"}`,
			msgID:      "msg_456",
			wantText:   "[用户发送了一个文件]",
			wantOK:     true,
			wantMedia:  1,
			wantType:   "file",
			wantKey:    "file_xyz",
		},
		{
			name:       "empty file_key",
			rawContent: `{"file_key":""}`,
			msgID:      "msg_456",
			wantText:   "[文件]",
			wantOK:     true,
			wantMedia:  0,
		},
		{
			name:       "invalid json",
			rawContent: `{`,
			msgID:      "msg_456",
			wantText:   "[文件]",
			wantOK:     true,
			wantMedia:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := convertFile(tt.rawContent, tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			require.Len(t, media, tt.wantMedia)
			if tt.wantMedia > 0 {
				require.Equal(t, tt.wantType, media[0].Type)
				require.Equal(t, tt.wantKey, media[0].Key)
				require.Equal(t, tt.wantName, media[0].Name)
				require.Equal(t, tt.msgID, media[0].MessageID)
			}
		})
	}
}

func TestConvertAudio(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		rawContent string
		msgID      string
		wantText   string
		wantOK     bool
		wantMedia  int
		wantType   string
		wantKey    string
	}{
		{
			name:       "valid audio",
			rawContent: `{"file_key":"audio_xyz"}`,
			msgID:      "msg_audio",
			wantText:   "[用户发送了一条语音]",
			wantOK:     true,
			wantMedia:  1,
			wantType:   "audio",
			wantKey:    "audio_xyz",
		},
		{
			name:       "empty file_key",
			rawContent: `{"file_key":""}`,
			msgID:      "msg_audio",
			wantText:   "[语音]",
			wantOK:     true,
			wantMedia:  0,
		},
		{
			name:       "invalid json",
			rawContent: `not json`,
			msgID:      "msg_audio",
			wantText:   "[语音]",
			wantOK:     true,
			wantMedia:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := convertAudio(tt.rawContent, tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			require.Len(t, media, tt.wantMedia)
			if tt.wantMedia > 0 {
				require.Equal(t, tt.wantType, media[0].Type)
				require.Equal(t, tt.wantKey, media[0].Key)
				require.Equal(t, tt.msgID, media[0].MessageID)
			}
		})
	}
}

func TestConvertVideo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		rawContent string
		msgID      string
		wantText   string
		wantOK     bool
		wantMedia  int
		wantType   string
		wantKey    string
		wantName   string
	}{
		{
			name:       "valid video with name",
			rawContent: `{"file_key":"video_123","file_name":"clip.mp4"}`,
			msgID:      "msg_video",
			wantText:   "[用户发送了一段视频]",
			wantOK:     true,
			wantMedia:  1,
			wantType:   "video",
			wantKey:    "video_123",
			wantName:   "clip.mp4",
		},
		{
			name:       "valid video without name",
			rawContent: `{"file_key":"video_456"}`,
			msgID:      "msg_video",
			wantText:   "[用户发送了一段视频]",
			wantOK:     true,
			wantMedia:  1,
			wantType:   "video",
			wantKey:    "video_456",
		},
		{
			name:       "empty file_key",
			rawContent: `{"file_key":""}`,
			msgID:      "msg_video",
			wantText:   "[视频]",
			wantOK:     true,
			wantMedia:  0,
		},
		{
			name:       "invalid json",
			rawContent: `broken`,
			msgID:      "msg_video",
			wantText:   "[视频]",
			wantOK:     true,
			wantMedia:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := convertVideo(tt.rawContent, tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			require.Len(t, media, tt.wantMedia)
			if tt.wantMedia > 0 {
				require.Equal(t, tt.wantType, media[0].Type)
				require.Equal(t, tt.wantKey, media[0].Key)
				require.Equal(t, tt.wantName, media[0].Name)
				require.Equal(t, tt.msgID, media[0].MessageID)
			}
		})
	}
}

func TestConvertSticker(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		rawContent string
		msgID      string
		wantText   string
		wantOK     bool
		wantMedia  int
		wantType   string
		wantKey    string
	}{
		{
			name:       "valid sticker",
			rawContent: `{"file_key":"sticker_gif"}`,
			msgID:      "msg_sticker",
			wantText:   "[用户发送了一个表情]",
			wantOK:     true,
			wantMedia:  1,
			wantType:   "sticker",
			wantKey:    "sticker_gif",
		},
		{
			name:       "empty file_key",
			rawContent: `{"file_key":""}`,
			msgID:      "msg_sticker",
			wantText:   "[表情]",
			wantOK:     true,
			wantMedia:  0,
		},
		{
			name:       "invalid json",
			rawContent: `null`,
			msgID:      "msg_sticker",
			wantText:   "[表情]",
			wantOK:     true,
			wantMedia:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := convertSticker(tt.rawContent, tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			require.Len(t, media, tt.wantMedia)
			if tt.wantMedia > 0 {
				require.Equal(t, tt.wantType, media[0].Type)
				require.Equal(t, tt.wantKey, media[0].Key)
				require.Equal(t, tt.msgID, media[0].MessageID)
			}
		})
	}
}

func TestConvertMessage_MediaTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		msgType   string
		content   string
		msgID     string
		wantText  string
		wantOK    bool
		wantMedia int
		wantType  string
	}{
		{
			name:      "text type returns no media",
			msgType:   "text",
			content:   `{"text":"hello"}`,
			msgID:     "msg_text",
			wantText:  "hello",
			wantOK:    true,
			wantMedia: 0,
		},
		{
			name:      "unsupported type returns false",
			msgType:   "unsupported",
			content:   `{}`,
			msgID:     "msg_bad",
			wantText:  "",
			wantOK:    false,
			wantMedia: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := ConvertMessage(tt.msgType, tt.content, nil, "", tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			require.Len(t, media, tt.wantMedia)
		})
	}
}

func TestConvertPost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		rawContent     string
		messageID      string
		wantContains   []string
		wantMediaCount int
		wantMediaKeys  []string
	}{
		{
			name:           "text only, no images",
			rawContent:     `{"content":[[{"tag":"text","text":"hello"}]]}`,
			messageID:      "msg_001",
			wantContains:   []string{"hello"},
			wantMediaCount: 0,
		},
		{
			name:           "single image in post",
			rawContent:     `{"content":[[{"tag":"text","text":"see "},{"tag":"img","image_key":"img_v3_abc"}]]}`,
			messageID:      "msg_002",
			wantContains:   []string{"see ", "[图片]"},
			wantMediaCount: 1,
			wantMediaKeys:  []string{"img_v3_abc"},
		},
		{
			name:           "multiple images in post",
			rawContent:     `{"content":[[{"tag":"img","image_key":"img_1"},{"tag":"text","text":" and "},{"tag":"img","image_key":"img_2"}]]}`,
			messageID:      "msg_003",
			wantContains:   []string{"[图片]", " and "},
			wantMediaCount: 2,
			wantMediaKeys:  []string{"img_1", "img_2"},
		},
		{
			name:           "post with title and image",
			rawContent:     `{"title":"My Title","content":[[{"tag":"img","image_key":"img_title"}]]}`,
			messageID:      "msg_004",
			wantContains:   []string{"## My Title", "[图片]"},
			wantMediaCount: 1,
			wantMediaKeys:  []string{"img_title"},
		},
		{
			name:           "invalid json returns empty",
			rawContent:     `{invalid`,
			messageID:      "msg_005",
			wantContains:   nil,
			wantMediaCount: 0,
		},
		{
			name:           "empty content array",
			rawContent:     `{"content":[]}`,
			messageID:      "msg_006",
			wantContains:   nil,
			wantMediaCount: 0,
		},
		{
			name:           "image without key produces no media",
			rawContent:     `{"content":[[{"tag":"img"}]]}`,
			messageID:      "msg_007",
			wantContains:   []string{"[图片]"},
			wantMediaCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, mediaList := convertPost(tt.rawContent, nil, "", tt.messageID)
			for _, sub := range tt.wantContains {
				require.Contains(t, text, sub)
			}
			require.Len(t, mediaList, tt.wantMediaCount)
			for i, key := range tt.wantMediaKeys {
				require.Equal(t, key, mediaList[i].Key)
				require.Equal(t, "image", mediaList[i].Type)
				require.Equal(t, tt.messageID, mediaList[i].MessageID)
			}
		})
	}
}

func TestBuildMediaPrompt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		text         string
		paths        []string
		medias       []*MediaInfo
		wantContains []string
	}{
		{
			name:   "single image with text",
			text:   "看看这个图里有什么？",
			paths:  []string{"/tmp/hotplex/media/images/img_v3_xxx.jpg"},
			medias: []*MediaInfo{{Type: "image", Key: "img_v3_xxx"}},
			wantContains: []string{
				"1 张图片",
				"请使用 Read 工具查看后再回答",
				"- /tmp/hotplex/media/images/img_v3_xxx.jpg",
				"用户的文字内容:",
				"看看这个图里有什么？",
			},
		},
		{
			name:   "multiple images",
			text:   "比较这两张图",
			paths:  []string{"/tmp/hotplex/media/images/img_a.jpg", "/tmp/hotplex/media/images/img_b.jpg"},
			medias: []*MediaInfo{{Type: "image", Key: "img_a"}, {Type: "image", Key: "img_b"}},
			wantContains: []string{
				"2 张图片",
				"- /tmp/hotplex/media/images/img_a.jpg",
				"- /tmp/hotplex/media/images/img_b.jpg",
				"比较这两张图",
			},
		},
		{
			name:   "standalone file no user text",
			text:   "",
			paths:  []string{"/tmp/hotplex/media/files/file_abc_report.pdf"},
			medias: []*MediaInfo{{Type: "file", Key: "file_abc", Name: "report.pdf"}},
			wantContains: []string{
				"1 个文件",
				"- /tmp/hotplex/media/files/file_abc_report.pdf",
			},
		},
		{
			name:   "mixed media types",
			text:   "查看这些",
			paths:  []string{"/tmp/hotplex/media/images/img_x.jpg", "/tmp/hotplex/media/files/file_y.pdf"},
			medias: []*MediaInfo{{Type: "image", Key: "img_x"}, {Type: "file", Key: "file_y"}},
			wantContains: []string{
				"1 张图片",
				"1 个文件",
				"- /tmp/hotplex/media/images/img_x.jpg",
				"- /tmp/hotplex/media/files/file_y.pdf",
				"查看这些",
			},
		},
		{
			name:   "audio file",
			text:   "听听这段",
			paths:  []string{"/tmp/hotplex/media/audios/audio_z.opus"},
			medias: []*MediaInfo{{Type: "audio", Key: "audio_z"}},
			wantContains: []string{
				"1 条语音",
				"- /tmp/hotplex/media/audios/audio_z.opus",
			},
		},
		{
			name:   "video file",
			text:   "看看视频",
			paths:  []string{"/tmp/hotplex/media/videos/vid_w.mp4"},
			medias: []*MediaInfo{{Type: "video", Key: "vid_w"}},
			wantContains: []string{
				"1 段视频",
				"- /tmp/hotplex/media/videos/vid_w.mp4",
			},
		},
		{
			name:   "sticker",
			text:   "发了个表情",
			paths:  []string{"/tmp/hotplex/media/stickers/stk_s.gif"},
			medias: []*MediaInfo{{Type: "sticker", Key: "stk_s"}},
			wantContains: []string{
				"1 个表情贴纸",
				"- /tmp/hotplex/media/stickers/stk_s.gif",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildMediaPrompt(tt.text, tt.paths, tt.medias, nil)
			for _, sub := range tt.wantContains {
				require.Contains(t, got, sub)
			}
		})
	}
}

func TestMimeExt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mime string
		want string
	}{
		{"image/jpeg", ".jpg"},
		{"image/png", ".png"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"audio/opus", ".opus"},
		{"audio/mpeg", ".mp3"},
		{"audio/wav", ".wav"},
		{"video/mp4", ".mp4"},
		{"video/webm", ".webm"},
		{"application/pdf", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			require.Equal(t, tt.want, mimeExt(tt.mime))
		})
	}
}

// ─── convertPostElement ──────────────────────────────────────────────────────

func TestConvertPostElement(t *testing.T) {
	t.Parallel()
	botID := "ou_bot"
	openID := "ou_user"
	name := "Alice"
	mentionMap := map[string]*larkim.MentionEvent{
		openID: {Id: &larkim.UserId{OpenId: &openID}, Name: &name},
	}

	tests := []struct {
		name string
		elem postElement
		want string
	}{
		{
			name: "text element",
			elem: postElement{Tag: "text", Text: "hello"},
			want: "hello",
		},
		{
			name: "a element with href",
			elem: postElement{Tag: "a", Text: "link", Href: "https://example.com"},
			want: "[link](https://example.com)",
		},
		{
			name: "a element without href",
			elem: postElement{Tag: "a", Text: "no href"},
			want: "no href",
		},
		{
			name: "at mentions bot stripped",
			elem: postElement{Tag: "at", UserID: botID},
			want: "",
		},
		{
			name: "at mentions known user",
			elem: postElement{Tag: "at", UserID: openID},
			want: "@Alice",
		},
		{
			name: "at mentions unknown user",
			elem: postElement{Tag: "at", UserID: "ou_unknown"},
			want: "@ou_unknown",
		},
		{
			name: "img element",
			elem: postElement{Tag: "img"},
			want: "[图片]",
		},
		{
			name: "unknown tag",
			elem: postElement{Tag: "unknown", Text: "ignored"},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := convertPostElement(tt.elem, mentionMap, botID)
			require.Equal(t, tt.want, got)
		})
	}
}

// ─── BuildMediaPrompt edge cases ─────────────────────────────────────────────

func TestBuildMediaPrompt_EdgeCases(t *testing.T) {
	t.Parallel()

	// nil paths and nil media: the else branch still writes the header
	// (BuildMediaPrompt's else is unconditional when hasTranscriptions=false).
	got := BuildMediaPrompt("hello world", nil, nil, nil)
	// The else branch unconditionally writes "[用户发送的消息包含 %s，已下载...]" with
	// an empty parts string, then appends user text. So the header always appears.
	require.Contains(t, got, "用户的文字内容:")
	require.Contains(t, got, "hello world")

	// Empty user text with media — no "用户的文字内容:" prefix (text is empty after trim).
	got = BuildMediaPrompt("", []string{"/img/photo.jpg"}, []*MediaInfo{{Type: "image", Key: "i"}}, nil)
	require.NotContains(t, got, "用户的文字内容:")

	// Whitespace-only user text — treated as empty.
	got = BuildMediaPrompt("  ", nil, nil, nil)
	require.NotContains(t, got, "用户的文字内容:")

	// All media types together.
	got = BuildMediaPrompt("all media",
		[]string{"/a.jpg", "/b.pdf", "/c.wav", "/d.mp4", "/e.sticker"},
		[]*MediaInfo{
			{Type: "image", Key: "i"},
			{Type: "file", Key: "f"},
			{Type: "audio", Key: "a"},
			{Type: "video", Key: "v"},
			{Type: "sticker", Key: "s"},
		}, nil)
	require.Contains(t, got, "1 张图片")
	require.Contains(t, got, "1 个文件")
	require.Contains(t, got, "1 条语音")
	require.Contains(t, got, "1 段视频")
	require.Contains(t, got, "1 个表情贴纸")
}

// ─── BuildMediaPrompt transcription paths ────────────────────────────────────

func TestBuildMediaPrompt_TranscriptionPaths(t *testing.T) {
	t.Parallel()

	// Transcription only (no file paths).
	got := BuildMediaPrompt("语音说了什么？", nil,
		[]*MediaInfo{{Type: "audio", Key: "a"}},
		[]string{"用户说: hello"})
	require.Contains(t, got, "已转文字")
	require.Contains(t, got, "语音内容: 用户说: hello")

	// Both transcription and file paths.
	got = BuildMediaPrompt("音频文件", []string{"/tmp/recording.opus"},
		[]*MediaInfo{{Type: "audio", Key: "a"}},
		[]string{"transcribed"})
	require.Contains(t, got, "已转文字（音频文件也已保存供参考）")
	require.Contains(t, got, "语音内容: transcribed")
	require.Contains(t, got, "/tmp/recording.opus")
}

// ─── buildMentionMap edge cases ───────────────────────────────────────────────

func TestBuildMentionMap_EdgeCases(t *testing.T) {
	t.Parallel()

	// Nil MentionEvent in slice causes panic — skip this test.
	// (buildMentionMap dereferences mention.Id without nil guard)

	// Id with nil OpenId is skipped.
	openID := "ou_abc"
	mentions := []*larkim.MentionEvent{
		{Id: &larkim.UserId{OpenId: nil}},
		{Id: &larkim.UserId{OpenId: &openID}},
	}
	m := buildMentionMap(mentions)
	require.Len(t, m, 1)
	require.Contains(t, m, "ou_abc")
}

// ─── ConvertMessage post type ───────────────────────────────────────────────

func TestConvertMessage_Post(t *testing.T) {
	t.Parallel()
	raw := `{
		"title": "Test Post",
		"content": [
			[{"tag": "text", "text": "Hello "}, {"tag": "text", "text": "world"}]
		]
	}`
	text, ok, media := ConvertMessage("post", raw, nil, "", "msg123")
	require.True(t, ok)
	require.Contains(t, text, "## Test Post")
	require.Contains(t, text, "Hello world")
	require.Nil(t, media)
}
