package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── Command Detection in handleInput ──────────────────────────────────────
//
// When a user types a command-like text (help, control, worker command),
// handleInput intercepts it before reaching the worker input path.
// These tests verify the three-layer detection:
//
//  1. Help commands (?, /help, $help) → reply with help message
//  2. Control commands (/gc, /reset, $gc, etc.) → dispatch to handleControl
//  3. Worker commands (/context, /compact, etc.) → dispatch to handleWorkerCommand
//  4. Normal text → pass through to worker as before

// inputEnvelopeWithOwner creates an input envelope with OwnerID set for ownership validation.
func inputEnvelopeWithOwner(sessionID, ownerID, content string) *events.Envelope {
	env := events.NewEnvelope(aep.NewID(), sessionID, 1, events.Input, map[string]any{
		"content": content,
	})
	env.OwnerID = ownerID
	return env
}

// readNextEnvelope reads the next message from a websocket conn, decodes it as an Envelope.
func readNextEnvelope(t *testing.T, serverConn interface {
	SetReadDeadline(time.Time) error
	ReadMessage() (int, []byte, error)
}) events.Envelope {
	t.Helper()
	_ = serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := serverConn.ReadMessage()
	require.NoError(t, err)
	var env events.Envelope
	require.NoError(t, json.Unmarshal(data, &env))
	return env
}

// ─── Help Command Detection ────────────────────────────────────────────────

func TestHandleInput_HelpCommand_QuestionMark(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_help_q"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	// Join a WS conn so SendToSession routes to a real connection.
	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "?")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	// Read the help message from the server side of the WS pair.
	got := readNextEnvelope(t, serverConn)
	require.Equal(t, events.Message, got.Event.Type)
	msgData, ok := got.Event.Data.(map[string]any)
	require.True(t, ok)
	content, _ := msgData["content"].(string)
	require.Contains(t, content, "会话控制")
}

func TestHandleInput_HelpCommand_SlashHelp(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_help_slash"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "/help")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	got := readNextEnvelope(t, serverConn)
	require.Equal(t, events.Message, got.Event.Type)
	msgData, ok := got.Event.Data.(map[string]any)
	require.True(t, ok)
	content, _ := msgData["content"].(string)
	require.NotEmpty(t, content)
}

func TestHandleInput_HelpCommand_DollarHelp(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_help_dollar"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "$help")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	got := readNextEnvelope(t, serverConn)
	require.Equal(t, events.Message, got.Event.Type)
}

func TestHandleInput_HelpCommand_DoesNotReachWorker(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_help_no_worker"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	// Attach a worker that tracks if Input is called.
	// Also allow Terminate for mgr.Close() cleanup.
	w := new(mockWorkerForHandler)
	w.On("Input", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	w.On("Terminate", mock.Anything).Return(nil)
	require.NoError(t, mgr.AttachWorker(sid, w))

	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "?")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	// Read the help message so the WS buffer is drained.
	_ = readNextEnvelope(t, serverConn)

	// Worker.Input must NOT have been called for help command.
	w.AssertNotCalled(t, "Input", mock.Anything, mock.Anything, mock.Anything)
}

// ─── Control Command Detection ─────────────────────────────────────────────

func TestHandleInput_ControlCommand_GC(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_ctrl_gc"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	w := new(mockWorkerForHandler)
	w.On("Terminate", mock.Anything).Return(nil)
	require.NoError(t, mgr.AttachWorker(sid, w))

	clientConn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "/gc")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	// Session should transition to TERMINATED.
	si, err := mgr.Get(sid)
	require.NoError(t, err)
	require.Equal(t, events.StateTerminated, si.State)
}

func TestHandleInput_ControlCommand_Reset(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_ctrl_reset"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	w := new(mockWorkerForHandler)
	w.On("ResetContext", mock.Anything).Return(nil)
	w.On("Terminate", mock.Anything).Return(nil)
	require.NoError(t, mgr.AttachWorker(sid, w))

	clientConn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "/reset")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	// Session should remain RUNNING after reset.
	si, err := mgr.Get(sid)
	require.NoError(t, err)
	require.Equal(t, events.StateRunning, si.State)
}

func TestHandleInput_ControlCommand_DoesNotReachWorker(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_ctrl_no_worker"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	w := new(mockWorkerForHandler)
	// Only expect Terminate (called by handleGC), never Input.
	w.On("Terminate", mock.Anything).Return(nil)
	require.NoError(t, mgr.AttachWorker(sid, w))

	clientConn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "/gc")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	// Worker.Input must NOT have been called for control commands.
	w.AssertNotCalled(t, "Input", mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleInput_ControlCommand_NaturalLanguageGC(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_ctrl_nl_gc"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	w := new(mockWorkerForHandler)
	w.On("Terminate", mock.Anything).Return(nil)
	require.NoError(t, mgr.AttachWorker(sid, w))

	clientConn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	// $gc is the natural language trigger for GC.
	env := inputEnvelopeWithOwner(sid, "user1", "$gc")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	si, err := mgr.Get(sid)
	require.NoError(t, err)
	require.Equal(t, events.StateTerminated, si.State)
}

// ─── Worker Command Detection ──────────────────────────────────────────────

func TestHandleInput_WorkerCommand_Context(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_wcmd_ctx"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	w := &mockControlWorker{
		controlResp: map[string]any{
			"totalTokens": float64(10000),
			"maxTokens":   float64(200000),
			"percentage":  float64(5),
		},
	}
	require.NoError(t, mgr.AttachWorker(sid, w))

	clientConn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "/context")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	// ControlRequester.SendControlRequest should have been called.
	require.True(t, w.controlCalled, "SendControlRequest should be called for /context")
	require.Equal(t, "get_context_usage", w.controlSubtype)
}

func TestHandleInput_WorkerCommand_Compact(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_wcmd_compact"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	w := &mockCommanderWorker{}
	require.NoError(t, mgr.AttachWorker(sid, w))

	clientConn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "/compact")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	require.True(t, w.compactCalled, "Compact should be called for /compact")
}

func TestHandleInput_WorkerCommand_DoesNotSendAsPlainInput(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_wcmd_no_plain"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	w := &mockCommanderWorker{}
	require.NoError(t, mgr.AttachWorker(sid, w))

	clientConn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "/clear")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	// WorkerCommander.Clear should be called, NOT worker.Input with "/clear".
	require.True(t, w.clearCalled, "Clear should be called for /clear")
}

// ─── Normal Text Pass-through ──────────────────────────────────────────────

func TestHandleInput_NormalText_PassesToWorker(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_normal_text"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	w := new(mockWorkerForHandler)
	w.On("Input", mock.Anything, "hello world", mock.Anything).Return(nil)
	w.On("Terminate", mock.Anything).Return(nil)
	require.NoError(t, mgr.AttachWorker(sid, w))

	clientConn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "hello world")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	w.AssertCalled(t, "Input", mock.Anything, "hello world", mock.Anything)
}

func TestHandleInput_NormalText_LooksLikeCommandButIsNot(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_normal_likecmd"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	w := new(mockWorkerForHandler)
	// "please help me with this" contains "help" but is NOT a help command.
	w.On("Input", mock.Anything, "please help me with this", mock.Anything).Return(nil)
	w.On("Terminate", mock.Anything).Return(nil)
	require.NoError(t, mgr.AttachWorker(sid, w))

	clientConn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "please help me with this")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	// Should reach the worker as plain input.
	w.AssertCalled(t, "Input", mock.Anything, "please help me with this", mock.Anything)
}

func TestHandleInput_SlashPrefixNotInMaps_PassesToWorker(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_slash_unknown"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Transition(context.Background(), sid, events.StateRunning))

	w := new(mockWorkerForHandler)
	w.On("Input", mock.Anything, "/unknown_command", mock.Anything).Return(nil)
	w.On("Terminate", mock.Anything).Return(nil)
	require.NoError(t, mgr.AttachWorker(sid, w))

	clientConn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	// /unknown_command is not in any command map → should pass to worker.
	env := inputEnvelopeWithOwner(sid, "user1", "/unknown_command")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	w.AssertCalled(t, "Input", mock.Anything, "/unknown_command", mock.Anything)
}

// ─── sanitizeLastInput Tests ───────────────────────────────────────────────

func TestSanitizeLastInput_EmptyString(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", sanitizeLastInput(""))
}

func TestSanitizeLastInput_ControlCommandOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{"gc", "/gc"},
		{"reset", "/reset"},
		{"park", "/park"},
		{"dollar_gc", "$gc"},
		{"dollar_reset", "$reset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, "", sanitizeLastInput(tt.input))
		})
	}
}

func TestSanitizeLastInput_MixedLines(t *testing.T) {
	t.Parallel()
	input := "hello world\n/gc\nsome more text\n$reset\nfinal line"
	result := sanitizeLastInput(input)
	require.Equal(t, "hello world\nsome more text\nfinal line", result)
}

func TestSanitizeLastInput_NormalText_Unchanged(t *testing.T) {
	t.Parallel()
	input := "hello world\nthis is a test\nno commands here"
	require.Equal(t, input, sanitizeLastInput(input))
}

func TestSanitizeLastInput_AllControlCommands(t *testing.T) {
	t.Parallel()
	input := "/gc\n$reset\n/park"
	require.Equal(t, "", sanitizeLastInput(input))
}

func TestSanitizeLastInput_ControlCommandWithTrailingWhitespace(t *testing.T) {
	t.Parallel()
	// Lines with control commands + surrounding whitespace should be filtered.
	input := "hello\n  /gc  \nworld"
	result := sanitizeLastInput(input)
	require.Equal(t, "hello\nworld", result)
}

func TestSanitizeLastInput_SingleNormalLine(t *testing.T) {
	t.Parallel()
	require.Equal(t, "just text", sanitizeLastInput("just text"))
}

func TestSanitizeLastInput_CommandWithTrailingPunctuation(t *testing.T) {
	t.Parallel()
	// ParseControlCommand strips trailing punctuation, so "/gc." matches.
	input := "hello\n/gc.\nworld"
	result := sanitizeLastInput(input)
	require.Equal(t, "hello\nworld", result)
}

// ─── Edge Cases ────────────────────────────────────────────────────────────

func TestHandleInput_HelpCommand_WithWhitespace(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_help_ws"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	// Whitespace around help command should still be recognized.
	env := inputEnvelopeWithOwner(sid, "user1", "  ?  ")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	got := readNextEnvelope(t, serverConn)
	require.Equal(t, events.Message, got.Event.Type)
}

func TestHandleInput_HelpCommand_FullWidthQuestionMark(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_help_fwqm"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "？") // Full-width question mark
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)

	got := readNextEnvelope(t, serverConn)
	require.Equal(t, events.Message, got.Event.Type)
}

// ─── Integration: Full Handle() dispatch for command detection ─────────────

func TestHandle_FullDispatch_HelpCommand(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_handle_help"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	env := inputEnvelopeWithOwner(sid, "user1", "?")
	env.ID = aep.NewID()
	env.Seq = 1

	err = handler.Handle(context.Background(), env)
	require.NoError(t, err)

	got := readNextEnvelope(t, serverConn)
	require.Equal(t, events.Message, got.Event.Type)
	msgData, ok := got.Event.Data.(map[string]any)
	require.True(t, ok)
	content, _ := msgData["content"].(string)
	require.True(t, strings.Contains(content, "会话控制") || strings.Contains(content, "/gc"),
		"help text should contain command listing")
}

// Verify that messaging helpers are consistent with what the handler expects.
func TestCommandDetectionMessagingParity(t *testing.T) {
	t.Parallel()

	// Help commands should be detected.
	require.True(t, messaging.IsHelpCommand("?"))
	require.True(t, messaging.IsHelpCommand("/help"))
	require.True(t, messaging.IsHelpCommand("$help"))
	require.False(t, messaging.IsHelpCommand("help"))
	require.False(t, messaging.IsHelpCommand("please help"))

	// Control commands should be detected.
	require.NotNil(t, messaging.ParseControlCommand("/gc"))
	require.NotNil(t, messaging.ParseControlCommand("/reset"))
	require.NotNil(t, messaging.ParseControlCommand("$gc"))
	require.NotNil(t, messaging.ParseControlCommand("$reset"))
	require.Nil(t, messaging.ParseControlCommand("gc"))
	require.Nil(t, messaging.ParseControlCommand("/context"))

	// Worker commands should be detected.
	require.NotNil(t, messaging.ParseWorkerCommand("/context"))
	require.NotNil(t, messaging.ParseWorkerCommand("/compact"))
	require.NotNil(t, messaging.ParseWorkerCommand("/clear"))
	require.Nil(t, messaging.ParseWorkerCommand("context"))
	require.Nil(t, messaging.ParseWorkerCommand("/gc"))    // gc is control, not worker
	require.Nil(t, messaging.ParseWorkerCommand("/reset")) // reset is control, not worker
}
