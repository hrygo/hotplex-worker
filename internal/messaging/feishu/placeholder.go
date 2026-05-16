package feishu

import "github.com/hrygo/hotplex/internal/messaging/phrases"

// buildPlaceholderText constructs the placeholder card text with Feishu native stickers.
func buildPlaceholderText(p *phrases.Phrases) string {
	return ":Get: " + p.Random("greetings") + "\n:StatusFlashOfInspiration: " + p.Random("tips")
}

// buildPersonaText returns two distinct persona lines for the tool_activity area
// during the placeholder phase, giving the bot a lively "preparing" presence.
func buildPersonaText(p *phrases.Phrases) string {
	line1 := p.Random("persona")
	if line1 == "" {
		return ""
	}
	line2 := p.Random("persona")
	for attempts := 0; line2 == line1 && attempts < 3; attempts++ {
		line2 = p.Random("persona")
	}
	if line2 == "" || line2 == line1 {
		return line1
	}
	return line1 + "\n" + line2
}

// buildClosingText returns a single closing phrase for the tool_activity area
// when a turn completes, replacing the transient tool status with a personified sign-off.
func buildClosingText(p *phrases.Phrases) string {
	return p.Random("closings")
}
