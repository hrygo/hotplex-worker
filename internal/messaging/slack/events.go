package slack

import (
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

// extractText extracts the text content from a Slack message event.
func extractText(event slackevents.MessageEvent) string {
	if event.Text != "" {
		return event.Text
	}
	// Try to extract text from blocks.
	for _, block := range event.Blocks.BlockSet {
		if section, ok := block.(*slack.SectionBlock); ok {
			if section.Text != nil {
				text := section.Text.Text
				if section.Text.Type == slack.MarkdownType {
					// Strip markdown for plain text extraction.
					text = stripMarkdown(text)
				}
				if strings.TrimSpace(text) != "" {
					return text
				}
			}
		}
	}
	return ""
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
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "~", "")
	s = strings.ReplaceAll(s, "`", "")
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
