package slack

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/slack-go/slack"

	"github.com/hrygo/hotplex/internal/config"
)

// MediaPathPrefix is the base path for downloaded Slack media files.
var MediaPathPrefix = filepath.Join(config.TempBaseDir(), "media", "slack")

// Pre-computed media subdirectory prefixes for fast path matching.
var (
	mediaImagesPrefix = filepath.Join(MediaPathPrefix, "images") + string(filepath.Separator)
	mediaVideosPrefix = filepath.Join(MediaPathPrefix, "videos") + string(filepath.Separator)
)

// ImageExtensions lists file extensions recognized as images by block rendering.
var ImageExtensions = []string{".png", ".jpg", ".jpeg", ".gif", ".webp"}

type imagePart struct {
	URL     string
	AltText string
}

// extractImages extracts image paths from AI text and returns cleaned remaining text.
func extractImages(text string) (parts []imagePart, remaining string) {
	var lines []string

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)

		if isLocalMediaPath(trimmed) {
			imgURL, altText := localFileToImagePart(trimmed)
			if imgURL != "" {
				parts = append(parts, imagePart{URL: imgURL, AltText: altText})
				continue
			}
		} else if isImageURL(trimmed) {
			parts = append(parts, imagePart{URL: trimmed, AltText: "image"})
			continue
		}

		lines = append(lines, line)
	}

	remaining = strings.TrimSpace(strings.Join(lines, "\n"))
	return parts, remaining
}

func isLocalMediaPath(s string) bool {
	return strings.HasPrefix(s, mediaImagesPrefix) ||
		strings.HasPrefix(s, mediaVideosPrefix)
}

func isImageURL(s string) bool {
	return (strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")) &&
		(hasImageExt(s) || strings.Contains(s, "files.slack.com"))
}

func hasImageExt(s string) bool {
	lower := strings.ToLower(s)
	for _, ext := range ImageExtensions {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

func localFileToImagePart(path string) (imgURL, altText string) {
	const maxImageSize = 5 * 1024 * 1024

	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	if len(data) > maxImageSize {
		return "", ""
	}

	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return "", ""
	}

	altText = filepath.Base(path)
	imgURL = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	return imgURL, altText
}

// buildImageBlocks creates Slack Block Kit blocks from extracted image parts and remaining text.
func buildImageBlocks(parts []imagePart, remaining string) []slack.Block {
	var blocks []slack.Block

	if remaining != "" {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, FormatMrkdwn(remaining), false, false),
			nil, nil,
		))
	}

	for _, img := range parts {
		blocks = append(blocks, slack.NewImageBlock(
			img.URL,
			img.AltText,
			"",
			nil,
		))
	}

	return blocks
}
