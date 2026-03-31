package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"hotplex-worker/internal/aep"
	"hotplex-worker/internal/config"
	"hotplex-worker/internal/metrics"
	"hotplex-worker/internal/security"
	"hotplex-worker/internal/session"
	"hotplex-worker/internal/tracing"
	"hotplex-worker/internal/worker"
	"hotplex-worker/internal/worker/noop"
	"hotplex-worker/pkg/events"
)

// Conn represents a single WebSocket client connection.
type Conn struct {
	log *slog.Logger
	wc  *websocket.Conn
	hub *Hub

	sessionID string
	userID    string

	// Heartbeat.
	hb *heartbeat

	mu     sync.Mutex
	closed bool

	done chan struct{}
}

// newConn creates a new Conn.
func newConn(hub *Hub, wc *websocket.Conn, sessionID string) *Conn {
	return &Conn{
		log:       hub.log,
		wc:        wc,
		hub:       hub,
		sessionID: sessionID,
		hb:        newHeartbeat(hub.log),
		done:      make(chan struct{}),
	}
}

// RemoteAddr returns the remote address of the client.
func (c *Conn) RemoteAddr() string {
	if c.wc != nil {
		return c.wc.RemoteAddr().String()
	}
	return "?"
}

// ReadPump pumps messages from the WebSocket to the hub's broadcast channel.
// It also handles pong responses, missed pong detection, and the AEP init handshake.
func (c *Conn) ReadPump(handler *Handler) {
	defer func() {
		c.hb.Stop()
		c.Close()
		c.hub.LeaveSession(c.sessionID, c)
	}()

	c.wc.SetReadLimit(maxMessageSize)

	// Phase 1: AEP init handshake — read the first message.
	if err := c.performInit(handler); err != nil {
		c.log.Warn("gateway: init handshake failed", "err", err)
		return
	}

	// Phase 2: Normal message loop.
	for {
		// Set read deadline for pong detection.
		_ = c.wc.SetReadDeadline(time.Now().Add(pongWait))

		// Pong handler: record that remote responded.
		c.wc.SetPongHandler(func(ping string) error {
			c.hb.MarkAlive()
			_ = c.wc.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		_, data, err := c.wc.ReadMessage()
		if err != nil {
			// Detect missed pong (read deadline exceeded).
			if isReadTimeout(err) {
				metrics.GatewayErrorsTotal.WithLabelValues("pong_timeout").Inc()
				if c.hb.MarkMissed() {
					c.log.Warn("gateway: max missed pongs, disconnecting",
						"session_id", c.sessionID)
					return
				}
			}
			if !errors.Is(err, websocket.ErrCloseSent) {
				c.log.Debug("gateway: read error", "err", err)
			}
			metrics.GatewayErrorsTotal.WithLabelValues("read_error").Inc()
			return
		}

		// Reset missed counter on any successful read.
		c.hb.MarkAlive()

		env, err := aep.DecodeLine(data)
		if err != nil {
			c.sendError(events.ErrCodeInvalidMessage, err.Error())
			metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInvalidMessage)).Inc()
			continue
		}

		metrics.GatewayMessagesTotal.WithLabelValues("incoming", string(env.Event.Type)).Inc()

		// Stamp session ID, sequence number, and owner ID.
		env.SessionID = c.sessionID
		env.OwnerID = c.userID
		env.Seq = c.hub.NextSeq(c.sessionID)

		// Route to handler with tracing span.
		_, span := tracing.SpanFromContext(context.Background()).Start(context.Background(), "conn.recv")
		span.SetAttributes(
			attribute.String("session_id", c.sessionID),
			attribute.String("event_type", string(env.Event.Type)),
			attribute.Int64("seq", env.Seq),
		)
		if err := handler.Handle(context.Background(), env); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			c.log.Debug("gateway: handle error", "err", err, "session_id", c.sessionID)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// performInit reads and processes the AEP init handshake message.
// It blocks until either an init message is processed or an error occurs.
func (c *Conn) performInit(handler *Handler) error {
	_, span := tracing.SpanFromContext(context.Background()).Start(context.Background(), "conn.init")
	defer func() {
		if span != nil {
			span.End()
		}
	}()

	// Read first message with a longer deadline (init may take time on cold start).
	_ = c.wc.SetReadDeadline(time.Now().Add(30 * time.Second))

	_, data, err := c.wc.ReadMessage()
	if err != nil {
		return fmt.Errorf("read init: %w", err)
	}

	env, err := aep.DecodeLine(data)
	if err != nil {
		c.sendInitError(events.ErrCodeInvalidMessage, "malformed message: "+err.Error())
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInvalidMessage)).Inc()
		return err
	}

	// Only accept init message as first message.
	if env.Event.Type != Init {
		c.sendInitError(events.ErrCodeProtocolViolation, "expected init as first message, got "+string(env.Event.Type))
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeProtocolViolation)).Inc()
		return fmt.Errorf("expected init, got %s", env.Event.Type)
	}

	metrics.GatewayMessagesTotal.WithLabelValues("incoming", Init).Inc()

	// Validate init fields.
	initData, initErr := ValidateInit(env)
	if initErr != nil {
		c.sendInitError(initErr.Code, initErr.Message)
		metrics.GatewayErrorsTotal.WithLabelValues(string(initErr.Code)).Inc()
		return initErr
	}

	// Validate JWT token from init envelope (if provided and validator is configured).
	if initData.Auth.Token != "" && handler.jwtValidator != nil {
		claims, err := handler.jwtValidator.Validate(initData.Auth.Token)
		if err != nil {
			c.log.Warn("gateway: init JWT validation failed", "err", err)
			c.sendInitError(events.ErrCodeUnauthorized, "invalid token")
			metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeUnauthorized)).Inc()
			return fmt.Errorf("jwt validation: %w", err)
		}
		// Bind user_id from JWT subject claim (overrides HTTP auth userID for session ownership).
		if claims.Subject != "" {
			c.userID = claims.Subject
		}
	}

	// Determine session ID: prefer envelope's session_id, fall back to connection's.
	sessionID := initData.SessionID
	if sessionID == "" {
		sessionID = c.sessionID
	}

	// Resolve session: create new or resume existing.
	si, err := handler.sm.Get(sessionID)
	if err != nil {
		// Session does not exist → create new.
		if errors.Is(err, session.ErrSessionNotFound) {
			si, err = handler.sm.Create(context.Background(), sessionID, c.userID, initData.WorkerType, initData.Config.AllowedTools)
			if err != nil {
				c.sendInitError(events.ErrCodeInternalError, "failed to create session")
				metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
				return fmt.Errorf("create session: %w", err)
			}
			c.log.Info("gateway: session created via init", "session_id", sessionID,
				"worker_type", initData.WorkerType)
		} else {
			c.sendInitError(events.ErrCodeInternalError, err.Error())
			metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
			return fmt.Errorf("get session: %w", err)
		}
	} else if si.State == events.StateDeleted {
		// Deleted session → reject.
		c.sendInitError(events.ErrCodeSessionNotFound, "session was deleted")
		return ErrInitSessionDeleted
	} else if si.State == events.StateTerminated {
		// Terminated → attempt resume (restart worker).
		c.log.Info("gateway: resuming terminated session", "session_id", sessionID)
	}

	// Update connection's session ID if it changed.
	c.mu.Lock()
	c.sessionID = sessionID
	c.userID = si.UserID
	c.mu.Unlock()

	// Update hub's session subscription.
	c.hub.LeaveSession("", c)       // unsubscribe from old (empty) session
	c.hub.JoinSession(sessionID, c) // subscribe to new session

	// Send init_ack.
	ack := BuildInitAck(sessionID, si.State, initData.WorkerType)
	ack.Seq = c.hub.NextSeq(sessionID)
	if err := c.WriteCtx(context.Background(), ack); err != nil {
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
		return fmt.Errorf("send init_ack: %w", err)
	}
	metrics.GatewayMessagesTotal.WithLabelValues("outgoing", InitAck).Inc()

	// If session was CREATED, transition to RUNNING. (StateNotifier will broadcast the state change)
	if si.State == events.StateCreated {
		if err := handler.sm.Transition(context.Background(), sessionID, events.StateRunning); err != nil {
			c.log.Warn("gateway: transition to running", "session_id", sessionID, "err", err)
		}
	}

	c.log.Info("gateway: init complete", "session_id", sessionID,
		"worker_type", initData.WorkerType, "state", si.State)
	span.SetStatus(codes.Ok, "init complete")
	return nil
}

func (c *Conn) sendInitError(code events.ErrorCode, msg string) {
	ack := BuildInitAckError(c.sessionID, &InitError{Code: code, Message: msg})
	ack.Seq = c.hub.NextSeq(c.sessionID)
	_ = c.WriteCtx(context.Background(), ack)
}

// WritePump pumps periodic pings to the WebSocket.
// It also drains the hub's broadcast channel and writes to the client.
func (c *Conn) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-c.hb.Stopped():
			return
		case <-ticker.C:
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return
			}
			_ = c.wc.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.wc.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.mu.Unlock()
				c.log.Debug("gateway: ping failed", "err", err)
				return
			}
			c.mu.Unlock()
		}
	}
}

// WriteCtx writes an envelope to the connection using the provided context for deadline.
func (c *Conn) WriteCtx(ctx context.Context, env *events.Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return errors.New("conn closed")
	}

	data, err := aep.EncodeJSON(env)
	if err != nil {
		return err
	}

	_ = c.wc.SetWriteDeadline(time.Now().Add(writeWait))
	if err := c.wc.WriteMessage(websocket.TextMessage, data); err != nil {
		metrics.GatewayErrorsTotal.WithLabelValues("write_error").Inc()
		return err
	}
	metrics.GatewayMessagesTotal.WithLabelValues("outgoing", string(env.Event.Type)).Inc()
	return nil
}

// WriteMessage writes raw bytes to the connection.
func (c *Conn) WriteMessage(msgType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("conn closed")
	}
	_ = c.wc.SetWriteDeadline(time.Now().Add(writeWait))
	return c.wc.WriteMessage(msgType, data)
}

// Close closes the WebSocket connection.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.done)
	c.mu.Unlock()

	_ = c.wc.SetWriteDeadline(time.Now().Add(writeWait))
	_ = c.wc.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	return c.wc.Close()
}

func (c *Conn) sendError(code events.ErrorCode, msg string) {
	env := events.NewEnvelope(aep.NewID(), c.sessionID, c.hub.NextSeq(c.sessionID), events.Error, events.ErrorData{
		Code:    code,
		Message: msg,
	})
	_ = c.WriteCtx(context.Background(), env)
}

// ─── Message Handler ─────────────────────────────────────────────────────────

// Handler processes incoming messages from a client connection.
// It coordinates between the hub, session manager, and pool.
type Handler struct {
	log           *slog.Logger
	cfg           *config.Config
	hub           *Hub
	sm            *session.Manager
	jwtValidator  *security.JWTValidator
}

// NewHandler creates a new message handler.
func NewHandler(log *slog.Logger, cfg *config.Config, hub *Hub, sm *session.Manager, jwtValidator *security.JWTValidator) *Handler {
	return &Handler{
		log:          log,
		cfg:          cfg,
		hub:          hub,
		sm:           sm,
		jwtValidator: jwtValidator,
	}
}

// Handle processes an incoming envelope from a client.
func (h *Handler) Handle(ctx context.Context, env *events.Envelope) error {
	switch env.Event.Type {
	case events.Input:
		return h.handleInput(ctx, env)
	case events.Ping:
		return h.handlePing(ctx, env)
	case events.Control:
		return h.handleControl(ctx, env)
	default:
		return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "unknown event type: %s", env.Event.Type)
	}
}

func (h *Handler) handleInput(ctx context.Context, env *events.Envelope) error {
	data, ok := env.Event.Data.(map[string]any)
	if !ok {
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "malformed input data")
	}

	content, _ := data["content"].(string)

	// Check SESSION_BUSY: input and state transition must be atomic.
	si, err := h.sm.Get(env.SessionID)
	if err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
	}

	if !si.State.IsActive() {
		return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session not active: %s", si.State)
	}

	// Atomic transition + input.
	if err := h.sm.TransitionWithInput(ctx, env.SessionID, events.StateRunning, content, nil); err != nil {
		return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session busy: %v", err)
	}

	// Deliver to worker.
	w := h.sm.GetWorker(env.SessionID)
	if w != nil {
		if err := w.Input(ctx, content, nil); err != nil {
			h.log.Warn("gateway: worker input", "err", err, "session_id", env.SessionID)
		}
	}

	return nil
}

func (h *Handler) handlePing(ctx context.Context, env *events.Envelope) error {
	// Include current session state in pong (per AEP spec §11.4).
	si, err := h.sm.Get(env.SessionID)
	state := "unknown"
	if err == nil {
		state = string(si.State)
	}

	reply := events.NewEnvelope(
		aep.NewID(),
		env.SessionID,
		h.hub.NextSeq(env.SessionID),
		events.Pong,
		map[string]any{"state": state},
	)
	return h.hub.SendToSession(ctx, reply)
}

// handleControl processes client-originated control messages (terminate, delete).
// Server-originated control messages (reconnect, session_invalid, throttle) are
// sent via SendControlToSession.
func (h *Handler) handleControl(ctx context.Context, env *events.Envelope) error {
	data, ok := env.Event.Data.(map[string]any)
	if !ok {
		return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "control: invalid data")
	}

	action, _ := data["action"].(string)
	h.log.Info("gateway: control received", "action", action, "session_id", env.SessionID)

	switch events.ControlAction(action) {
	case events.ControlActionTerminate:
		// Ownership check: only the session owner can terminate.
		if err := h.sm.ValidateOwnership(ctx, env.SessionID, env.OwnerID, ""); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
		}
		// Transition to TERMINATED and kill the worker.
		if err := h.sm.TransitionWithReason(ctx, env.SessionID, events.StateTerminated, "client_kill"); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "terminate failed: %v", err)
		}
		// Send error + done to client.
		errEnv := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.Error, events.ErrorData{
			Code:    events.ErrCodeSessionTerminated,
			Message: "session terminated by client",
		})
		doneEnv := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.Done, events.DoneData{
			Success: false,
		})
		_ = h.hub.SendToSession(ctx, errEnv)
		_ = h.hub.SendToSession(ctx, doneEnv)
		return nil

	case events.ControlActionDelete:
		// Ownership check: only the session owner can delete.
		if err := h.sm.ValidateOwnership(ctx, env.SessionID, env.OwnerID, ""); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
		}
		// Delete the session (bypasses TERMINATED state per design §5).
		if err := h.sm.Delete(ctx, env.SessionID); err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
			}
			return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "delete failed: %v", err)
		}
		return nil

	default:
		return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "unknown control action: %s", action)
	}
}

// SendControlToSession sends a server-originated control message to the client.
// Used for reconnect, session_invalid, and throttle notifications.
func (h *Handler) SendControlToSession(ctx context.Context, sessionID string, action events.ControlAction, reason string, details map[string]any) error {
	env := events.NewEnvelope(aep.NewID(), sessionID, h.hub.NextSeq(sessionID), events.Control, events.ControlData{
		Action:  action,
		Reason:  reason,
		Details: details,
	})
	env.Priority = events.PriorityControl // control messages bypass backpressure
	return h.hub.SendToSession(ctx, env)
}

// SendReconnect sends a reconnect control message to the client.
func (h *Handler) SendReconnect(ctx context.Context, sessionID, reason string, delayMs int) error {
	return h.SendControlToSession(ctx, sessionID, events.ControlActionReconnect, reason, map[string]any{
		"delay_ms": delayMs,
	})
}

// SendSessionInvalid sends a session_invalid control message to the client.
func (h *Handler) SendSessionInvalid(ctx context.Context, sessionID, reason string, recoverable bool) error {
	return h.SendControlToSession(ctx, sessionID, events.ControlActionSessionInvalid, reason, map[string]any{
		"recoverable": recoverable,
	})
}

// SendThrottle sends a throttle control message to the client.
func (h *Handler) SendThrottle(ctx context.Context, sessionID string, backoffMs int, maxMessageRate int) error {
	return h.SendControlToSession(ctx, sessionID, events.ControlActionThrottle, "rate limit exceeded", map[string]any{
		"suggestion": map[string]any{
			"max_message_rate": maxMessageRate,
		},
		"backoff_ms":  backoffMs,
		"retry_after": backoffMs,
	})
}

func (h *Handler) sendErrorf(ctx context.Context, env *events.Envelope, code events.ErrorCode, format string, args ...any) error {
	err := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.Error, events.ErrorData{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	})
	return h.hub.SendToSession(ctx, err)
}

// ─── Bridge ─────────────────────────────────────────────────────────────────

// Bridge connects the gateway to the session manager.
// It runs the read pump in a goroutine and proxies worker events to the hub.
type Bridge struct {
	log      *slog.Logger
	hub      *Hub
	sm       *session.Manager
	msgStore session.MessageStore // EVT-004: optional; nil means event persistence disabled
}

// NewBridge creates a new bridge. msgStore may be nil (event persistence disabled).
func NewBridge(log *slog.Logger, hub *Hub, sm *session.Manager, msgStore session.MessageStore) *Bridge {
	return &Bridge{
		log:      log,
		hub:      hub,
		sm:       sm,
		msgStore: msgStore,
	}
}

// StartSession creates a new session and starts a worker.
func (b *Bridge) StartSession(ctx context.Context, id, userID string, wt worker.WorkerType) error {
	// Create session in DB.
	si, err := b.sm.Create(ctx, id, userID, wt, nil) // AllowedTools set via Update later if needed
	if err != nil {
		return fmt.Errorf("bridge: create session: %w", err)
	}
	_ = si

	// Create worker.
	w, err := worker.NewWorker(wt)
	if err != nil {
		return fmt.Errorf("bridge: create worker: %w", err)
	}

	// Attach worker.
	if err := b.sm.AttachWorker(id, w); err != nil {
		_ = b.sm.Delete(ctx, id)
		return fmt.Errorf("bridge: attach worker: %w", err)
	}

	// Start worker.
	workerInfo := worker.SessionInfo{
		SessionID:    id,
		UserID:       userID,
		ProjectDir:   "",
		Env:          nil,
		Args:         nil,
		AllowedTools: si.AllowedTools,
	}
	if err := w.Start(ctx, workerInfo); err != nil {
		b.sm.DetachWorker(id)
		_ = b.sm.Delete(ctx, id)
		return fmt.Errorf("bridge: start worker: %w", err)
	}

	// Transition to RUNNING. (StateNotifier will emit state event automatically)
	if err := b.sm.Transition(ctx, id, events.StateRunning); err != nil {
		b.log.Warn("bridge: transition to running failed", "id", id, "err", err)
	}

	// Forward worker events to hub. Goroutine exits when conn.Recv() is closed
	// (happens when the worker is killed via poolMgr.Close).
	go b.forwardEvents(w.Conn(), id)

	return nil
}

// ResumeSession reattaches to an existing session.
func (b *Bridge) ResumeSession(ctx context.Context, id string) error {
	si, err := b.sm.Get(id)
	if err != nil {
		return err
	}

	if si.State == events.StateDeleted {
		return session.ErrSessionNotFound
	}

	// Create worker.
	w, err := worker.NewWorker(si.WorkerType)
	if err != nil {
		return fmt.Errorf("bridge: create worker: %w", err)
	}
	if noopw, ok := w.(*noop.Worker); ok {
		conn := noop.NewConn(id, si.UserID)
		noopw.SetConn(conn)
	}
	// Attach worker with quota.
	if err := b.sm.AttachWorker(id, w); err != nil {
		return fmt.Errorf("bridge: attach worker: %w", err)
	}

	// Start worker.
	workerInfo := worker.SessionInfo{
		SessionID:    si.ID,
		UserID:       si.UserID,
		AllowedTools: si.AllowedTools,
	}
	if err := w.Resume(ctx, workerInfo); err != nil {
		b.sm.DetachWorker(id)
		return fmt.Errorf("bridge: resume start: %w", err)
	}

	if si.State == events.StateTerminated {
		if err := b.sm.Transition(ctx, id, events.StateRunning); err != nil {
			return err
		}
	}

	// Notify client of current state.
	stateToNotify := si.State
	if stateToNotify == events.StateTerminated {
		stateToNotify = events.StateRunning // We just transitioned it
	}
	stateEvt := events.NewEnvelope(aep.NewID(), id, b.hub.NextSeq(id), events.State, events.StateData{
		State: stateToNotify,
	})
	return b.hub.SendToSession(ctx, stateEvt)
}

// forwardEvents proxies worker events to the hub with seq assignment.
// EVT-004: if msgStore is configured, it appends to the event log on done events.
func (b *Bridge) forwardEvents(conn worker.SessionConn, sessionID string) {
	for env := range conn.Recv() {
		env.SessionID = sessionID
		// Seq is assigned by hub.SendToSession via SeqGen (seq=0 triggers auto-assignment).

		// UI Reconciliation (Fallback full message if silent dropped)
		if env.Event.Type == events.Done {
			if b.hub.GetAndClearDropped(sessionID) {
				b.log.Warn("gateway: handling dropped deltas before done", "session_id", sessionID)

				// Optional: Here we could inject a raw `message` pulling full state from Worker.
				// For now, we mutate the `done` event to pass the `dropped: true` flag inside `stats`.
				if dataMap, ok := env.Event.Data.(map[string]any); ok {
					if stats, ok := dataMap["stats"].(map[string]any); ok {
						stats["dropped"] = true
					} else {
						dataMap["stats"] = map[string]any{"dropped": true}
					}
					// Update with custom DoneData if needed
				} else if doneData, ok := env.Event.Data.(events.DoneData); ok {
					doneData.Dropped = true
					env.Event.Data = doneData
				} else if doneDataPtr, ok := env.Event.Data.(*events.DoneData); ok {
					doneDataPtr.Dropped = true
					env.Event.Data = doneDataPtr
				}
			}
		}

		if err := b.hub.SendToSession(context.Background(), env); err != nil {
			b.log.Warn("bridge: forward event failed", "err", err, "session_id", sessionID)
		}

		// EVT-004: append to MessageStore on done events (end of each turn).
		// The Append call is async and non-blocking; failures are logged but do not
		// affect the event stream.
		if b.msgStore != nil && env.Event.Type == events.Done {
			payload, _ := aep.EncodeJSON(env)
			if err := b.msgStore.Append(context.Background(), env.SessionID, env.Seq, string(env.Event.Type), payload); err != nil {
				b.log.Warn("bridge: msgstore append", "err", err, "session_id", sessionID)
			}
		}
	}
}
