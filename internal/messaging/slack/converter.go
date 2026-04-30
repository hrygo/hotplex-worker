package slack

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

const mediaMaxSize = 20 * 1024 * 1024 // 20 MB

const (
	mediaTypeImage    = "image"
	mediaTypeAudio    = "audio"
	mediaTypeVideo    = "video"
	mediaTypeDocument = "document"
	mediaTypeFile     = "file"

	audioMaxSizeBytes = 5 * 1024 * 1024 // 5 MB heuristic threshold for voice detection
)

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
			switch m.Type {
			case mediaTypeImage:
				parts = append(parts, fmt.Sprintf("[user shared an image: %s]", m.Name))
			case mediaTypeAudio:
				parts = append(parts, fmt.Sprintf("[user sent a voice message: %s]", m.Name))
			default:
				parts = append(parts, fmt.Sprintf("[user shared a file: %s]", m.Name))
			}
		}
		text = strings.Join(parts, " ")
	}

	return text, text != "" || len(media) > 0, media
}

// fileCategory classifies a Slack file by its filetype.
func fileCategory(f slack.File) string {
	if f.Mode == "voice" {
		return mediaTypeAudio
	}
	if f.Filetype == "mp4" && f.Size > 0 && f.Size < audioMaxSizeBytes &&
		f.OriginalW == 0 && f.OriginalH == 0 {
		return mediaTypeAudio
	}
	switch f.Filetype {
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "svg":
		return mediaTypeImage
	case "mp4", "mov", "avi", "webm", "flv":
		return mediaTypeVideo
	case "mp3", "wav", "ogg", "opus", "m4a":
		return mediaTypeAudio
	case "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "csv", "md":
		return mediaTypeDocument
	default:
		return mediaTypeFile
	}
}

// downloadMedia downloads a file from Slack to local storage.
func (a *Adapter) downloadMedia(ctx context.Context, m *MediaInfo) (string, error) {
	if m.Size > mediaMaxSize {
		return "", fmt.Errorf("file too large: %d bytes", m.Size)
	}

	dir, path := mediaFilePath(m)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := a.client.GetFileContext(downloadCtx, m.DownloadURL, f); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("get file: %w", err)
	}

	return path, nil
}

func (a *Adapter) downloadMediaBytes(ctx context.Context, m *MediaInfo) ([]byte, error) {
	if m.Size > mediaMaxSize {
		return nil, fmt.Errorf("file too large: %d bytes", m.Size)
	}

	downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var buf bytes.Buffer
	if err := a.client.GetFileContext(downloadCtx, m.DownloadURL, &buf); err != nil {
		return nil, fmt.Errorf("get file bytes: %w", err)
	}
	return buf.Bytes(), nil
}

func (a *Adapter) saveMediaBytes(m *MediaInfo, data []byte) (string, error) {
	dir, path := mediaFilePath(m)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// mediaFilePath returns (dir, fullPath) for a media file on local storage.
func mediaFilePath(m *MediaInfo) (string, string) {
	ext := mimeExt(m.MimeType)
	if ext == "" {
		ext = "." + m.FileID
	}
	dir := filepath.Join(MediaPathPrefix, m.Type+"s")
	path := filepath.Join(dir, fmt.Sprintf("%s_%s%s", m.Type, m.FileID, ext))
	return dir, path
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
