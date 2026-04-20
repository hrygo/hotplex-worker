package slack

import (
	"fmt"
	"regexp"
	"strings"
)

// FormatMrkdwn converts standard Markdown to Slack mrkdwn.
// Preserves code blocks and inline code unchanged.
// Converts CommonMark *italic* to Slack _italic_.
func FormatMrkdwn(text string) string {
	// Protect code blocks and inline code
	placeholders := make(map[string]string)
	text = protectCode(text, placeholders)

	// Convert CommonMark *italic* → _italic_ BEFORE heading/bold conversion.
	// This ensures *italic* (single-asterisk emphasis) is captured before
	// **bold** → *bold* creates new single-asterisk patterns.
	// \B matches where * is NOT adjacent to another * (excludes **bold**, ***bold***).
	italicPh := make(map[string]string)
	text = protectItalics(text, italicPh)

	// Convert headings: ## H2 → *H2*
	text = headingRe.ReplaceAllStringFunc(text, func(m string) string {
		sub := headingRe.FindStringSubmatch(m)
		return "*" + strings.TrimSpace(sub[1]) + "*"
	})

	// Convert bold+italic: ***text*** → *_text_* (must be before bold)
	text = tripletBoldRe.ReplaceAllString(text, "*_${1}_*")

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

	// Restore italic placeholders (now safe — bold/heading already done)
	text = restoreItalics(text, italicPh)
	return text
}

var (
	headingRe       = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	tripletBoldRe   = regexp.MustCompile(`\*{3}(.+?)\*{3}`)
	boldRe          = regexp.MustCompile(`\*\*(.+?)\*\*`)
	strikethroughRe = regexp.MustCompile(`~~([^~]+)~~`)
	linkRe          = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	listRe          = regexp.MustCompile(`(?m)^(\s*)[-*+]\s+`)
	fencedCodeRe    = regexp.MustCompile("(?s)(```.*?```)")
	inlineCodeRe    = regexp.MustCompile("(`[^`\n]+`)")
	// Match *text* where * is NOT adjacent to another *.
	// This captures CommonMark italic while excluding **bold** and ***bold-italic***.
	singleAsteriskItalicRe = regexp.MustCompile(`(?:^|[^*])\*([^*]+?)\*(?:[^*]|$)`)
)

var codePlaceholderPrefix = "\x00CODE"
var italicPlaceholderPrefix = "\x00ITALIC"

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

func protectItalics(text string, ph map[string]string) string {
	return singleAsteriskItalicRe.ReplaceAllStringFunc(text, func(m string) string {
		// Extract the content between asterisks, trimming captured surrounding chars.
		content := strings.TrimFunc(m, func(r rune) bool { return r == '*' || r == ' ' || r == '\t' })
		key := fmt.Sprintf("%s%d\x00", italicPlaceholderPrefix, len(ph))
		ph[key] = "_" + content + "_"
		return key
	})
}

func restoreItalics(text string, ph map[string]string) string {
	for k, v := range ph {
		text = strings.ReplaceAll(text, k, v)
	}
	return text
}
