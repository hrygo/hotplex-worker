package feishu

import (
	"github.com/hrygo/hotplex/pkg/events"
)

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
