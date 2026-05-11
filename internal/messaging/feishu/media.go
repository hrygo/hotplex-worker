package feishu

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/hrygo/hotplex/internal/config"
)

// processMediaAttachments downloads media files and runs STT on audio,
// returning file paths, transcriptions to be appended to the message text,
// and whether any audio was successfully transcribed (for TTS trigger).
func (a *Adapter) processMediaAttachments(ctx context.Context, medias []*MediaInfo) (paths, transcriptions []string, hasAudioTranscription bool) {
	for _, m := range medias {
		// Audio + STT: try transcription, conditionally skip disk write.
		if m.Type == "audio" && a.transcriber != nil {
			data, ext, fetchErr := a.fetchMediaBytes(ctx, m)
			if fetchErr != nil {
				a.Log.Warn("feishu: audio fetch failed", "key", m.Key, "err", fetchErr)
				continue
			}
			transcription, sttErr := a.transcriber.Transcribe(ctx, data)
			if sttErr == nil && transcription != "" {
				transcriptions = append(transcriptions, transcription)
				hasAudioTranscription = true
				// Pure cloud STT: skip disk write entirely.
				if !a.transcriber.RequiresDisk() {
					continue
				}
			} else if sttErr != nil {
				a.Log.Warn("feishu: stt failed, saving audio to disk", "err", sttErr)
			}
			// Local/fallback mode or STT failure: save to disk for the worker.
			path, saveErr := a.saveMediaBytes(data, m, ext)
			if saveErr != nil {
				a.Log.Warn("feishu: audio save failed", "err", saveErr)
				continue
			}
			paths = append(paths, path)
			continue
		}
		// Non-audio or no STT: download to disk.
		path, err := a.downloadMedia(ctx, m)
		if err != nil {
			a.Log.Warn("feishu: media download failed", "type", m.Type, "key", m.Key, "err", err)
			continue
		}
		if path != "" {
			paths = append(paths, path)
		}
	}
	return
}

const mediaMaxSize = 10 * 1024 * 1024 // 10 MB

const silenceTimeout = 30 * time.Second

// mediaTypeToResourceType maps our internal media types to Feishu resource types.
var mediaTypeToResourceType = map[string]string{
	"image":   "image",
	"file":    "file",
	"audio":   "file",
	"video":   "file",
	"sticker": "file",
}

// mediaExtByType provides fallback extensions when Content-Type is unavailable.
var mediaExtByType = map[string]string{
	"image":   ".jpg",
	"file":    "",
	"audio":   ".opus",
	"video":   ".mp4",
	"sticker": ".gif",
}

// fetchMediaBytes downloads media content to memory without writing to disk.
func (a *Adapter) fetchMediaBytes(ctx context.Context, media *MediaInfo) ([]byte, string, error) {
	if a.larkClient == nil || media == nil || media.MessageID == "" || media.Key == "" {
		return nil, "", fmt.Errorf("feishu: missing lark client, media, messageID, or key")
	}

	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(media.MessageID).
		FileKey(media.Key).
		Type(mediaTypeToResourceType[media.Type]).
		Build()

	// Add a 30-second timeout for the media download.
	downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := a.larkClient.Im.MessageResource.Get(downloadCtx, req)
	if err != nil {
		return nil, "", fmt.Errorf("feishu: download %s: %w", media.Type, err)
	}
	if !resp.Success() {
		return nil, "", fmt.Errorf("feishu: download %s failed: code=%d msg=%s", media.Type, resp.Code, resp.Msg)
	}

	ext := mediaExtByType[media.Type]
	if resp.FileName != "" {
		ext = filepath.Ext(resp.FileName)
	} else if ct := resp.Header.Get("Content-Type"); ct != "" {
		ext = mimeExt(ct)
	}

	data, err := io.ReadAll(resp.File)
	if err != nil {
		return nil, "", fmt.Errorf("feishu: read file content: %w", err)
	}
	if len(data) > mediaMaxSize {
		return nil, "", fmt.Errorf("feishu: file too large: %d > %d bytes", len(data), mediaMaxSize)
	}

	a.Log.Debug("feishu: media fetched", "type", media.Type, "key", media.Key, "size", len(data))
	return data, ext, nil
}

// saveMediaBytes writes media data to disk and returns the file path.
func (a *Adapter) saveMediaBytes(data []byte, media *MediaInfo, ext string) (string, error) {
	mediaDir := filepath.Join(config.TempBaseDir(), "media", media.Type+"s")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return "", fmt.Errorf("feishu: create media dir: %w", err)
	}

	filename := media.Key + ext
	if media.Name != "" {
		if base := filepath.Base(media.Name); base != "." && base != ".." && base != string(filepath.Separator) {
			filename = media.Key + "_" + base
		}
	}
	filePath := filepath.Join(mediaDir, filename)

	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return "", fmt.Errorf("feishu: write file: %w", err)
	}

	a.Log.Debug("feishu: media saved", "type", media.Type, "key", media.Key, "path", filePath)
	return filePath, nil
}

// downloadMedia fetches media and writes to disk. Convenience wrapper.
func (a *Adapter) downloadMedia(ctx context.Context, media *MediaInfo) (string, error) {
	data, ext, err := a.fetchMediaBytes(ctx, media)
	if err != nil {
		return "", err
	}
	return a.saveMediaBytes(data, media, ext)
}

// mimeExt maps MIME type to common file extension.
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
	case "audio/opus":
		return ".opus"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	default:
		return ""
	}
}
