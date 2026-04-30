package messaging

import (
	"github.com/hrygo/hotplex/pkg/events"
)

// ExtractResponseText extracts text content from an AEP event envelope.
// Returns the text and true if text was found, empty string and false otherwise.
func ExtractResponseText(env *events.Envelope) (string, bool) {
	if env == nil {
		return "", false
	}
	switch env.Event.Type {
	case "text", events.MessageDelta:
		if d, ok := env.Event.Data.(events.MessageDeltaData); ok {
			return d.Content, d.Content != ""
		}
		if m, ok := env.Event.Data.(map[string]any); ok {
			if text, ok := m["content"].(string); ok {
				return text, true
			}
			if text, ok := m["text"].(string); ok {
				return text, true
			}
		}
		if text, ok := env.Event.Data.(string); ok {
			return text, true
		}
	case "done":
		return "", false
	case "raw":
		if d, ok := env.Event.Data.(events.RawData); ok {
			if m, ok := d.Raw.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					return text, true
				}
			}
		}
	}
	return "", false
}

// ExtractErrorMessage tries ErrorData then map[string]any fallback.
func ExtractErrorMessage(env *events.Envelope) string {
	if d, ok := env.Event.Data.(events.ErrorData); ok {
		return d.Message
	}
	if m, ok := env.Event.Data.(map[string]any); ok {
		if msg, ok := m["message"].(string); ok {
			return msg
		}
	}
	return ""
}
