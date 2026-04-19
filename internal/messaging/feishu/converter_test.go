package feishu

import (
	"testing"

	"github.com/stretchr/testify/require"
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
