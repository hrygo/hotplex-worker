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
func ConvertMessage(msgType, rawContent string, mentions []*larkim.MentionEvent, botOpenID, messageID string) (string, bool, []*MediaInfo) {
	switch msgType {
	case "text":
		text := extractTextFromContent(rawContent)
		return ResolveMentions(text, mentions, botOpenID), true, nil
	case "post":
		text, media := convertPost(rawContent, mentions, botOpenID, messageID)
		return text, true, media
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
	case "interactive":
		return convertInteractive(rawContent, messageID)
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
// Embedded images are collected as MediaInfo for downstream download.
func convertPost(rawContent string, mentions []*larkim.MentionEvent, botOpenID, messageID string) (string, []*MediaInfo) {
	var post postContent
	if err := json.Unmarshal([]byte(rawContent), &post); err != nil {
		return "", nil
	}

	mentionMap := buildMentionMap(mentions)
	var sb strings.Builder
	var mediaList []*MediaInfo

	if post.Title != "" {
		sb.WriteString("## ")
		sb.WriteString(post.Title)
		sb.WriteString("\n\n")
	}

	for _, paragraph := range post.Content {
		for _, elem := range paragraph {
			sb.WriteString(convertPostElement(elem, mentionMap, botOpenID))
			if elem.Tag == "img" && elem.ImageKey != "" {
				mediaList = append(mediaList, &MediaInfo{
					Type:      "image",
					Key:       elem.ImageKey,
					MessageID: messageID,
				})
			}
		}
		sb.WriteString("\n")
	}
	return sb.String(), mediaList
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
func convertImage(rawContent, messageID string) (string, bool, []*MediaInfo) {
	var img struct {
		ImageKey string `json:"image_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &img); err != nil || img.ImageKey == "" {
		return "[图片]", true, nil
	}
	return "[用户发送了一张图片]", true, []*MediaInfo{{Type: "image", Key: img.ImageKey, MessageID: messageID}}
}

// convertFile parses a Feishu file message and returns a descriptive string with media info.
func convertFile(rawContent, messageID string) (string, bool, []*MediaInfo) {
	var f struct {
		FileName string `json:"file_name"`
		Filekey  string `json:"file_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &f); err != nil || f.Filekey == "" {
		return "[文件]", true, nil
	}
	return "[用户发送了一个文件]", true, []*MediaInfo{{Type: "file", Key: f.Filekey, Name: f.FileName, MessageID: messageID}}
}

// convertAudio parses a Feishu audio message and returns a descriptive string with media info.
func convertAudio(rawContent, messageID string) (string, bool, []*MediaInfo) {
	var a struct {
		FileKey string `json:"file_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &a); err != nil || a.FileKey == "" {
		return "[语音]", true, nil
	}
	return "[用户发送了一条语音]", true, []*MediaInfo{{Type: "audio", Key: a.FileKey, MessageID: messageID}}
}

// convertVideo parses a Feishu video message and returns a descriptive string with media info.
func convertVideo(rawContent, messageID string) (string, bool, []*MediaInfo) {
	var v struct {
		FileKey  string `json:"file_key"`
		FileName string `json:"file_name"`
	}
	if err := json.Unmarshal([]byte(rawContent), &v); err != nil || v.FileKey == "" {
		return "[视频]", true, nil
	}
	return "[用户发送了一段视频]", true, []*MediaInfo{{Type: "video", Key: v.FileKey, Name: v.FileName, MessageID: messageID}}
}

// convertSticker parses a Feishu sticker message and returns a descriptive string with media info.
func convertSticker(rawContent, messageID string) (string, bool, []*MediaInfo) {
	var s struct {
		FileKey string `json:"file_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &s); err != nil || s.FileKey == "" {
		return "[表情]", true, nil
	}
	return "[用户发送了一个表情]", true, []*MediaInfo{{Type: "sticker", Key: s.FileKey, MessageID: messageID}}
}

// ─── Interactive card ──────────────────────────────────────────────────────────

// interactiveCard models the Feishu card JSON received in an interactive message.
// Supports both schema 1.0 (elements at root) and schema 2.0 (elements under body).
type interactiveCard struct {
	Schema   string               `json:"schema"`
	Header   *interactiveHeader   `json:"header"`
	Elements []interactiveElement `json:"elements"` // schema 1.0
	Body     *interactiveBody     `json:"body"`     // schema 2.0
}

type interactiveBody struct {
	Elements []interactiveElement `json:"elements"`
}

type interactiveHeader struct {
	Title    *interactiveText `json:"title"`
	Subtitle string           `json:"subtitle"`
}

type interactiveText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type interactiveElement struct {
	Tag      string               `json:"tag"`
	Text     *interactiveText     `json:"text"`
	Content  string               `json:"content"`  // markdown content
	ImgKey   string               `json:"img_key"`  // img element
	Elements []interactiveElement `json:"elements"` // note/column_set elements
}

// convertInteractive extracts text content and images from a Feishu interactive card message.
func convertInteractive(rawContent, messageID string) (string, bool, []*MediaInfo) {
	var card interactiveCard
	if err := json.Unmarshal([]byte(rawContent), &card); err != nil {
		return "[交互式卡片]", true, nil
	}

	var sb strings.Builder
	var mediaList []*MediaInfo

	if card.Header != nil {
		if card.Header.Title != nil && card.Header.Title.Content != "" {
			sb.WriteString("## ")
			sb.WriteString(card.Header.Title.Content)
			sb.WriteString("\n\n")
		}
		if card.Header.Subtitle != "" {
			sb.WriteString(card.Header.Subtitle)
			sb.WriteString("\n\n")
		}
	}

	// Schema 2.0 uses body.elements; schema 1.0 uses root-level elements.
	elements := card.Elements
	if card.Body != nil && len(card.Body.Elements) > 0 {
		elements = card.Body.Elements
	}

	for _, elem := range elements {
		text, medias := extractInteractiveElement(elem, messageID)
		if text != "" {
			sb.WriteString(text)
			sb.WriteString("\n")
		}
		mediaList = append(mediaList, medias...)
	}

	result := strings.TrimSpace(sb.String())
	if result == "" && len(mediaList) == 0 {
		return "[交互式卡片]", true, nil
	}

	return result, true, mediaList
}

// extractInteractiveElement recursively extracts text and images from a card element.
func extractInteractiveElement(elem interactiveElement, messageID string) (string, []*MediaInfo) {
	var text string
	var mediaList []*MediaInfo

	switch elem.Tag {
	case "div":
		if elem.Text != nil && elem.Text.Content != "" {
			text = elem.Text.Content
		}
	case "markdown":
		text = elem.Content
	case "img":
		if elem.ImgKey != "" {
			text = "[图片]"
			mediaList = append(mediaList, &MediaInfo{
				Type:      "image",
				Key:       elem.ImgKey,
				MessageID: messageID,
			})
		}
	case "note", "column_set":
		var sb strings.Builder
		for _, child := range elem.Elements {
			childText, childMedias := extractInteractiveElement(child, messageID)
			if childText != "" {
				sb.WriteString(childText)
				sb.WriteString("\n")
			}
			mediaList = append(mediaList, childMedias...)
		}
		text = strings.TrimSpace(sb.String())
	}

	return text, mediaList
}

// BuildMediaPrompt constructs a worker-friendly prompt with media file paths and transcriptions.
func BuildMediaPrompt(userText string, paths []string, medias []*MediaInfo, transcriptions []string) string {
	var sb strings.Builder

	// Count media types for natural language description.
	var imgCount, fileCount, audioCount, videoCount, stickerCount int
	for _, m := range medias {
		switch m.Type {
		case "image":
			imgCount++
		case "file":
			fileCount++
		case "audio":
			audioCount++
		case "video":
			videoCount++
		case "sticker":
			stickerCount++
		}
	}

	var parts []string
	if imgCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 张图片", imgCount))
	}
	if fileCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 个文件", fileCount))
	}
	if audioCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 条语音", audioCount))
	}
	if videoCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 段视频", videoCount))
	}
	if stickerCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 个表情贴纸", stickerCount))
	}

	// Build header based on whether we have transcriptions, file paths, or both.
	hasTranscriptions := len(transcriptions) > 0
	hasPaths := len(paths) > 0

	if hasTranscriptions && !hasPaths {
		// Transcription only — no files to attach.
		fmt.Fprintf(&sb, "[用户发送的消息包含 %s，已转文字]\n", strings.Join(parts, "、"))
		for _, t := range transcriptions {
			fmt.Fprintf(&sb, "语音内容: %s\n", t)
		}
	} else if hasTranscriptions && hasPaths {
		// Both transcription and file paths available.
		fmt.Fprintf(&sb, "[用户发送的消息包含 %s，已转文字（音频文件也已保存供参考）]\n", strings.Join(parts, "、"))
		for _, t := range transcriptions {
			fmt.Fprintf(&sb, "语音内容: %s\n", t)
		}
		for _, p := range paths {
			sb.WriteString("- ")
			sb.WriteString(p)
			sb.WriteString("\n")
		}
	} else {
		// File paths only (no transcription).
		if audioCount > 0 {
			fmt.Fprintf(&sb, "[用户发送的消息包含 %s，STT 转写失败，已下载到本地，请优先使用 stt_once.py 转写]\n", strings.Join(parts, "、"))
		} else {
			fmt.Fprintf(&sb, "[用户发送的消息包含 %s，已下载到本地，请使用 Read 工具查看后再回答]\n", strings.Join(parts, "、"))
		}
		for _, p := range paths {
			sb.WriteString("- ")
			sb.WriteString(p)
			sb.WriteString("\n")
		}
	}

	userText = strings.TrimSpace(userText)
	if userText != "" {
		sb.WriteString("\n用户的文字内容:\n")
		sb.WriteString(userText)
	}

	return sb.String()
}
