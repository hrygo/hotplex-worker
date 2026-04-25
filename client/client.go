package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// ErrNotConnected is returned when sending before connection.
var ErrNotConnected = errors.New("client: not connected")

// Protocol constants (matching gateway defaults).
const (
	DefaultPingInterval = 54 * time.Second
	SendChannelCap      = 100
	InitTimeout         = 30 * time.Second
)

// Client is the HotPlex Worker Gateway client.
// It implements the AEP v1 WebSocket protocol.
type Client struct {
	// config from options
	url             string
	workerType      string
	authToken       string
	apiKey          string
	clientSessionID string

	// heartbeat config
	pingInterval time.Duration

	// reconnection config
	autoReconnect bool
	metadata      map[string]any

	// runtime state
	mu        sync.Mutex
	conn      *websocket.Conn
	sessionID string
	state     SessionState
	seq       int64
	closed    bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	sendCh   chan []byte
	eventsCh chan Event

	logger *slog.Logger
}

// Event is an inbound event delivered via the Events() channel.
type Event struct {
	Type    string `json:"type"`
	Seq     int64  `json:"seq"`
	Session string `json:"session"`
	Data    any    `json:"data,omitempty"`
}

// decodeAs is a generic helper that round-trips map[string]any through JSON
// to produce a typed struct. Returns (zero, false) if data is not a map.
func decodeAs[T any](data any) (T, bool) {
	d, ok := data.(map[string]any)
	if !ok {
		var zero T
		return zero, false
	}
	b, _ := json.Marshal(d)
	var res T
	_ = json.Unmarshal(b, &res)
	return res, true
}

// AsDoneData parses event data as DoneData.
func (e Event) AsDoneData() (DoneData, bool) { return decodeAs[DoneData](e.Data) }

// AsErrorData parses event data as ErrorData.
func (e Event) AsErrorData() (ErrorData, bool) { return decodeAs[ErrorData](e.Data) }

// AsToolCallData parses event data as ToolCallData.
func (e Event) AsToolCallData() (ToolCallData, bool) { return decodeAs[ToolCallData](e.Data) }

// AsPermissionRequestData parses event data as PermissionRequestData.
func (e Event) AsPermissionRequestData() (PermissionRequestData, bool) {
	return decodeAs[PermissionRequestData](e.Data)
}

// AsQuestionRequestData parses event data as QuestionRequestData.
func (e Event) AsQuestionRequestData() (QuestionRequestData, bool) {
	return decodeAs[QuestionRequestData](e.Data)
}

// AsElicitationRequestData parses event data as ElicitationRequestData.
func (e Event) AsElicitationRequestData() (ElicitationRequestData, bool) {
	return decodeAs[ElicitationRequestData](e.Data)
}

// AsMessageStartData parses event data as MessageStartData.
func (e Event) AsMessageStartData() (MessageStartData, bool) {
	return decodeAs[MessageStartData](e.Data)
}

// AsMessageDeltaData parses event data as MessageDeltaData.
func (e Event) AsMessageDeltaData() (MessageDeltaData, bool) {
	return decodeAs[MessageDeltaData](e.Data)
}

// AsMessageEndData parses event data as MessageEndData.
func (e Event) AsMessageEndData() (MessageEndData, bool) { return decodeAs[MessageEndData](e.Data) }

// AsStateData parses event data as StateData.
func (e Event) AsStateData() (StateData, bool) { return decodeAs[StateData](e.Data) }

// AsReasoningData parses event data as ReasoningData.
func (e Event) AsReasoningData() (ReasoningData, bool) { return decodeAs[ReasoningData](e.Data) }

// AsStepData parses event data as StepData.
func (e Event) AsStepData() (StepData, bool) { return decodeAs[StepData](e.Data) }

// AsToolResultData parses event data as ToolResultData.
func (e Event) AsToolResultData() (ToolResultData, bool) { return decodeAs[ToolResultData](e.Data) }

// AsInitAckData parses event data as InitAckData.
func (e Event) AsInitAckData() (InitAckData, bool) { return decodeAs[InitAckData](e.Data) }

// New creates a new client with the given options.
func New(ctx context.Context, opts ...Option) (*Client, error) {
	c := &Client{
		pingInterval: DefaultPingInterval,
		sendCh:       make(chan []byte, SendChannelCap),
		eventsCh:     make(chan Event, SendChannelCap),
		logger:       slog.Default(),
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	if c.url == "" {
		return nil, errors.New("client: URL is required")
	}
	if c.workerType == "" {
		return nil, errors.New("client: workerType is required")
	}
	c.ctx, c.cancel = context.WithCancel(ctx)
	return c, nil
}

// Connect establishes a new session with the gateway.
func (c *Client) Connect(ctx context.Context) (*InitAckData, error) {
	sessionID := c.clientSessionID
	if sessionID == "" {
		sessionID = aep.NewSessionID()
	}

	if !c.autoReconnect {
		return c.doConnect(ctx, sessionID, false)
	}

	// Reconnection loop
	var (
		ack     *InitAckData
		err     error
		attempt int
	)
	for {
		ack, err = c.doConnect(ctx, sessionID, attempt > 0)
		if err == nil {
			return ack, nil
		}

		attempt++
		c.logger.Warn("client: connect failed, retrying", "attempt", attempt, "err", err)

		backoff := backoffDuration(attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.ctx.Done():
			return nil, errors.New("client: closed")
		case <-time.After(backoff):
			// retry
		}
	}
}

// Resume attaches to an existing session.
func (c *Client) Resume(ctx context.Context, sessionID string) (*InitAckData, error) {
	return c.doConnect(ctx, sessionID, true)
}

func (c *Client) doConnect(ctx context.Context, sessionID string, isResume bool) (*InitAckData, error) {
	hdr := http.Header{}
	if c.authToken != "" {
		hdr.Set("Authorization", "Bearer "+c.authToken)
	}
	if c.apiKey != "" {
		hdr.Set("X-API-Key", c.apiKey)
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.url, hdr)
	if err != nil {
		return nil, fmt.Errorf("client: dial: %w", err)
	}
	c.conn = conn
	c.sessionID = sessionID

	// Build and send init envelope.
	initData := map[string]any{
		"version":     events.Version,
		"worker_type": c.workerType,
		"client_caps": map[string]any{
			"supports_delta":     true,
			"supports_tool_call": true,
			"supported_kinds": []string{
				"error", "state", "done", "message", "message.start",
				"message.delta", "message.end", "tool_call", "tool_result",
				"reasoning", "step", "raw", "permission_request",
				"control", "ping", "pong", "question_request",
				"question_response", "elicitation_request", "elicitation_response",
			},
		},
	}
	if c.metadata != nil {
		initData["config"] = map[string]any{"metadata": c.metadata}
	}
	if c.authToken != "" {
		initData["auth"] = map[string]any{"token": c.authToken}
	}
	if c.clientSessionID != "" || isResume {
		initData["session_id"] = sessionID
	}

	env := aep.NewEnvelope(aep.NewID(), sessionID, 1, events.Init, initData)
	env.Priority = PriorityControl
	frame, err := aep.EncodeJSON(env)
	if err != nil {
		if c.conn != nil {
			_ = c.conn.Close()
		}
		return nil, fmt.Errorf("client: send init: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("client: send init: %w", err)
	}

	// Read init_ack. Use raw JSON decode to avoid strict Validate()
	// (init_ack from gateway may not satisfy all envelope requirements).
	//
	// The gateway may send other events (e.g. state) before init_ack arrives
	// due to a race between the ReadPump goroutine (sending init_ack) and the
	// Run goroutine (routing state events). Skip non-init_ack messages until
	// the handshake response is received.
	var ackEnv events.Envelope
	var preInitEvents []Event
	for {
		_, r, err := conn.NextReader()
		if err != nil {
			if c.conn != nil {
				_ = c.conn.Close()
			}
			return nil, fmt.Errorf("client: read init ack: %w", err)
		}
		raw, err := io.ReadAll(r)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("client: read init ack body: %w", err)
		}

		if err := json.Unmarshal(raw, &ackEnv); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("client: decode init ack: %w", err)
		}
		if ackEnv.Event.Type == "init_ack" {
			break
		}
		// Buffer pre-init_ack events for delivery after the handshake.
		// The gateway may route state events before init_ack due to a race
		// between the Bridge (starting worker) and Conn (sending init_ack).
		preInitEvents = append(preInitEvents, Event{
			Type:    string(ackEnv.Event.Type),
			Seq:     ackEnv.Seq,
			Session: ackEnv.SessionID,
			Data:    ackEnv.Event.Data,
		})
	}

	ack := parseInitAck(&ackEnv)

	c.mu.Lock()
	c.conn = conn
	c.sessionID = ack.SessionID
	c.state = ack.State
	c.seq = 1
	c.mu.Unlock()

	c.wg.Add(3)
	go c.recvPump()
	go c.sendPump()
	go c.pingPump()

	// Deliver any events buffered during the handshake.
	for _, evt := range preInitEvents {
		c.deliver(evt)
	}

	return ack, nil
}

// Events returns a receive-only channel of inbound events.
func (c *Client) Events() <-chan Event {
	return c.eventsCh
}

// SessionID returns the current session ID.
func (c *Client) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

// State returns the current session state.
func (c *Client) State() SessionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// SendInput sends a user input message.
func (c *Client) SendInput(ctx context.Context, content string, metadata ...map[string]any) error {
	data := map[string]any{"content": content}
	if len(metadata) > 0 && metadata[0] != nil {
		data["metadata"] = metadata[0]
	}
	return c.send(ctx, events.Input, data, PriorityData)
}

// SendPermissionResponse approves or denies a tool permission.
func (c *Client) SendPermissionResponse(ctx context.Context, id string, approved bool, reason string) error {
	data := map[string]any{"id": id, "allowed": approved}
	if reason != "" {
		data["reason"] = reason
	}
	return c.send(ctx, events.PermissionResponse, data, PriorityControl)
}

// SendQuestionResponse sends answers to a question request.
func (c *Client) SendQuestionResponse(ctx context.Context, id string, answers map[string]string) error {
	return c.send(ctx, events.QuestionResponse, QuestionResponseData{
		ID:      id,
		Answers: answers,
	}, PriorityControl)
}

// SendElicitationResponse sends a response to an elicit request.
func (c *Client) SendElicitationResponse(ctx context.Context, id, action string, content map[string]any) error {
	return c.send(ctx, events.ElicitationResponse, ElicitationResponseData{
		ID:      id,
		Action:  action,
		Content: content,
	}, PriorityControl)
}

// SendControl sends a control action ("terminate" | "delete").
func (c *Client) SendControl(ctx context.Context, action string) error {
	return c.send(ctx, events.Control, &events.ControlData{
		Action: events.ControlAction(action),
	}, PriorityControl)
}

// SendReset sends a reset control action to clear session context.
// The session will restart with a fresh worker, preserving the session ID.
func (c *Client) SendReset(ctx context.Context, reason string) error {
	return c.sendControlWithReason(ctx, events.ControlActionReset, reason)
}

// SendGC sends a gc control action to archive the session.
// The worker is terminated but session history is preserved for resume.
func (c *Client) SendGC(ctx context.Context, reason string) error {
	return c.sendControlWithReason(ctx, events.ControlActionGC, reason)
}

func (c *Client) sendControlWithReason(ctx context.Context, action events.ControlAction, reason string) error {
	data := &events.ControlData{Action: action}
	if reason != "" {
		data.Reason = reason
	}
	return c.send(ctx, events.Control, data, PriorityControl)
}

// Close gracefully shuts down the client.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conn := c.conn
	c.mu.Unlock()

	// Cancel context to unblock sendPump (select on ctx.Done) and pingPump.
	c.cancel()
	// Close the WebSocket connection to unblock recvPump (NextReader).
	// This must happen before wg.Wait() to avoid deadlock.
	if conn != nil {
		_ = conn.Close()
	}
	// Close sendCh to unblock sendPump (range c.sendCh).
	// Safe because c.closed=true prevents new writes to sendCh.
	close(c.sendCh)
	c.wg.Wait()
	close(c.eventsCh)
	return nil
}

// ─── Private ─────────────────────────────────────────────────────────────────

func (c *Client) send(ctx context.Context, kind events.Kind, data any, priority Priority) error {
	c.mu.Lock()
	closed := c.closed
	sessionID := c.sessionID
	c.seq++
	seq := c.seq
	conn := c.conn
	c.mu.Unlock()

	if conn == nil || closed {
		return ErrNotConnected
	}

	env := aep.NewEnvelope(aep.NewID(), sessionID, seq, kind, data)
	env.Priority = priority
	frame, err := aep.EncodeJSON(env)
	if err != nil {
		return err
	}
	select {
	case c.sendCh <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return ErrNotConnected
	}
}

func (c *Client) recvPump() {
	defer c.wg.Done()
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}

		_, r, err := conn.NextReader()
		if err != nil {
			if isClosedWS(err) {
				return
			}
			c.deliver(Event{Type: EventError, Data: map[string]any{"code": "read_error", "message": err.Error()}})
			return
		}

		raw, err := io.ReadAll(r)
		if err != nil {
			c.deliver(Event{Type: EventError, Data: map[string]any{"code": "read_error", "message": err.Error()}})
			return
		}

		env, err := aep.DecodeLine(raw)
		if err != nil {
			c.deliver(Event{Type: EventError, Data: map[string]any{"code": "decode_error", "message": err.Error()}})
			return
		}

		// Update local state on state events.
		if env.Event.Type == events.State {
			if d, ok := decodeAs[StateData](env.Event.Data); ok {
				c.mu.Lock()
				c.state = d.State
				c.mu.Unlock()
			}
		}

		c.deliver(Event{
			Type:    string(env.Event.Type),
			Seq:     env.Seq,
			Session: env.SessionID,
			Data:    env.Event.Data,
		})

		if aep.IsTerminalEvent(env.Event.Type) {
			return
		}
	}
}

func (c *Client) sendPump() {
	defer c.wg.Done()
	for frame := range c.sendCh {
		c.mu.Lock()
		conn := c.conn
		closed := c.closed
		c.mu.Unlock()
		if conn == nil || closed {
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
			c.logger.Debug("send pump: write failed", "err", err)
			return
		}
	}
}

func (c *Client) pingPump() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			if conn == nil {
				return
			}
			deadline := time.Now().Add(10 * time.Second)
			if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				return
			}
		}
	}
}

func (c *Client) deliver(evt Event) {
	// Critical events (done/error/state) must never be dropped — block until
	// delivered or the client is shutting down.
	if evt.Type == EventDone || evt.Type == EventError || evt.Type == EventState {
		select {
		case c.eventsCh <- evt:
		case <-c.ctx.Done():
		}
		return
	}
	// Non-critical events (delta, raw, etc.) are silently dropped under backpressure.
	select {
	case c.eventsCh <- evt:
	default:
		c.logger.Debug("events channel full, dropping event", "type", evt.Type)
	}
}

func parseInitAck(env *events.Envelope) *InitAckData {
	ack, _ := decodeAs[InitAckData](env.Event.Data)
	if ack.SessionID == "" {
		ack.SessionID = env.SessionID
	}
	if ack.State == "" {
		ack.State = StateCreated
	}
	return &ack
}

func isClosedWS(err error) bool {
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	)
}

func backoffDuration(attempt int) time.Duration {
	const base = 1 * time.Second
	const max = 30 * time.Second
	d := base * (1 << uint(attempt-1))
	if d > max {
		return max
	}
	return d
}
