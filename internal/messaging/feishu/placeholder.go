package feishu

import "github.com/hrygo/hotplex/internal/messaging/phrases"

// buildPlaceholderText constructs the placeholder card text with Feishu native stickers.
func buildPlaceholderText(p *phrases.Phrases) string {
	return ":Get: " + p.Random("greetings") + "\n:StatusFlashOfInspiration: " + p.Random("tips")
}
