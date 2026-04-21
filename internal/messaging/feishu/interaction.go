package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hotplex/hotplex-worker/internal/messaging"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/hotplex/hotplex-worker/pkg/events"
)

// sendPermissionRequest posts a permission request card to Feishu.
// Since the Feishu WS client does not forward card.action.trigger events,
// the card is display-only — users respond by typing "允许/allow" or "拒绝/deny".
func (c *FeishuConn) sendPermissionRequest(ctx context.Context, env *events.Envelope) error {
	data, err := messaging.ExtractPermissionData(env)
	if err != nil {
		return fmt.Errorf("feishu: extract permission data: %w", err)
	}

	// Build header
	header := fmt.Sprintf("**⚠️ 工具执行授权**\nClaude Code 请求：\n📝 **%s**", data.ToolName)
	if data.Description != "" && data.Description != data.ToolName {
		header += fmt.Sprintf("\n> %s", data.Description)
	}

	// Args preview
	if len(data.Args) > 0 && data.Args[0] != "{}" {
		preview := data.Args[0]
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		header += fmt.Sprintf("\n```\n%s\n```", preview)
	}

	// Instruction text
	footer := "---\n💬 回复 **允许** 或 **拒绝** 来响应此请求"

	cardJSON := buildInteractionCard(header, footer)

	chatID := c.chatID
	c.adapter.log.Debug("feishu: sending permission request card", "chat", chatID, "request_id", data.ID)

	if err := c.adapter.sendCardMessage(ctx, chatID, cardJSON); err != nil {
		return fmt.Errorf("feishu: send permission card: %w", err)
	}

	c.adapter.registerInteraction(data.ID, env.SessionID, events.PermissionRequest, c)

	c.adapter.log.Info("feishu: permission request posted",
		"request_id", data.ID,
		"tool_name", data.ToolName,
		"chat", chatID)

	return nil
}

// sendQuestionRequest posts a question request card to Feishu.
func (c *FeishuConn) sendQuestionRequest(ctx context.Context, env *events.Envelope) error {
	data, err := messaging.ExtractQuestionData(env)
	if err != nil {
		return fmt.Errorf("feishu: extract question data: %w", err)
	}

	var sb strings.Builder
	for _, q := range data.Questions {
		headerLabel := q.Header
		if headerLabel == "" {
			headerLabel = "Question"
		}
		fmt.Fprintf(&sb, "**%s**\n%s\n", headerLabel, q.Question)

		// List options
		if len(q.Options) > 0 {
			for _, opt := range q.Options {
				label := opt.Label
				if opt.Description != "" {
					label += " — " + opt.Description
				}
				fmt.Fprintf(&sb, "- %s\n", label)
			}
		}
		sb.WriteString("\n")
	}

	footer := "---\n💬 回复选项文本或自定义答案来响应此问题"

	cardJSON := buildInteractionCard(sb.String(), footer)

	chatID := c.chatID
	if err := c.adapter.sendCardMessage(ctx, chatID, cardJSON); err != nil {
		return fmt.Errorf("feishu: send question card: %w", err)
	}

	c.adapter.registerInteraction(data.ID, env.SessionID, events.QuestionRequest, c)

	c.adapter.log.Info("feishu: question request posted",
		"request_id", data.ID,
		"questions", len(data.Questions))

	return nil
}

// sendElicitationRequest posts an MCP elicitation request card to Feishu.
func (c *FeishuConn) sendElicitationRequest(ctx context.Context, env *events.Envelope) error {
	data, err := messaging.ExtractElicitationData(env)
	if err != nil {
		return fmt.Errorf("feishu: extract elicitation data: %w", err)
	}

	header := fmt.Sprintf("**🔗 MCP Server Request**\n`%s` 请求输入：\n%s", data.MCPServerName, data.Message)

	var footer strings.Builder
	footer.WriteString("---\n")
	if data.URL != "" {
		fmt.Fprintf(&footer, "📎 [外部表单](%s)\n", data.URL)
	}
	footer.WriteString("💬 回复 **accept** 或 **decline** 来响应此请求")

	cardJSON := buildInteractionCard(header, footer.String())

	chatID := c.chatID
	if err := c.adapter.sendCardMessage(ctx, chatID, cardJSON); err != nil {
		return fmt.Errorf("feishu: send elicitation card: %w", err)
	}

	c.adapter.registerInteraction(data.ID, env.SessionID, events.ElicitationRequest, c)

	c.adapter.log.Info("feishu: elicitation request posted",
		"request_id", data.ID,
		"mcp_server", data.MCPServerName)

	return nil
}

// registerInteraction registers a pending interaction with the adapter's manager.
func (a *Adapter) registerInteraction(requestID, sessionID string, kind events.Kind, conn *FeishuConn) {
	a.interactions.Register(&messaging.PendingInteraction{
		ID:        requestID,
		SessionID: sessionID,
		Type:      kind,
		CreatedAt: time.Now(),
		Timeout:   messaging.DefaultInteractionTimeout,
		SendResponse: func(metadata map[string]any) {
			env := &events.Envelope{
				Version:   events.Version,
				ID:        requestID,
				SessionID: sessionID,
				Event: events.Event{
					Type: events.Input,
					Data: map[string]any{
						"content":  "",
						"metadata": metadata,
					},
				},
			}
			if a.bridge != nil {
				_ = a.bridge.Handle(context.Background(), env, conn)
			}
		},
	})
}

// checkPendingInteraction checks if a text message is a response to a pending
// interaction. Returns true if the text was consumed as an interaction response.
func (a *Adapter) checkPendingInteraction(ctx context.Context, text string, conn *FeishuConn) bool {
	if a.interactions.Len() == 0 {
		return false
	}

	conn.mu.RLock()
	sid := conn.sessionID
	conn.mu.RUnlock()

	var candidates []*messaging.PendingInteraction
	if sid != "" {
		candidates = a.interactions.GetBySession(sid)
	} else {
		candidates = a.interactions.GetAll()
	}
	if len(candidates) == 0 {
		return false
	}

	normalized := strings.ToLower(strings.TrimSpace(text))

	matched := candidates[0]

	var metadata map[string]any

	switch matched.Type {
	case events.PermissionRequest:
		allowed := normalized == "允许" || normalized == "allow" || normalized == "yes" || normalized == "是"
		if !allowed && normalized != "拒绝" && normalized != "deny" && normalized != "no" && normalized != "否" {
			return false // not a recognized permission response
		}
		reason := ""
		if !allowed {
			reason = "user denied"
		}
		metadata = map[string]any{
			"permission_response": map[string]any{
				"request_id": matched.ID,
				"allowed":    allowed,
				"reason":     reason,
			},
		}

	case events.QuestionRequest:
		metadata = map[string]any{
			"question_response": map[string]any{
				"id": matched.ID,
				"answers": map[string]string{
					"_": text, // pass the raw text as the answer
				},
			},
		}

	case events.ElicitationRequest:
		action := "accept"
		if normalized == "decline" || normalized == "拒绝" || normalized == "cancel" || normalized == "取消" {
			action = "decline"
		}
		metadata = map[string]any{
			"elicitation_response": map[string]any{
				"id":     matched.ID,
				"action": action,
			},
		}
	}

	// Complete (remove) the interaction
	if completed, ok := a.interactions.Complete(matched.ID); !ok {
		return false
	} else {
		_ = completed
	}

	// Send the response
	matched.SendResponse(metadata)

	a.log.Info("feishu: interaction response received via text",
		"request_id", matched.ID,
		"type", matched.Type,
		"text_preview", truncate(text, 50))

	// Send acknowledgment
	ackText := "✅ 已收到响应"
	if matched.Type == events.PermissionRequest {
		if d, ok := metadata["permission_response"].(map[string]any); ok {
			if allowed, _ := d["allowed"].(bool); allowed {
				ackText = "✅ 已允许"
			} else {
				ackText = "🚫 已拒绝"
			}
		}
	}

	_ = a.sendTextMessage(ctx, conn.chatID, ackText)

	return true
}

// sendCardMessage sends a CardKit v2 interactive card to a chat.
func (a *Adapter) sendCardMessage(ctx context.Context, chatID, cardJSON string) error {
	if a.larkClient == nil {
		return fmt.Errorf("feishu: lark client not initialized")
	}

	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType(larkim.MsgTypeInteractive).
		Content(cardJSON).
		Build()

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(body).
		Build()

	resp, err := a.larkClient.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu: send card message: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: send card message failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// buildInteractionCard builds a CardKit v2 card for interaction requests.
func buildInteractionCard(body, footer string) string {
	elements := []map[string]any{
		{"tag": "markdown", "content": body},
	}
	if footer != "" {
		elements = append(elements, map[string]any{"tag": "hr"})
		elements = append(elements, map[string]any{"tag": "markdown", "content": footer})
	}

	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{"wide_screen_mode": true},
		"body":   map[string]any{"elements": elements},
	}

	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(card)
	return strings.TrimRight(buf.String(), "\n")
}

// truncate shortens a string to maxLen.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
