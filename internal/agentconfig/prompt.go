package agentconfig

import (
	"fmt"
	"strings"
)

// BuildSystemPrompt assembles the full agent context (B+C channels) into a single
// system prompt. Used by both Claude Code (--append-system-prompt) and OpenCode
// Server (system field per message). XML tags delineate sections for clear boundary
// isolation regardless of delivery mechanism.
func BuildSystemPrompt(configs *AgentConfigs) string {
	if configs == nil || configs.IsEmpty() {
		return ""
	}

	var sections []string

	// B-channel: behavior-shaping content (highest priority, listed first).
	if configs.Soul != "" {
		sections = append(sections, fmt.Sprintf(
			"<agent-persona>\nIf SOUL.md is present, embody its persona and tone.\nFollow its guidance unless higher-priority instructions override it.\nAvoid stiff, generic replies.\n\n%s\n</agent-persona>",
			configs.Soul,
		))
	}
	if configs.Agents != "" {
		sections = append(sections, fmt.Sprintf("<workspace-rules>\n%s\n</workspace-rules>", configs.Agents))
	}
	if configs.Skills != "" {
		sections = append(sections, fmt.Sprintf("<tool-guide>\n%s\n</tool-guide>", configs.Skills))
	}

	// C-channel: reference context (no hedging — system prompt delivery).
	if configs.User != "" {
		sections = append(sections, fmt.Sprintf("<user-profile>\n%s\n</user-profile>", configs.User))
	}
	if configs.Memory != "" {
		sections = append(sections, fmt.Sprintf("<persistent-memory>\n%s\n</persistent-memory>", configs.Memory))
	}

	if len(sections) == 0 {
		return ""
	}

	return "<hotplex-context>\n\n" +
		strings.Join(sections, "\n\n") +
		"\n\n</hotplex-context>"
}
