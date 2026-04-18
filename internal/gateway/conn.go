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

	"github.com/hotplex/hotplex-worker/internal/metrics"
	"github.com/hotplex/hotplex-worker/internal/security"
	"github.com/hotplex/hotplex-worker/internal/session"
	"github.com/hotplex/hotplex-worker/internal/tracing"
	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// SessionStarter initiates a worker session. It is the only Bridge capability
// used by Conn (called once during the AEP init handshake).
type SessionStarter interface {
	StartSession(ctx context.Context, id, userID, botID string,
		wt worker.WorkerType, allowedTools []string, workDir string) error
	ResumeSession(ctx context.Context, id string, workDir string) error
}

var _ SessionStarter = (*Bridge)(nil) // compile-time: Bridge implements SessionStarter

// Conn represents a single WebSocket client connection.
type Conn struct {
	log *slog.Logger
	wc  *websocket.Conn
	hub *Hub

	sessionID string
	userID    string
	botID     string // SEC-007: bot isolation tag from JWT

	// starter handles session creation and worker lifecycle (nil = no-op, test mode).
	starter SessionStarter

	// Heartbeat.
	hb *heartbeat

	mu     sync.Mutex
	closed bool

	done chan struct{}
}

// newConn creates a new Conn.
func newConn(hub *Hub, wc *websocket.Conn, sessionID string, starter SessionStarter) *Conn {
	log := slog.Default()
	if hub != nil {
		log = hub.log
	}
	return &Conn{
		log:       log,
		wc:        wc,
		hub:       hub,
		starter:   starter,
		sessionID: sessionID,
		hb:        newHeartbeat(log),
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

		// Transition to IDLE BEFORE unregistering so the state(idle) event
		// can be routed through Hub.Run while the conn is still in h.sessions.
		// If we unregister first, routeMessage finds no connections and the
		// state event is silently dropped.
		if c.sessionID != "" {
			if err := handler.sm.Transition(context.Background(), c.sessionID, events.StateIdle); err != nil {
				c.log.Debug("gateway: conn close transition to idle", "session_id", c.sessionID, "err", err)
			}
		}

		// Now safe to remove from routing — state event already queued.
		c.hub.UnregisterConn(c)

		_ = c.Close()
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
		// P2: ping/pong are heartbeat control messages — don't consume seq.
		if env.Event.Type != events.Ping {
			env.Seq = c.hub.NextSeq(c.sessionID)
		}

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
	if env.Event.Type != events.Init {
		c.sendInitError(events.ErrCodeProtocolViolation, "expected init as first message, got "+string(env.Event.Type))
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeProtocolViolation)).Inc()
		return fmt.Errorf("expected init, got %s", env.Event.Type)
	}

	metrics.GatewayMessagesTotal.WithLabelValues("incoming", string(events.Init)).Inc()

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
		// SEC-007: bind bot_id for multi-bot isolation.
		if claims.BotID != "" {
			c.botID = claims.BotID
		}
	}

	// Resolve work dir: use client-provided value or default from config.
	workDir := initData.Config.WorkDir
	if workDir == "" {
		workDir = handler.cfg.Worker.DefaultWorkDir
	}
	if err := security.ValidateWorkDir(workDir); err != nil {
		c.sendInitError(events.ErrCodeInvalidMessage, err.Error())
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInvalidMessage)).Inc()
		return err
	}

	// Determine session ID via deterministic UUIDv5 mapping from client session ID.
	// DeriveSessionKey(ownerID, workerType, clientSessionID, workDir) is always deterministic
	// for the same (ownerID, workerType, clientSessionID, workDir) tuple.
	sessionID := session.DeriveSessionKey(c.userID, initData.WorkerType, initData.SessionID, workDir)

	// Resolve session: create new or resume existing.
	si, err := handler.sm.Get(sessionID)
	if err != nil {
		// Session does not exist → create and start via SessionStarter.
		if errors.Is(err, session.ErrSessionNotFound) {
			// starter.StartSession creates the DB record, worker, transitions to RUNNING,
			// and starts forwarding events. nil starter means test mode.
			if c.starter != nil {
				if err := c.starter.StartSession(context.Background(), sessionID, c.userID, c.botID, initData.WorkerType, initData.Config.AllowedTools, workDir); err != nil {
					c.sendInitError(events.ErrCodeInternalError, "failed to create session")
					metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
					return fmt.Errorf("create session: %w", err)
				}
				// Fetch the session info that StartSession created.
				si, err = handler.sm.Get(sessionID)
				if err != nil {
					c.sendInitError(events.ErrCodeInternalError, "session not found after creation")
					metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
					return fmt.Errorf("get session after start: %w", err)
				}
				c.log.Info("gateway: session created via init", "session_id", sessionID,
					"worker_type", initData.WorkerType)
			} else {
				// Test mode — create session without starting a worker.
				si, err = handler.sm.CreateWithBot(context.Background(), sessionID, c.userID, c.botID, initData.WorkerType, initData.Config.AllowedTools)
				if err != nil {
					c.sendInitError(events.ErrCodeInternalError, "failed to create session")
					metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
					return fmt.Errorf("create session: %w", err)
				}
				c.log.Info("gateway: session created via init (test mode)", "session_id", sessionID,
					"worker_type", initData.WorkerType)
			}
		} else {
			c.sendInitError(events.ErrCodeInternalError, err.Error())
			metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
			return fmt.Errorf("get session: %w", err)
		}
	} else if si.State == events.StateDeleted {
		// Deleted session → reject.
		c.sendInitError(events.ErrCodeSessionNotFound, "session was deleted")
		return ErrInitSessionDeleted
	} else if si.State == events.StateIdle || si.State == events.StateTerminated {
		// Idle/Terminated session → resume worker (reattach to existing session/worker).
		c.log.Info("gateway: resuming session", "session_id", sessionID, "from_state", si.State)
		// ResumeSession requires a valid SessionStarter (Bridge). In test mode,
		// starter may be nil; skip resumption and let bot_id validation proceed.
		if c.starter != nil {
			if err := c.starter.ResumeSession(context.Background(), sessionID, workDir); err != nil {
				c.sendInitError(events.ErrCodeInternalError, "failed to resume session")
				return fmt.Errorf("resume session: %w", err)
			}
		}
	}

	// SEC-007: reject cross-bot access — bot_id from JWT must match session's bot_id.
	if c.botID != "" && si.BotID != "" && c.botID != si.BotID {
		c.sendInitError(events.ErrCodeUnauthorized, "bot_id mismatch")
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeUnauthorized)).Inc()
		return fmt.Errorf("bot_id mismatch: connection=%s session=%s", c.botID, si.BotID)
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

	// NOTE: Session remains in CREATED state until handleInput transitions it to RUNNING.
	// Do NOT transition here — handleInput (→ TransitionWithInput) is the sole entry point
	// for the CREATED → RUNNING transition and is atomic with input delivery.

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
