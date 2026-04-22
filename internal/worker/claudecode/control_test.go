package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (lb *lockedBuffer) Write(p []byte) (int, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.buf.Write(p)
}

func (lb *lockedBuffer) Bytes() []byte {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.buf.Bytes()
}

func TestControlHandlerSendControlRequest(t *testing.T) {
	t.Parallel()

	var lb lockedBuffer
	h := NewControlHandler(slog.Default(), &lb)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		time.Sleep(50 * time.Millisecond)
		data := lb.Bytes()
		var req map[string]any
		require.NoError(t, json.Unmarshal(data, &req))
		reqID, _ := req["request_id"].(string)
		h.DeliverResponse(reqID, map[string]any{"status": "ok"})
	}()

	resp, err := h.SendControlRequest(ctx, "get_context_usage", nil)
	require.NoError(t, err)
	require.Equal(t, "ok", resp["status"])
}

func TestControlHandlerDeliverResponse(t *testing.T) {
	t.Parallel()

	var lb lockedBuffer
	h := NewControlHandler(slog.Default(), &lb)

	ch := make(chan map[string]any, 1)
	h.mu.Lock()
	h.pendingRequests["test-req-123"] = ch
	h.mu.Unlock()

	h.DeliverResponse("test-req-123", map[string]any{"result": "done"})

	select {
	case resp := <-ch:
		require.Equal(t, "done", resp["result"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
	}
}

func TestControlHandlerDeliverResponseUnknownID(t *testing.T) {
	t.Parallel()

	var lb lockedBuffer
	h := NewControlHandler(slog.Default(), &lb)

	require.NotPanics(t, func() {
		h.DeliverResponse("nonexistent-id", map[string]any{"result": "ignored"})
	})
}

func TestControlHandlerSendControlRequestContextCancel(t *testing.T) {
	t.Parallel()

	var lb lockedBuffer
	h := NewControlHandler(slog.Default(), &lb)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := h.SendControlRequest(ctx, "get_context_usage", nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestControlHandlerSendResponse(t *testing.T) {
	t.Parallel()

	var lb lockedBuffer
	h := NewControlHandler(slog.Default(), &lb)

	resp := &ControlResponse{
		Type: "control_response",
		Response: ResponsePayload{
			Subtype:   "success",
			RequestID: "req-001",
			Response:  map[string]any{"status": "ok"},
		},
	}
	require.NoError(t, h.SendResponse(resp))

	var parsed ControlResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(lb.Bytes()), &parsed))
	require.Equal(t, "control_response", parsed.Type)
	require.Equal(t, "success", parsed.Response.Subtype)
	require.Equal(t, "req-001", parsed.Response.RequestID)
}

func TestControlHandlerSendPermissionResponse(t *testing.T) {
	t.Parallel()

	var lb lockedBuffer
	h := NewControlHandler(slog.Default(), &lb)

	require.NoError(t, h.SendPermissionResponse("req-002", true, "user approved"))

	var parsed ControlResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(lb.Bytes()), &parsed))
	require.Equal(t, "success", parsed.Response.Subtype)
	require.Equal(t, "req-002", parsed.Response.RequestID)
	require.Equal(t, true, parsed.Response.Response["allowed"])
	require.Equal(t, "user approved", parsed.Response.Response["reason"])
}

func TestControlHandlerSendQuestionResponse(t *testing.T) {
	t.Parallel()

	var lb lockedBuffer
	h := NewControlHandler(slog.Default(), &lb)

	answers := map[string]string{"q1": "option_a"}
	require.NoError(t, h.SendQuestionResponse("req-003", answers))

	var parsed ControlResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(lb.Bytes()), &parsed))
	require.Equal(t, "req-003", parsed.Response.RequestID)
}

func TestControlHandlerHandlePayloadAutoSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subtype string
		reqID   string
	}{
		{"set_permission_mode", "set_permission_mode", "auto-001"},
		{"set_model", "set_model", "auto-002"},
		{"set_max_thinking_tokens", "set_max_thinking_tokens", "auto-003"},
		{"mcp_status", "mcp_status", "auto-004"},
		{"mcp_set_servers", "mcp_set_servers", "auto-005"},
		{"mcp_message", "mcp_message", "auto-006"},
		{"interrupt", "interrupt", "auto-007"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lb lockedBuffer
			h := NewControlHandler(slog.Default(), &lb)

			payload := &ControlRequestPayload{
				Subtype:   tt.subtype,
				RequestID: tt.reqID,
			}

			evt, err := h.HandlePayload(payload)
			require.NoError(t, err)
			require.Nil(t, evt)

			if tt.subtype != "interrupt" {
				var written ControlResponse
				require.NoError(t, json.Unmarshal(bytes.TrimSpace(lb.Bytes()), &written))
				require.Equal(t, "success", written.Response.Subtype)
				require.Equal(t, tt.reqID, written.Response.RequestID)
			}
		})
	}
}
