// Package feishu provides message type converters for Feishu inbound messages.
package feishu

import (
	"encoding/json"
	"fmt"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// ConvertMessage converts a Feishu raw content to AI-friendly text based on message type.
// Returns ("", false) for unsupported types that should be silently ignored.
func ConvertMessage(msgType, rawContent string, mentions []*larkim.MentionEvent, botOpenID string) (string, bool) {
	switch msgType {
	case "text":
		text := extractTextFromContent(rawContent)
		return ResolveMentions(text, mentions, botOpenID), true
	case "post":
		return convertPost(rawContent, mentions, botOpenID), true
	case "image":
		return convertImage(rawContent), true
	case "file":
		return convertFile(rawContent), true
	default:
		return "", false
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

// convertImage parses a Feishu image message and returns a descriptive string.
func convertImage(rawContent string) string {
	var img struct {
		ImageKey string `json:"image_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &img); err != nil || img.ImageKey == "" {
		return "[图片]"
	}
	return "[图片: " + img.ImageKey + "]"
}

// convertFile parses a Feishu file message and returns a descriptive string.
func convertFile(rawContent string) string {
	var f struct {
		FileName string `json:"file_name"`
		FileKey  string `json:"file_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &f); err != nil || f.FileName == "" {
		return "[文件]"
	}
	return "[文件: " + f.FileName + "]"
}
