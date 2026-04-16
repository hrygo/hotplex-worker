package claudecode

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// EventType represents the type of a parsed worker event.
type EventType string

const (
	EventStream       EventType = "stream"
	EventAssistant    EventType = "assistant"
	EventToolProgress EventType = "tool_progress"
	EventResult       EventType = "result"
	EventControl      EventType = "control"
	EventSystem       EventType = "system"
	EventSessionState EventType = "session_state"
	EventInterrupt    EventType = "interrupt" // internal: received from Claude Code
)

// StreamType represents Claude Code stream event subtypes.
type StreamType string

const (
	StreamThinking StreamType = "thinking"
	StreamText     StreamType = "text"
	StreamCode     StreamType = "code"
	StreamImage    StreamType = "image"
	StreamToolUse  StreamType = "tool_use"
)

// ControlSubtype represents Claude Code control_request subtypes.
type ControlSubtype string

const (
	ControlCanUseTool           ControlSubtype = "can_use_tool"
	ControlInterrupt            ControlSubtype = "interrupt"
	ControlSetPermissionMode    ControlSubtype = "set_permission_mode"
	ControlSetModel             ControlSubtype = "set_model"
	ControlSetMaxThinkingTokens ControlSubtype = "set_max_thinking_tokens"
	ControlMCPStatus            ControlSubtype = "mcp_status"
	ControlMCPSetServers        ControlSubtype = "mcp_set_servers"
	ControlMCPMessage           ControlSubtype = "mcp_message"
)

// WorkerEvent represents a parsed event ready for mapping to AEP.
type WorkerEvent struct {
	Type       EventType
	Payload    any         // Concrete type varies by event; use type assertion to dispatch
	RawMessage *SDKMessage // Original message for advanced handling
}

// StreamPayload represents streaming content.
type StreamPayload struct {
	Type      string // "thinking", "text", "tool_use", "code", "image"
	MessageID string
	Content   string
	Input     json.RawMessage // For tool_use
}

// ToolCallPayload represents a tool invocation.
type ToolCallPayload struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToolResultPayload represents a tool execution result.
type ToolResultPayload struct {
	ToolUseID string
	Output    any
	Error     string
}

// ResultPayload represents turn completion.
type ResultPayload struct {
	Success    bool
	Message    string
	Stats      map[string]any
	Usage      map[string]any
	ModelUsage map[string]any
}

// Parser parses SDK messages into WorkerEvents.
type Parser struct {
	log *slog.Logger
}

// NewParser creates a new Parser instance.
func NewParser(log *slog.Logger) *Parser {
	return &Parser{
		log: log,
	}
}

// Returns multiple events for compound messages (e.g., assistant with text + tool_use).
func (p *Parser) ParseLine(line string) ([]*WorkerEvent, error) {
	var msg SDKMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return nil, fmt.Errorf("parser: unmarshal: %w", err)
	}

	// Route by type
	switch msg.Type {
	case "stream_event":
		return p.parseStreamEvent(&msg)
	case "assistant":
		return p.parseAssistant(&msg)
	case "tool_progress":
		return p.parseToolProgress(&msg)
	case "result":
		return p.parseResult(&msg)
	case "control_request":
		return p.parseControlRequest(&msg)
	case "system":
		return p.parseSystem(&msg)
	case "session_state_changed":
		return p.parseSessionState(&msg)
	default:
		// Ignore unknown types (files_persisted, hook_*, task_*, rate_limit, etc.)
		p.log.Debug("parser: ignoring unknown message type", "type", msg.Type)
		return nil, nil
	}
}

// parseStreamEvent handles stream_event messages.
func (p *Parser) parseStreamEvent(msg *SDKMessage) ([]*WorkerEvent, error) {
	var streamEvt StreamEvent
	if err := json.Unmarshal(msg.Event, &streamEvt); err != nil {
		return nil, fmt.Errorf("parser: unmarshal stream_event: %w", err)
	}

	// Extract message ID
	messageID := streamEvt.Message.ID

	// Extract content based on event type
	var content string
	switch streamEvt.Type {
	case string(StreamThinking), string(StreamText), string(StreamCode):
		// Extract text from message content
		content = extractTextFromContent(streamEvt.Message.Content)
	case string(StreamImage):
		// Image content - extract as base64 or URL
		content = extractTextFromContent(streamEvt.Message.Content)
	case string(StreamToolUse):
		// Tool use event - will be handled separately
		return []*WorkerEvent{{
			Type: EventStream,
			Payload: &StreamPayload{
				Type:      streamEvt.Type,
				MessageID: messageID,
				Content:   streamEvt.Name, // Tool name
				Input:     streamEvt.Input,
			},
			RawMessage: msg,
		}}, nil
	default:
		p.log.Debug("parser: unknown stream_event type", "type", streamEvt.Type)
		return nil, nil
	}

	return []*WorkerEvent{{
		Type: EventStream,
		Payload: &StreamPayload{
			Type:      streamEvt.Type,
			MessageID: messageID,
			Content:   content,
		},
		RawMessage: msg,
	}}, nil
}

// parseAssistant handles assistant messages.
func (p *Parser) parseAssistant(msg *SDKMessage) ([]*WorkerEvent, error) {
	var assistantMsg AssistantMessage
	if err := json.Unmarshal(msg.Message, &assistantMsg); err != nil {
		return nil, fmt.Errorf("parser: unmarshal assistant: %w", err)
	}

	// Parse content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(assistantMsg.Content, &blocks); err != nil {
		return nil, fmt.Errorf("parser: unmarshal content blocks: %w", err)
	}

	var events []*WorkerEvent
	for _, block := range blocks {
		switch block.Type {
		case "text":
			events = append(events, &WorkerEvent{
				Type: EventAssistant,
				Payload: &StreamPayload{
					Type:    "text",
					Content: block.Text,
				},
				RawMessage: msg,
			})
		case "thinking":
			events = append(events, &WorkerEvent{
				Type: EventAssistant,
				Payload: &StreamPayload{
					Type:    "thinking",
					Content: block.Thinking,
				},
				RawMessage: msg,
			})
		case "tool_use":
			var input map[string]any
			if err := json.Unmarshal(block.Input, &input); err != nil {
				p.log.Warn("parser: unmarshal tool_use input", "error", err)
				input = make(map[string]any)
			}
			events = append(events, &WorkerEvent{
				Type: EventAssistant,
				Payload: &ToolCallPayload{
					ID:    block.ID,
					Name:  block.Name,
					Input: input,
				},
				RawMessage: msg,
			})
		}
	}

	return events, nil
}

// parseToolProgress handles tool_progress messages.
func (p *Parser) parseToolProgress(msg *SDKMessage) ([]*WorkerEvent, error) {
	var output any
	var errStr string

	// Try to parse content as tool result
	if len(msg.Content) > 0 {
		var toolResult struct {
			Content any    `json:"content"`
			Error   string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(msg.Content, &toolResult); err != nil {
			p.log.Warn("parser: unmarshal tool_progress content", "error", err)
			output = string(msg.Content)
		} else {
			output = toolResult.Content
			errStr = toolResult.Error
		}
	}

	return []*WorkerEvent{{
		Type: EventToolProgress,
		Payload: &ToolResultPayload{
			ToolUseID: msg.ToolUseID,
			Output:    output,
			Error:     errStr,
		},
		RawMessage: msg,
	}}, nil
}

// parseResult handles result messages (turn completion).
func (p *Parser) parseResult(msg *SDKMessage) ([]*WorkerEvent, error) {
	// Parse usage stats
	var usage, modelUsage map[string]any
	if len(msg.Usage) > 0 {
		_ = json.Unmarshal(msg.Usage, &usage)
	}
	if len(msg.ModelUsage) > 0 {
		_ = json.Unmarshal(msg.ModelUsage, &modelUsage)
	}

	// Build stats
	stats := map[string]any{
		"duration_ms":     msg.DurationMs,
		"duration_api_ms": msg.DurationAPIMs,
		"num_turns":       msg.NumTurns,
		"total_cost_usd":  msg.TotalCostUSD,
	}

	return []*WorkerEvent{{
		Type: EventResult,
		Payload: &ResultPayload{
			Success:    !msg.IsError,
			Message:    msg.Result,
			Stats:      stats,
			Usage:      usage,
			ModelUsage: modelUsage,
		},
		RawMessage: msg,
	}}, nil
}

// parseControlRequest handles control_request messages.
// All subtypes (can_use_tool, set_*, mcp_*, etc.) are returned as
// EventControl with Payload=*ControlRequestPayload, routing by Subtype in worker.go.
func (p *Parser) parseControlRequest(msg *SDKMessage) ([]*WorkerEvent, error) {
	var req ControlRequestPayload
	if err := json.Unmarshal(msg.Response, &req); err != nil {
		return nil, fmt.Errorf("parser: unmarshal control_request: %w", err)
	}
	req.RequestID = msg.RequestID // canonical source is outer SDKMessage

	switch req.Subtype {
	case string(ControlInterrupt):
		p.log.Debug("parser: received interrupt from Claude Code")
		return []*WorkerEvent{{
			Type:       EventInterrupt,
			RawMessage: msg,
		}}, nil

	default:
		// can_use_tool, set_permission_mode, set_model, mcp_status, etc.
		// worker.go dispatches by Subtype: can_use_tool → gateway, set_*/mcp_* → auto-success.
		return []*WorkerEvent{{
			Type:       EventControl,
			Payload:    &req,
			RawMessage: msg,
		}}, nil
	}
}

// parseSystem handles system messages.
func (p *Parser) parseSystem(msg *SDKMessage) ([]*WorkerEvent, error) {
	// Only forward status messages
	if msg.Subtype != "status" {
		return nil, nil
	}

	return []*WorkerEvent{{
		Type:       EventSystem,
		Payload:    msg.Status,
		RawMessage: msg,
	}}, nil
}

// parseSessionState handles session_state_changed messages.
func (p *Parser) parseSessionState(msg *SDKMessage) ([]*WorkerEvent, error) {
	return []*WorkerEvent{{
		Type:       EventSessionState,
		Payload:    msg.State,
		RawMessage: msg,
	}}, nil
}

// extractTextFromContent extracts text from various content formats.
func extractTextFromContent(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}

	// Try as string
	var str string
	if err := json.Unmarshal(content, &str); err == nil {
		return str
	}

	// Try as array of content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		var builder strings.Builder
		for _, block := range blocks {
			if block.Type == "text" && block.Text != "" {
				builder.WriteString(block.Text)
			}
		}
		return builder.String()
	}

	// Fallback: return raw JSON
	return string(content)
}
