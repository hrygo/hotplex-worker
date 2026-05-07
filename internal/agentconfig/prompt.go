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
		hotplexMetacognition = "    <hotplex>\n" + embeddedMetacognition + "\n    </hotplex>"
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

	hotplex := buildHotplexMetacognition()

	// B-channel: behavior-shaping directives (highest priority, listed first).
	// HotPlex metacognition goes first as it defines the systemic ground rules.
	if configs.Soul != "" || configs.Agents != "" || configs.Skills != "" || hotplex != "" {
		var b []string
		if hotplex != "" {
			b = append(b, hotplex)
		}
		if configs.Soul != "" {
			b = append(b, fmt.Sprintf(
				"    <persona>\n    在所有交互中自然地代入并体现此人格定位。\n\n%s\n    </persona>",
				sanitize(configs.Soul),
			))
		}
		if configs.Agents != "" {
			b = append(b, fmt.Sprintf(
				"    <rules>\n    视为强制性的工作空间行为约束。\n\n%s\n    </rules>",
				sanitize(configs.Agents),
			))
		}
		if configs.Skills != "" {
			b = append(b, fmt.Sprintf(
				"    <skills>\n    在相关时调用这些能力。\n\n%s\n    </skills>",
				sanitize(configs.Skills),
			))
		}
		groups = append(groups, "  <directives>\n  核心行为准则 —— 除非用户有明确的反向指令，否则必须严格遵守。\n\n"+
			joinLines(b)+
			"\n  </directives>")
	}

	// C-channel: reference context.
	// We add a strict isolation notice (P5) to prevent C-channel noise from overriding B-channel instructions.
	if configs.User != "" || configs.Memory != "" {
		var c []string
		c = append(c, "    <notice>\n    以下 [context] 区域提供了执行任务所需的关键背景与事实。你应该在不违反 [directives] 的前提下，尽可能深度参考并采纳这些信息。若两者冲突，以 [directives] 为准。\n    </notice>")
		if configs.User != "" {
			c = append(c, fmt.Sprintf(
				"    <user>\n    深入理解用户的偏好、习惯与专业背景，提供个性化的服务体验。\n\n%s\n    </user>",
				sanitize(configs.User),
			))
		}
		if configs.Memory != "" {
			c = append(c, fmt.Sprintf(
				"    <memory>\n    回顾历史交互记录，确保任务执行的连贯性与深度。\n\n%s\n    </memory>",
				sanitize(configs.Memory),
			))
		}
		groups = append(groups, "  <context>\n  提供执行任务所需的背景与事实依据。\n\n"+
			joinLines(c)+
			"\n  </context>")
	}

	if len(groups) == 0 {
		return ""
	}

	return "<agent-configuration>\n" +
		joinLines(groups) +
		"\n</agent-configuration>"
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

var reservedTags = []string{
	"agent-configuration", "directives", "context", "persona",
	"rules", "skills", "user", "memory", "hotplex", "notice",
}

// sanitize prevents XML injection by escaping tags that match our structural schema.
// This ensures that literal strings like "<directives>" in markdown files
// don't break Claude's XML parser or allow prompt injection.
func sanitize(s string) string {
	res := s
	for _, tag := range reservedTags {
		res = strings.ReplaceAll(res, "<"+tag+">", "&lt;"+tag+"&gt;")
		res = strings.ReplaceAll(res, "</"+tag+">", "&lt;/"+tag+"&gt;")
		res = strings.ReplaceAll(res, "<"+tag+" ", "&lt;"+tag+" ")

		tagUpper := strings.ToUpper(tag)
		res = strings.ReplaceAll(res, "<"+tagUpper+">", "&lt;"+tagUpper+"&gt;")
		res = strings.ReplaceAll(res, "</"+tagUpper+">", "&lt;/"+tagUpper+"&gt;")
	}
	return res
}
