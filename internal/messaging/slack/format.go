package slack

import (
	"fmt"
	"regexp"
	"strings"
)

// FormatMrkdwn converts standard Markdown to Slack mrkdwn.
// Preserves code blocks and inline code unchanged.
func FormatMrkdwn(text string) string {
	// Protect code blocks and inline code
	placeholders := make(map[string]string)
	text = protectCode(text, placeholders)

	// Convert headings: ## H2 → *H2*
	text = headingRe.ReplaceAllStringFunc(text, func(m string) string {
		sub := headingRe.FindStringSubmatch(m)
		return "*" + strings.TrimSpace(sub[1]) + "*"
	})

	// Convert bold: **text** → *text*
	text = boldRe.ReplaceAllString(text, "*$1*")

	// Convert strikethrough: ~~text~~ → ~text~
	text = strikethroughRe.ReplaceAllString(text, "~$1~")

	// Convert links: [text](url) → <url|text>
	text = linkRe.ReplaceAllString(text, "<$2|$1>")

	// Convert unordered lists: - item → • item
	text = listRe.ReplaceAllString(text, "$1• ")

	// Restore code
	text = restoreCode(text, placeholders)
	return text
}

var (
	headingRe       = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	boldRe          = regexp.MustCompile(`\*\*(.+?)\*\*`)
	strikethroughRe = regexp.MustCompile(`~~([^~]+)~~`)
	linkRe          = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	listRe          = regexp.MustCompile(`(?m)^(\s*)[-*+]\s+`)
	fencedCodeRe    = regexp.MustCompile("(?s)(```.*?```)")
	inlineCodeRe    = regexp.MustCompile("(`[^`\n]+`)")
)

var codePlaceholderPrefix = "\x00CODE"

func protectCode(text string, ph map[string]string) string {
	// Protect fenced code blocks first (greedy), then inline code
	text = fencedCodeRe.ReplaceAllStringFunc(text, func(m string) string {
		key := fmt.Sprintf("%s%d\x00", codePlaceholderPrefix, len(ph))
		ph[key] = m
		return key
	})
	text = inlineCodeRe.ReplaceAllStringFunc(text, func(m string) string {
		key := fmt.Sprintf("%s%d\x00", codePlaceholderPrefix, len(ph))
		ph[key] = m
		return key
	})
	return text
}

func restoreCode(text string, ph map[string]string) string {
	for k, v := range ph {
		text = strings.ReplaceAll(text, k, v)
	}
	return text
}
