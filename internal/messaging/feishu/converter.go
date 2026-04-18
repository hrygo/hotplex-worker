// Package feishu provides message type converters for Feishu inbound messages.
package feishu

import (
	"encoding/json"
	"fmt"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// MediaInfo carries metadata about a non-text media attachment in a Feishu message.
type MediaInfo struct {
	Type      string // "image", "file", "audio", "video", "sticker"
	Key       string // image_key, file_key, etc.
	Name      string // Original filename (file type only).
	MessageID string // Message ID (for downloading user-sent media via MessageResource API).
}

// ConvertMessage converts a Feishu raw content to AI-friendly text based on message type.
// Returns ("", false, nil) for unsupported types that should be silently ignored.
func ConvertMessage(msgType, rawContent string, mentions []*larkim.MentionEvent, botOpenID, messageID string) (string, bool, *MediaInfo) {
	switch msgType {
	case "text":
		text := extractTextFromContent(rawContent)
		return ResolveMentions(text, mentions, botOpenID), true, nil
	case "post":
		return convertPost(rawContent, mentions, botOpenID), true, nil
	case "image":
		return convertImage(rawContent, messageID)
	case "file":
		return convertFile(rawContent, messageID)
	case "audio":
		return convertAudio(rawContent, messageID)
	case "video":
		return convertVideo(rawContent, messageID)
	case "sticker":
		return convertSticker(rawContent, messageID)
	default:
		return "", false, nil
	}
}

type postContent struct {
	Title   string          `json:"title"`
	Content [][]postElement `json:"content"`
}

type postElement struct {
	Tag      string `json:"tag"`
	Text     string `json:"text"`
	Href     string `json:"href"`
	UserID   string `json:"user_id"`
	Unfold   bool   `json:"unfold"`
	ImageKey string `json:"image_key"`
}

// convertPost parses a Feishu post (rich text) message and converts it to markdown.
func convertPost(rawContent string, mentions []*larkim.MentionEvent, botOpenID string) string {
	var post postContent
	if err := json.Unmarshal([]byte(rawContent), &post); err != nil {
		return ""
	}

	mentionMap := buildMentionMap(mentions)
	var sb strings.Builder

	if post.Title != "" {
		sb.WriteString("## ")
		sb.WriteString(post.Title)
		sb.WriteString("\n\n")
	}

	for _, paragraph := range post.Content {
		for _, elem := range paragraph {
			sb.WriteString(convertPostElement(elem, mentionMap, botOpenID))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// convertPostElement converts a single post element to markdown text.
func convertPostElement(elem postElement, mentionMap map[string]*larkim.MentionEvent, botOpenID string) string {
	switch elem.Tag {
	case "text":
		return elem.Text
	case "a":
		if elem.Href != "" {
			return fmt.Sprintf("[%s](%s)", elem.Text, elem.Href)
		}
		return elem.Text
	case "at":
		if elem.UserID == botOpenID {
			return ""
		}
		if m, ok := mentionMap[elem.UserID]; ok && m.Name != nil {
			return "@" + *m.Name
		}
		return "@" + elem.UserID
	case "img":
		if elem.ImageKey != "" {
			return fmt.Sprintf("![image](%s)", elem.ImageKey)
		}
		return "[图片]"
	default:
		return ""
	}
}

// buildMentionMap creates a user_id → MentionEvent lookup from the mentions array.
func buildMentionMap(mentions []*larkim.MentionEvent) map[string]*larkim.MentionEvent {
	m := make(map[string]*larkim.MentionEvent, len(mentions))
	for _, mention := range mentions {
		if mention.Id != nil && mention.Id.OpenId != nil {
			m[*mention.Id.OpenId] = mention
		}
	}
	return m
}

// convertImage parses a Feishu image message and returns a descriptive string with media info.
func convertImage(rawContent, messageID string) (string, bool, *MediaInfo) {
	var img struct {
		ImageKey string `json:"image_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &img); err != nil || img.ImageKey == "" {
		return "[图片]", true, nil
	}
	return "[用户发送了一张图片]", true, &MediaInfo{Type: "image", Key: img.ImageKey, MessageID: messageID}
}

// convertFile parses a Feishu file message and returns a descriptive string with media info.
func convertFile(rawContent, messageID string) (string, bool, *MediaInfo) {
	var f struct {
		FileName string `json:"file_name"`
		FileKey  string `json:"file_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &f); err != nil || f.FileKey == "" {
		return "[文件]", true, nil
	}
	return "[用户发送了一个文件]", true, &MediaInfo{Type: "file", Key: f.FileKey, Name: f.FileName, MessageID: messageID}
}

// convertAudio parses a Feishu audio message and returns a descriptive string with media info.
func convertAudio(rawContent, messageID string) (string, bool, *MediaInfo) {
	var a struct {
		FileKey string `json:"file_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &a); err != nil || a.FileKey == "" {
		return "[语音]", true, nil
	}
	return "[用户发送了一条语音]", true, &MediaInfo{Type: "audio", Key: a.FileKey, MessageID: messageID}
}

// convertVideo parses a Feishu video message and returns a descriptive string with media info.
func convertVideo(rawContent, messageID string) (string, bool, *MediaInfo) {
	var v struct {
		FileKey  string `json:"file_key"`
		FileName string `json:"file_name"`
	}
	if err := json.Unmarshal([]byte(rawContent), &v); err != nil || v.FileKey == "" {
		return "[视频]", true, nil
	}
	return "[用户发送了一段视频]", true, &MediaInfo{Type: "video", Key: v.FileKey, Name: v.FileName, MessageID: messageID}
}

// convertSticker parses a Feishu sticker message and returns a descriptive string with media info.
func convertSticker(rawContent, messageID string) (string, bool, *MediaInfo) {
	var s struct {
		FileKey string `json:"file_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &s); err != nil || s.FileKey == "" {
		return "[表情]", true, nil
	}
	return "[用户发送了一个表情]", true, &MediaInfo{Type: "sticker", Key: s.FileKey, MessageID: messageID}
}
