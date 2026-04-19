package slack

import (
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

// extractText extracts the text content from a Slack message event.
func extractText(event slackevents.MessageEvent) string {
	if event.Text != "" {
		return event.Text
	}
	// Try to extract text from blocks.
	var parts []string
	for _, block := range event.Blocks.BlockSet {
		switch b := block.(type) {
		case *slack.SectionBlock:
			if b.Text != nil {
				text := b.Text.Text
				if b.Text.Type == slack.MarkdownType {
					text = stripMarkdown(text)
				}
				if strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		case *slack.ContextBlock:
			for _, elem := range b.ContextElements.Elements {
				if t, ok := elem.(*slack.TextBlockObject); ok && t.Text != "" {
					parts = append(parts, t.Text)
				}
			}
		case *slack.RichTextBlock:
			for _, elem := range b.Elements {
				if sec, ok := elem.(*slack.RichTextSection); ok {
					parts = append(parts, extractRichTextSection(sec))
				}
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return ""
}

func extractRichTextSection(sec *slack.RichTextSection) string {
	var parts []string
	for _, elem := range sec.Elements {
		if t, ok := elem.(*slack.RichTextSectionTextElement); ok && t.Text != "" {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "")
}

// extractThreadTS returns the thread timestamp if the message is in a thread.
func extractThreadTS(event slackevents.MessageEvent) string {
	return event.ThreadTimeStamp
}

// isBotMessage reports whether the message was sent by a bot.
func isBotMessage(event slackevents.MessageEvent) bool {
	return event.BotID != "" || event.SubType == "bot_message"
}

// stripMarkdown removes basic markdown formatting from text.
func stripMarkdown(s string) string {
	// Preserve angle-bracket content (mentions <@UID>, links <url|text>, etc.)
	// from formatting removal by temporarily replacing with null-byte placeholders.
	var saved []string
	for {
		i := strings.Index(s, "<")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], ">")
		if j < 0 {
			break
		}
		j += i
		saved = append(saved, s[i:j+1])
		s = s[:i] + "\x00" + s[j+1:]
	}

	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "~", "")
	s = strings.ReplaceAll(s, "`", "")

	for _, orig := range saved {
		s = strings.Replace(s, "\x00", orig, 1)
	}

	return strings.TrimSpace(s)
}

// ExtractChannelThread parses channel_id and thread_ts from a Slack session ID.
// Format: slack:{team_id}:{channel_id}:{thread_ts}:{user_id}
func ExtractChannelThread(sessionID string) (channelID, threadTS string) {
	parts := strings.SplitN(sessionID, ":", 5)
	if len(parts) < 5 || parts[0] != "slack" {
		return "", ""
	}
	return parts[2], parts[3]
}

// parseSlackTS converts a Slack timestamp string (e.g. "1234567890.123456") to time.Time.
func parseSlackTS(ts string) (time.Time, error) {
	parts := strings.SplitN(ts, ".", 2)
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sec, 0), nil
}
