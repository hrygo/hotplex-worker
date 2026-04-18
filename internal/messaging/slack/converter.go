package slack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

const mediaMaxSize = 20 * 1024 * 1024 // 20 MB

// MediaInfo holds metadata about an attached file.
type MediaInfo struct {
	Type        string // "image", "video", "audio", "document", "file"
	FileID      string
	Name        string
	MimeType    string
	Size        int
	DownloadURL string // url_private_download
	PublicURL   string // permalink
}

// ConvertMessage converts a Slack MessageEvent into text + media info.
func (a *Adapter) ConvertMessage(msgEvent slackevents.MessageEvent) (text string, ok bool, media []*MediaInfo) {
	text = extractText(msgEvent)

	// Extract files from msgEvent.Files (if available)
	msg := msgEvent.Message
	if msg != nil && len(msg.Files) > 0 {
		media = make([]*MediaInfo, 0, len(msg.Files))
		for _, f := range msg.Files {
			if f.User == a.botID {
				continue
			}
			if f.IsExternal || f.ExternalType != "" {
				continue
			}
			media = append(media, &MediaInfo{
				Type:        fileCategory(f),
				FileID:      f.ID,
				Name:        f.Name,
				MimeType:    f.Mimetype,
				Size:        f.Size,
				DownloadURL: f.URLPrivateDownload,
				PublicURL:   f.Permalink,
			})
		}
	}

	// file_share but no text → generate placeholder
	if text == "" && len(media) > 0 {
		var parts []string
		for _, m := range media {
			if m.Type == "image" {
				parts = append(parts, fmt.Sprintf("[user shared an image: %s]", m.Name))
			} else {
				parts = append(parts, fmt.Sprintf("[user shared a file: %s]", m.Name))
			}
		}
		text = strings.Join(parts, " ")
	}

	return text, text != "" || len(media) > 0, media
}

// fileCategory classifies a Slack file by its filetype.
func fileCategory(f slack.File) string {
	switch f.Filetype {
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "svg":
		return "image"
	case "mp4", "mov", "avi", "webm", "flv":
		return "video"
	case "mp3", "wav", "ogg", "opus", "m4a":
		return "audio"
	case "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "csv", "md":
		return "document"
	default:
		return "file"
	}
}

// downloadMedia downloads a file from Slack to local storage.
func (a *Adapter) downloadMedia(_ context.Context, m *MediaInfo) (string, error) {
	if m.Size > mediaMaxSize {
		return "", fmt.Errorf("file too large: %d bytes", m.Size)
	}

	ext := mimeExt(m.MimeType)
	if ext == "" {
		ext = "." + m.FileID
	}
	filename := fmt.Sprintf("%s_%s%s", m.Type, m.FileID, ext)

	dir := fmt.Sprintf("/tmp/hotplex/media/slack/%ss", m.Type)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, filename)

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	if err := a.client.GetFile(m.DownloadURL, f); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("get file: %w", err)
	}

	return path, nil
}

func mimeExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "video/webm":
		return ".webm"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	case "audio/opus":
		return ".opus"
	case "application/pdf":
		return ".pdf"
	}
	return ""
}
