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
		wantType   string
		wantKey    string
	}{
		{
			name:       "valid image",
			rawContent: `{"image_key":"img_abc123"}`,
			msgID:      "msg_123",
			wantText:   "[用户发送了一张图片]",
			wantOK:     true,
			wantType:   "image",
			wantKey:    "img_abc123",
		},
		{
			name:       "empty image_key",
			rawContent: `{"image_key":""}`,
			msgID:      "msg_123",
			wantText:   "[图片]",
			wantOK:     true,
			wantType:   "",
			wantKey:    "",
		},
		{
			name:       "invalid json",
			rawContent: `{invalid`,
			msgID:      "msg_123",
			wantText:   "[图片]",
			wantOK:     true,
			wantType:   "",
			wantKey:    "",
		},
		{
			name:       "missing image_key",
			rawContent: `{}`,
			msgID:      "msg_123",
			wantText:   "[图片]",
			wantOK:     true,
			wantType:   "",
			wantKey:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := convertImage(tt.rawContent, tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantType == "" {
				require.Nil(t, media)
			} else {
				require.NotNil(t, media)
				require.Equal(t, tt.wantType, media.Type)
				require.Equal(t, tt.wantKey, media.Key)
				require.Equal(t, tt.msgID, media.MessageID)
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
			wantType:   "file",
			wantKey:    "file_xyz",
		},
		{
			name:       "empty file_key",
			rawContent: `{"file_key":""}`,
			msgID:      "msg_456",
			wantText:   "[文件]",
			wantOK:     true,
			wantType:   "",
			wantKey:    "",
		},
		{
			name:       "invalid json",
			rawContent: `{`,
			msgID:      "msg_456",
			wantText:   "[文件]",
			wantOK:     true,
			wantType:   "",
			wantKey:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := convertFile(tt.rawContent, tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantType == "" {
				require.Nil(t, media)
			} else {
				require.NotNil(t, media)
				require.Equal(t, tt.wantType, media.Type)
				require.Equal(t, tt.wantKey, media.Key)
				require.Equal(t, tt.wantName, media.Name)
				require.Equal(t, tt.msgID, media.MessageID)
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
		wantType   string
		wantKey    string
	}{
		{
			name:       "valid audio",
			rawContent: `{"file_key":"audio_xyz"}`,
			msgID:      "msg_audio",
			wantText:   "[用户发送了一条语音]",
			wantOK:     true,
			wantType:   "audio",
			wantKey:    "audio_xyz",
		},
		{
			name:       "empty file_key",
			rawContent: `{"file_key":""}`,
			msgID:      "msg_audio",
			wantText:   "[语音]",
			wantOK:     true,
			wantType:   "",
			wantKey:    "",
		},
		{
			name:       "invalid json",
			rawContent: `not json`,
			msgID:      "msg_audio",
			wantText:   "[语音]",
			wantOK:     true,
			wantType:   "",
			wantKey:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := convertAudio(tt.rawContent, tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantType == "" {
				require.Nil(t, media)
			} else {
				require.NotNil(t, media)
				require.Equal(t, tt.wantType, media.Type)
				require.Equal(t, tt.wantKey, media.Key)
				require.Equal(t, tt.msgID, media.MessageID)
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
			wantType:   "video",
			wantKey:    "video_456",
		},
		{
			name:       "empty file_key",
			rawContent: `{"file_key":""}`,
			msgID:      "msg_video",
			wantText:   "[视频]",
			wantOK:     true,
			wantType:   "",
			wantKey:    "",
		},
		{
			name:       "invalid json",
			rawContent: `broken`,
			msgID:      "msg_video",
			wantText:   "[视频]",
			wantOK:     true,
			wantType:   "",
			wantKey:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := convertVideo(tt.rawContent, tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantType == "" {
				require.Nil(t, media)
			} else {
				require.NotNil(t, media)
				require.Equal(t, tt.wantType, media.Type)
				require.Equal(t, tt.wantKey, media.Key)
				require.Equal(t, tt.wantName, media.Name)
				require.Equal(t, tt.msgID, media.MessageID)
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
		wantType   string
		wantKey    string
	}{
		{
			name:       "valid sticker",
			rawContent: `{"file_key":"sticker_gif"}`,
			msgID:      "msg_sticker",
			wantText:   "[用户发送了一个表情]",
			wantOK:     true,
			wantType:   "sticker",
			wantKey:    "sticker_gif",
		},
		{
			name:       "empty file_key",
			rawContent: `{"file_key":""}`,
			msgID:      "msg_sticker",
			wantText:   "[表情]",
			wantOK:     true,
			wantType:   "",
			wantKey:    "",
		},
		{
			name:       "invalid json",
			rawContent: `null`,
			msgID:      "msg_sticker",
			wantText:   "[表情]",
			wantOK:     true,
			wantType:   "",
			wantKey:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := convertSticker(tt.rawContent, tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantType == "" {
				require.Nil(t, media)
			} else {
				require.NotNil(t, media)
				require.Equal(t, tt.wantType, media.Type)
				require.Equal(t, tt.wantKey, media.Key)
				require.Equal(t, tt.msgID, media.MessageID)
			}
		})
	}
}

func TestConvertMessage_MediaTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		msgType      string
		content      string
		msgID        string
		wantText     string
		wantOK       bool
		wantNilMedia bool
		wantType     string
	}{
		{
			name:         "text type returns no media",
			msgType:      "text",
			content:      `{"text":"hello"}`,
			msgID:        "msg_text",
			wantText:     "hello",
			wantOK:       true,
			wantNilMedia: true,
		},
		{
			name:         "unsupported type returns false",
			msgType:      "unsupported",
			content:      `{}`,
			msgID:        "msg_bad",
			wantText:     "",
			wantOK:       false,
			wantNilMedia: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok, media := ConvertMessage(tt.msgType, tt.content, nil, "", tt.msgID)
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
			if tt.wantNilMedia {
				require.Nil(t, media)
			} else {
				require.NotNil(t, media)
				require.Equal(t, tt.wantType, media.Type)
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
