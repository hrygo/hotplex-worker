package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/pkg/events"
)

func (c *FeishuConn) sendSkillsList(ctx context.Context, env *events.Envelope) error {
	var d events.SkillsListData
	switch v := env.Event.Data.(type) {
	case events.SkillsListData:
		d = v
	case map[string]any:
		raw, _ := json.Marshal(v)
		_ = json.Unmarshal(raw, &d)
	default:
		return nil
	}

	if len(d.Skills) == 0 {
		return c.sendSkillsText(ctx, "⚡ No skills found.")
	}

	// Batch: max 20 skills per message to stay within Feishu message size limits.
	const batchSize = 20
	for i := 0; i < len(d.Skills); i += batchSize {
		end := i + batchSize
		if end > len(d.Skills) {
			end = len(d.Skills)
		}
		batch := d.Skills[i:end]

		var sb strings.Builder
		if i == 0 {
			fmt.Fprintf(&sb, "⚡ Skills (%d)", d.Total)
		}
		partNum := i/batchSize + 1
		totalParts := (len(d.Skills) + batchSize - 1) / batchSize
		if totalParts > 1 {
			fmt.Fprintf(&sb, " — Part %d/%d", partNum, totalParts)
		}
		sb.WriteByte('\n')

		for _, s := range batch {
			icon := "\U0001F310" // 🌐 global
			if s.Source == "project" {
				icon = "\U0001F4C1" // 📁 project
			}
			desc := s.Description
			if len([]rune(desc)) > 80 {
				desc = string([]rune(desc)[:77]) + "..."
			}
			fmt.Fprintf(&sb, "%s %s — %s\n", icon, s.Name, desc)
		}

		if end < len(d.Skills) {
			sb.WriteString("…")
		}

		if err := c.sendSkillsText(ctx, sb.String()); err != nil {
			return err
		}
	}
	return nil
}

func (c *FeishuConn) sendSkillsText(ctx context.Context, text string) error {
	c.mu.RLock()
	chatID := c.chatID
	replyToMsgID := c.replyToMsgID
	c.mu.RUnlock()

	if replyToMsgID != "" {
		return c.adapter.replyMessage(ctx, replyToMsgID, text, false)
	}
	return c.adapter.sendTextMessage(ctx, chatID, text)
}
