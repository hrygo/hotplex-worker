// Package feishu provides event extraction utilities for Feishu messages.
package feishu

import (
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// extractResponseText extracts text content from an AEP event for Feishu output.
func extractResponseText(env *events.Envelope) (string, bool) {
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
