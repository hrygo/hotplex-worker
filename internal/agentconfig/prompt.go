package agentconfig

import (
	"fmt"
	"strings"
)

// BuildSystemPrompt assembles the full agent context (B+C channels) into a single
// system prompt. Used by both Claude Code (--append-system-prompt) and OpenCode
// Server (system field per message). Two-level XML nesting conveys the B/C priority
// distinction: directives (behavioral constraints) vs context (reference material).
func BuildSystemPrompt(configs *AgentConfigs) string {
	if configs == nil || configs.IsEmpty() {
		return ""
	}

	var groups []string

	// B-channel: behavior-shaping directives (highest priority, listed first).
	if configs.Soul != "" || configs.Agents != "" || configs.Skills != "" {
		var b []string
		if configs.Soul != "" {
			b = append(b, fmt.Sprintf(
				"<persona>\nEmbody this persona naturally in all interactions.\n\n%s\n</persona>",
				configs.Soul,
			))
		}
		if configs.Agents != "" {
			b = append(b, fmt.Sprintf(
				"<rules>\nTreat as mandatory workspace constraints.\n\n%s\n</rules>",
				configs.Agents,
			))
		}
		if configs.Skills != "" {
			b = append(b, fmt.Sprintf(
				"<skills>\nApply these capabilities when relevant.\n\n%s\n</skills>",
				configs.Skills,
			))
		}
		groups = append(groups, "<directives>\nCore behavioral parameters — follow unless overridden by explicit user instructions.\n\n"+
			strings.Join(b, "\n\n")+
			"\n\n</directives>")
	}

	// C-channel: reference context.
	if configs.User != "" || configs.Memory != "" {
		var c []string
		if configs.User != "" {
			c = append(c, fmt.Sprintf(
				"<user>\nTailor responses to this user's preferences and expertise.\n\n%s\n</user>",
				configs.User,
			))
		}
		if configs.Memory != "" {
			c = append(c, fmt.Sprintf(
				"<memory>\nRecall relevant past context when applicable.\n\n%s\n</memory>",
				configs.Memory,
			))
		}
		groups = append(groups, "<context>\nReference material to inform your responses.\n\n"+
			strings.Join(c, "\n\n")+
			"\n\n</context>")
	}

	if len(groups) == 0 {
		return ""
	}

	return "<agent-configuration>\n\n" +
		strings.Join(groups, "\n\n") +
		"\n\n</agent-configuration>"
}
