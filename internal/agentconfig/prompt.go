package agentconfig

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed META-COGNITION.md
var embeddedMetacognition string

var hotplexMetacognition string // computed once at init

func init() {
	if embeddedMetacognition != "" {
		hotplexMetacognition = "<hotplex>\n" + embeddedMetacognition + "\n</hotplex>"
	}
}

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
			joinLines(b)+
			"\n\n</directives>")
	}

	// C-channel: reference context. HotPlex metacognition is first, followed by user-defined content.
	hotplex := buildHotplexMetacognition()
	if configs.User != "" || configs.Memory != "" || hotplex != "" {
		var c []string
		if hotplex != "" {
			c = append(c, hotplex)
		}
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
			joinLines(c)+
			"\n\n</context>")
	}

	if len(groups) == 0 {
		return ""
	}

	return "<agent-configuration>\n\n" +
		joinLines(groups) +
		"\n\n</agent-configuration>"
}

func joinLines(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	b := new(strings.Builder)
	n := (len(parts) - 1) * 2 // "\n\n" separators
	for _, p := range parts {
		n += len(p)
	}
	b.Grow(n)
	for i, p := range parts {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(p)
	}
	return b.String()
}

func buildHotplexMetacognition() string { return hotplexMetacognition }
