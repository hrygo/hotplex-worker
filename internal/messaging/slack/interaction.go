package slack

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

// interactionActionPrefix is used to identify interaction button actions.
const interactionActionPrefix = "hp_interact"

// handleInteractionEvent processes Slack interactive component callbacks.
func (a *Adapter) handleInteractionEvent(ctx context.Context, evt socketmode.Event) {
	callback, ok := evt.Data.(slack.InteractionCallback)
	if !ok {
		return
	}

	// Only handle block kit button actions
	if callback.Type != slack.InteractionTypeBlockActions {
		return
	}

	for _, action := range callback.ActionCallback.BlockActions {
		if !strings.HasPrefix(action.ActionID, interactionActionPrefix+"/") {
			continue
		}

		// Parse: "hp_interact/<type>/<requestID>"
		parts := strings.SplitN(action.ActionID, "/", 3)
		if len(parts) != 3 {
			continue
		}

		interactionType := parts[1]
		requestID := parts[2]
		channelID := callback.Channel.ID
		threadTS := callback.MessageTs
		userID := callback.User.ID

		a.log.Info("slack: interaction callback",
			"type", interactionType,
			"request_id", requestID,
			"user_id", userID,
			"value", action.Value)

		// Acknowledge the interaction to Slack
		_ = a.socketMode.Ack(*evt.Request)

		// Look up the pending interaction
		pi, ok := a.interactions.Complete(requestID)
		if !ok {
			a.log.Warn("slack: interaction not found or expired", "request_id", requestID)
			continue
		}

		// Build response metadata and send through the bridge
		switch interactionType {
		case "allow":
			pi.SendResponse(map[string]any{
				"permission_response": map[string]any{
					"request_id": requestID,
					"allowed":    true,
					"reason":     "",
				},
			})
		case "deny":
			pi.SendResponse(map[string]any{
				"permission_response": map[string]any{
					"request_id": requestID,
					"allowed":    false,
					"reason":     "user denied",
				},
			})
		case "answer":
			pi.SendResponse(map[string]any{
				"question_response": map[string]any{
					"id": requestID,
					"answers": map[string]string{
						"_": action.Value,
					},
				},
			})
		case "accept":
			pi.SendResponse(map[string]any{
				"elicitation_response": map[string]any{
					"id":     requestID,
					"action": "accept",
				},
			})
		case "decline":
			pi.SendResponse(map[string]any{
				"elicitation_response": map[string]any{
					"id":     requestID,
					"action": "decline",
				},
			})
		}

		// Update the message to show the user's choice
		_, _, _, err := a.client.UpdateMessageContext(ctx, channelID, threadTS,
			slack.MsgOptionText(fmt.Sprintf("_Response recorded by <@%s>_", userID), false),
		)
		if err != nil {
			a.log.Debug("slack: update interaction message", "err", err)
		}

		_ = threadTS // thread context
	}
}

// buildPermissionFallbackText creates plain-text fallback for permission request.
// AC-3.6: Permission request has a fallbackText that conveys same info without blocks
func buildPermissionFallbackText(data *events.PermissionRequestData) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "*Tool Approval Required*\nClaude Code requests permission to run: `%s`\n", data.ToolName)

	if data.Description != "" && data.Description != data.ToolName {
		fmt.Fprintf(&sb, "Description: %s\n", data.Description)
	}

	if len(data.Args) > 0 && data.Args[0] != `{}` {
		preview := data.Args[0]
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		fmt.Fprintf(&sb, "Args: %s\n", preview)
	}

	fmt.Fprintf(&sb, "\nReply with 'allow %s' or 'deny %s'", data.ID, data.ID)
	return sb.String()
}

// sendPermissionRequest posts a permission request UI to Slack.
func (c *SlackConn) sendPermissionRequest(ctx context.Context, env *events.Envelope) error {
	data, err := messaging.ExtractPermissionData(env)
	if err != nil {
		return fmt.Errorf("slack: extract permission data: %w", err)
	}

	// Build the header text
	headerText := fmt.Sprintf("*Tool Approval Required*\nClaude Code requests permission to run:\n`%s`", data.ToolName)
	if data.Description != "" && data.Description != data.ToolName {
		headerText += fmt.Sprintf("\n> %s", data.Description)
	}

	// Show args preview if available
	if len(data.Args) > 0 && data.Args[0] != `{}` {
		preview := data.Args[0]
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		headerText += fmt.Sprintf("\n```%s```", preview)
	}

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, headerText, false, false),
			nil, nil,
		),
		slack.NewActionBlock(
			"permission_actions",
			slack.NewButtonBlockElement(
				interactionActionPrefix+"/allow/"+data.ID,
				"allow",
				slack.NewTextBlockObject(slack.PlainTextType, "Allow", false, true),
			).WithStyle(slack.StylePrimary),
			slack.NewButtonBlockElement(
				interactionActionPrefix+"/deny/"+data.ID,
				"deny",
				slack.NewTextBlockObject(slack.PlainTextType, "Deny", false, true),
			).WithStyle(slack.StyleDanger),
		),
	}

	// Sanitize blocks before sending
	blocks = SanitizeBlocks(blocks)

	opts := []slack.MsgOption{slack.MsgOptionBlocks(blocks...)}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}

	_, msgTS, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	if err != nil {
		// AC-3.5: On invalid_blocks API error, message is resent as plain text
		if isInvalidBlocksError(err) {
			fallbackText := buildPermissionFallbackText(data)
			fallbackOpts := []slack.MsgOption{slack.MsgOptionText(fallbackText, false)}
			if c.threadTS != "" {
				fallbackOpts = append(fallbackOpts, slack.MsgOptionTS(c.threadTS))
			}
			_, msgTS, err = c.adapter.client.PostMessageContext(ctx, c.channelID, fallbackOpts...)
			if err != nil {
				return fmt.Errorf("slack: post permission request (fallback): %w", err)
			}
			c.adapter.log.Warn("slack: sent permission request as plain text fallback", "request_id", data.ID)
		} else {
			return fmt.Errorf("slack: post permission request: %w", err)
		}
	}

	// Register the pending interaction with timeout
	c.adapter.registerInteraction(data.ID, env.SessionID, events.PermissionRequest, msgTS, c)

	c.adapter.log.Info("slack: permission request posted",
		"request_id", data.ID,
		"tool_name", data.ToolName,
		"channel", c.channelID,
		"thread", c.threadTS)

	return nil
}

// buildQuestionFallbackText creates plain-text fallback for question request.
// AC-3.7: Question request has a fallbackText with numbered options
func buildQuestionFallbackText(data *events.QuestionRequestData) string {
	var sb strings.Builder
	sb.WriteString("*Question Request*\n")

	for i, q := range data.Questions {
		headerLabel := q.Header
		if headerLabel == "" {
			headerLabel = "Question"
		}
		fmt.Fprintf(&sb, "\n*%s %d:* %s\n", headerLabel, i+1, q.Question)

		if len(q.Options) > 0 {
			sb.WriteString("Options:\n")
			for j, opt := range q.Options {
				label := opt.Label
				if opt.Description != "" {
					label += " — " + opt.Description
				}
				fmt.Fprintf(&sb, "  %d. %s\n", j+1, label)
			}
		}
	}

	fmt.Fprintf(&sb, "\nReply with the option number for request %s", data.ID)
	return sb.String()
}

// sendQuestionRequest posts a question request UI to Slack.
func (c *SlackConn) sendQuestionRequest(ctx context.Context, env *events.Envelope) error {
	data, err := messaging.ExtractQuestionData(env)
	if err != nil {
		return fmt.Errorf("slack: extract question data: %w", err)
	}

	var blocks []slack.Block

	for _, q := range data.Questions {
		headerLabel := q.Header
		if headerLabel == "" {
			headerLabel = "Question"
		}

		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("*%s*\n%s", headerLabel, q.Question), false, false),
			nil, nil,
		))

		// Build option buttons
		var buttons []slack.BlockElement
		for _, opt := range q.Options {
			label := opt.Label
			if opt.Description != "" {
				label += " — " + opt.Description
			}
			if len(label) > 75 {
				label = label[:72] + "..."
			}
			buttons = append(buttons, slack.NewButtonBlockElement(
				interactionActionPrefix+"/answer/"+data.ID,
				opt.Label,
				slack.NewTextBlockObject(slack.PlainTextType, label, false, true),
			))
		}

		if len(buttons) > 0 {
			blocks = append(blocks, slack.NewActionBlock(
				fmt.Sprintf("question_%s", data.ID),
				buttons...,
			))
		}
	}

	// Sanitize blocks before sending
	blocks = SanitizeBlocks(blocks)

	opts := []slack.MsgOption{slack.MsgOptionBlocks(blocks...)}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}

	_, msgTS, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	if err != nil {
		// AC-3.5: On invalid_blocks API error, message is resent as plain text
		if isInvalidBlocksError(err) {
			fallbackText := buildQuestionFallbackText(data)
			fallbackOpts := []slack.MsgOption{slack.MsgOptionText(fallbackText, false)}
			if c.threadTS != "" {
				fallbackOpts = append(fallbackOpts, slack.MsgOptionTS(c.threadTS))
			}
			_, msgTS, err = c.adapter.client.PostMessageContext(ctx, c.channelID, fallbackOpts...)
			if err != nil {
				return fmt.Errorf("slack: post question request (fallback): %w", err)
			}
			c.adapter.log.Warn("slack: sent question request as plain text fallback", "request_id", data.ID)
		} else {
			return fmt.Errorf("slack: post question request: %w", err)
		}
	}

	c.adapter.registerInteraction(data.ID, env.SessionID, events.QuestionRequest, msgTS, c)

	c.adapter.log.Info("slack: question request posted",
		"request_id", data.ID,
		"questions", len(data.Questions))

	return nil
}

// buildElicitationFallbackText creates plain-text fallback for elicitation request.
// AC-3.8: Elicitation request has a fallbackText
func buildElicitationFallbackText(data *events.ElicitationRequestData) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "*MCP Server Request*\n`%s` requests your input:\n%s\n",
		data.MCPServerName, data.Message)

	if data.URL != "" {
		fmt.Fprintf(&sb, "\nOpen external form: %s\n", data.URL)
	}

	fmt.Fprintf(&sb, "\nReply with 'accept %s' or 'decline %s'", data.ID, data.ID)
	return sb.String()
}

// sendElicitationRequest posts an MCP elicitation request UI to Slack.
func (c *SlackConn) sendElicitationRequest(ctx context.Context, env *events.Envelope) error {
	data, err := messaging.ExtractElicitationData(env)
	if err != nil {
		return fmt.Errorf("slack: extract elicitation data: %w", err)
	}

	headerText := fmt.Sprintf("*MCP Server Request*\n`%s` requests your input:\n%s",
		data.MCPServerName, data.Message)

	if data.URL != "" {
		headerText += fmt.Sprintf("\n<%s|Open external form>", data.URL)
	}

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, headerText, false, false),
			nil, nil,
		),
		slack.NewActionBlock(
			"elicitation_actions",
			slack.NewButtonBlockElement(
				interactionActionPrefix+"/accept/"+data.ID,
				"accept",
				slack.NewTextBlockObject(slack.PlainTextType, "Accept", false, true),
			).WithStyle(slack.StylePrimary),
			slack.NewButtonBlockElement(
				interactionActionPrefix+"/decline/"+data.ID,
				"decline",
				slack.NewTextBlockObject(slack.PlainTextType, "Decline", false, true),
			).WithStyle(slack.StyleDanger),
		),
	}

	// Sanitize blocks before sending
	blocks = SanitizeBlocks(blocks)

	opts := []slack.MsgOption{slack.MsgOptionBlocks(blocks...)}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}

	_, msgTS, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	if err != nil {
		// AC-3.5: On invalid_blocks API error, message is resent as plain text
		if isInvalidBlocksError(err) {
			fallbackText := buildElicitationFallbackText(data)
			fallbackOpts := []slack.MsgOption{slack.MsgOptionText(fallbackText, false)}
			if c.threadTS != "" {
				fallbackOpts = append(fallbackOpts, slack.MsgOptionTS(c.threadTS))
			}
			_, msgTS, err = c.adapter.client.PostMessageContext(ctx, c.channelID, fallbackOpts...)
			if err != nil {
				return fmt.Errorf("slack: post elicitation request (fallback): %w", err)
			}
			c.adapter.log.Warn("slack: sent elicitation request as plain text fallback", "request_id", data.ID)
		} else {
			return fmt.Errorf("slack: post elicitation request: %w", err)
		}
	}

	c.adapter.registerInteraction(data.ID, env.SessionID, events.ElicitationRequest, msgTS, c)

	return nil
}

// registerInteraction registers a pending interaction with the adapter's manager.
func (a *Adapter) registerInteraction(requestID, sessionID string, kind events.Kind, _ string, conn *SlackConn) {
	a.interactions.Register(&messaging.PendingInteraction{
		ID:        requestID,
		SessionID: sessionID,
		Type:      kind,
		CreatedAt: getTimeNow(),
		Timeout:   messaging.DefaultInteractionTimeout,
		SendResponse: func(metadata map[string]any) {
			respCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
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
				OwnerID: "",
			}
			if a.bridge != nil {
				_ = a.bridge.Handle(respCtx, env, conn)
			}
		},
	})
}

// getTimeNow returns the current time. Extracted for testability.
var getTimeNow = time.Now
