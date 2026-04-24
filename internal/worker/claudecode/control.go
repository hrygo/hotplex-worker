package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"sync"

	"github.com/hrygo/hotplex/pkg/aep"
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
	mu              sync.Mutex
	log             *slog.Logger
	stdin           io.Writer // CLI stdin
	pendingRequests map[string]chan map[string]any
}

// NewControlHandler creates a new ControlHandler instance.
func NewControlHandler(log *slog.Logger, stdin io.Writer) *ControlHandler {
	return &ControlHandler{
		log:             log,
		stdin:           stdin,
		pendingRequests: make(map[string]chan map[string]any),
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

// SendControlRequest sends a control_request to Claude Code via stdin and waits for the response.
func (h *ControlHandler) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
	reqID := "ctx_" + aep.NewID()

	req := map[string]any{
		"type":       "control_request",
		"request_id": reqID,
		"request":    buildRequestBody(subtype, body),
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("control: marshal request: %w", err)
	}
	data = append(data, '\n')

	ch := make(chan map[string]any, 1)
	h.mu.Lock()
	h.pendingRequests[reqID] = ch
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pendingRequests, reqID)
		h.mu.Unlock()
		select {
		case <-ch:
		default:
		}
	}()

	h.mu.Lock()
	_, err = h.stdin.Write(data)
	h.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("control: write request: %w", err)
	}

	h.log.Debug("control: sent request", "request_id", reqID, "subtype", subtype)

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("control: response channel closed")
		}
		if errMsg, hasErr := resp["error"].(string); hasErr && errMsg != "" {
			return nil, fmt.Errorf("control: request failed: %s", errMsg)
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func buildRequestBody(subtype string, body map[string]any) map[string]any {
	req := make(map[string]any, len(body)+1)
	req["subtype"] = subtype
	maps.Copy(req, body)
	return req
}

// DeliverResponse routes a control_response back to the pending SendControlRequest caller.
func (h *ControlHandler) DeliverResponse(reqID string, resp map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch, ok := h.pendingRequests[reqID]
	if !ok {
		h.log.Debug("control: no pending request for response", "request_id", reqID)
		return
	}

	select {
	case ch <- resp:
	default:
		h.log.Warn("control: pending response channel full, dropping", "request_id", reqID)
	}
}
