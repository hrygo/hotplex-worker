package claudecode

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// ControlResponse represents a response to Claude Code.
type ControlResponse struct {
	Type     string          `json:"type"`
	Response ResponsePayload `json:"response"`
}

// ResponsePayload represents the response payload.
type ResponsePayload struct {
	Subtype   string         `json:"subtype"`
	RequestID string         `json:"request_id"`
	Response  map[string]any `json:"response,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// ControlHandler handles bidirectional control protocol.
type ControlHandler struct {
	mu    sync.Mutex
	log   *slog.Logger
	stdin io.Writer // CLI stdin
}

// NewControlHandler creates a new ControlHandler instance.
func NewControlHandler(log *slog.Logger, stdin io.Writer) *ControlHandler {
	return &ControlHandler{
		log:   log,
		stdin: stdin,
	}
}

// HandlePayload processes already-unmarshaled control request fields.
// This avoids a second json.Unmarshal in the readOutput hot path.
// Note: can_use_tool is handled directly in parseControlRequest (parser.go),
// not through this method, so this method only handles auto-success subtypes.
func (h *ControlHandler) HandlePayload(payload *ControlRequestPayload) (*WorkerEvent, error) {
	switch payload.Subtype {
	case string(ControlInterrupt):
		h.log.Debug("control: received interrupt signal")
		return nil, nil

	case string(ControlSetPermissionMode), string(ControlSetModel), string(ControlSetMaxThinkingTokens):
		return nil, h.sendAutoSuccess(payload.RequestID)

	case string(ControlMCPStatus), string(ControlMCPSetServers), string(ControlMCPMessage):
		return nil, h.sendAutoSuccess(payload.RequestID)

	default:
		h.log.Warn("control: unknown request subtype", "subtype", payload.Subtype)
		return nil, nil
	}
}

// sendAutoSuccess sends a success response for auto-approved requests.
func (h *ControlHandler) sendAutoSuccess(reqID string) error {
	return h.sendResponse(reqID, map[string]any{"status": "ok"})
}

// sendResponse is the internal helper that constructs and sends the control_response
// envelope, eliminating duplication between sendAutoSuccess and SendPermissionResponse.
func (h *ControlHandler) sendResponse(reqID string, body map[string]any) error {
	return h.SendResponse(&ControlResponse{
		Type: "control_response",
		Response: ResponsePayload{
			Subtype:   "success",
			RequestID: reqID,
			Response:  body,
		},
	})
}

// SendResponse sends a control_response to Claude Code via stdin.
func (h *ControlHandler) SendResponse(resp *ControlResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("control: marshal response: %w", err)
	}
	data = append(data, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err = h.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("control: write response: %w", err)
	}

	h.log.Debug("control: sent response", "request_id", resp.Response.RequestID)
	return nil
}

// SendPermissionResponse sends a user's permission decision back to Claude Code.
func (h *ControlHandler) SendPermissionResponse(reqID string, allowed bool, reason string) error {
	return h.sendResponse(reqID, map[string]any{
		"allowed": allowed,
		"reason":  reason,
	})
}

// SendQuestionResponse sends a user's answers to an AskUserQuestion back to Claude Code.
func (h *ControlHandler) SendQuestionResponse(reqID string, answers map[string]string) error {
	return h.sendResponse(reqID, map[string]any{
		"behavior": "allow",
		"updatedInput": map[string]any{
			"answers": answers,
		},
	})
}

// SendElicitationResponse sends a user's response to an MCP Elicitation back to Claude Code.
func (h *ControlHandler) SendElicitationResponse(reqID, action string, content map[string]any) error {
	return h.sendResponse(reqID, map[string]any{
		"action":  action,
		"content": content,
	})
}
