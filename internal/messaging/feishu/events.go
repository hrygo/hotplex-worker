// Package feishu provides event extraction utilities for Feishu messages.
package feishu

import (
	"github.com/hrygo/hotplex/pkg/events"
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

// ExtractChatID extracts the Feishu chat_id from an AEP input envelope.
// It checks both the top-level Event.Data and the nested "metadata" map.
func ExtractChatID(env *events.Envelope) string {
	if env == nil {
		return ""
	}
	md, ok := env.Event.Data.(map[string]any)
	if !ok {
		return ""
	}
	// Top-level: used by clients that flatten metadata.
	if id, ok := md["chat_id"].(string); ok && id != "" {
		return id
	}
	// Nested: used by Bridge.MakeFeishuEnvelope.
	if meta, ok := md["metadata"].(map[string]any); ok {
		if id, ok := meta["chat_id"].(string); ok && id != "" {
			return id
		}
	}
	return ""
}
