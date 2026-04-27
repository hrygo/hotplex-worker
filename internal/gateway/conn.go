package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/tracing"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// platformWebChat is the platform tag for WebSocket webchat sessions.
const platformWebChat = "webchat"

// SessionStarter initiates a worker session. It is the only Bridge capability
// used by Conn (called once during the AEP init handshake).
type SessionStarter interface {
	StartSession(ctx context.Context, id, userID, botID string,
		wt worker.WorkerType, allowedTools []string, workDir string, platform string, platformKey map[string]string, title string) error
	ResumeSession(ctx context.Context, id string, workDir string) error
	SwitchWorkDir(ctx context.Context, oldSessionID, newWorkDir string) (*SwitchWorkDirResult, error)
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

	// Init-phase buffering: during the AEP init handshake, events from
	// Hub.routeMessage (state transitions during StartSession/ResumeSession)
	// are buffered here. This ensures init_ack is always the first
	// application-level message the client receives. Flushed by markInitDone.
	initDone    bool
	initPending [][]byte

	done chan struct{}
}

// newConn creates a new Conn.
func newConn(hub *Hub, wc *websocket.Conn, sessionID string, starter SessionStarter) *Conn {
	log := slog.Default()
	if hub != nil {
		log = hub.log
	}
	return &Conn{
		log:       log.With("component", "conn"),
		wc:        wc,
		hub:       hub,
		starter:   starter,
		sessionID: sessionID,
		hb:        newHeartbeat(log),
		initDone:  true, // true by default; performInit sets false during handshake
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
		if r := recover(); r != nil {
			c.log.Error("gateway: panic in ReadPump", "session_id", c.sessionID, "panic", r, "stack", string(debug.Stack()))
		}
		c.markInitDone() // flush buffered events or release on init failure
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
		c.log.Warn("gateway: init handshake failed", "session_id", c.sessionID, "err", err)
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
				c.log.Debug("gateway: read error", "session_id", c.sessionID, "err", err)
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

		// Route to handler — skip tracing span for high-frequency pings.
		if env.Event.Type == events.Ping {
			if err := handler.Handle(context.Background(), env); err != nil {
				c.log.Debug("gateway: handle ping error", "err", err, "session_id", c.sessionID)
			}
			continue
		}

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
		workDir = c.hub.cfgStore.Load().Worker.DefaultWorkDir
	}
	if err := security.ValidateWorkDir(workDir); err != nil {
		c.sendInitError(events.ErrCodeInvalidMessage, err.Error())
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInvalidMessage)).Inc()
		return err
	}

	// Resolve session ID: clients put session_id at the envelope level
	// (env.SessionID), not inside event.data. If the envelope session_id
	// already exists in the DB (e.g., a derived UUID from REST API
	// CreateSession), use it directly. Otherwise derive via UUIDv5.
	var sessionID string
	var preResolved *session.SessionInfo
	if env.SessionID != "" {
		if existing, getErr := handler.sm.Get(env.SessionID); getErr == nil && existing != nil && existing.State != events.StateDeleted {
			sessionID = env.SessionID
			preResolved = existing
		}
	}
	if sessionID == "" {
		sessionID = session.DeriveSessionKey(c.userID, initData.WorkerType, env.SessionID, workDir)
	}

	// Check throttler before doing heavy work.
	if !c.hub.InitThrottle.Check(sessionID) {
		c.sendInitError(events.ErrCodeRateLimited, "too many failed attempts, please back off")
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeRateLimited)).Inc()
		return fmt.Errorf("init throttled for session %s", sessionID)
	}

	// Enable init-phase buffering.
	c.mu.Lock()
	c.initDone = false
	c.mu.Unlock()

	// Subscribe to session BEFORE creation/resume.
	c.hub.LeaveSession("", c)
	c.hub.JoinSession(sessionID, c)

	// Resolve session: create new or resume existing.
	// Reuse preResolved when it's for the same session ID.
	var si *session.SessionInfo
	if preResolved != nil {
		si = preResolved
	} else {
		si, err = handler.sm.Get(sessionID)
	}
	if err != nil {
		// Session does not exist → create and start via SessionStarter.
		if errors.Is(err, session.ErrSessionNotFound) {
			if c.starter != nil {
				if err := c.starter.StartSession(context.Background(), sessionID, c.userID, c.botID, initData.WorkerType, initData.Config.AllowedTools, workDir, platformWebChat, nil, ""); err != nil {
					c.hub.InitThrottle.RecordFailure(sessionID)
					c.sendInitError(events.ErrCodeInternalError, "failed to create session")
					metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
					return fmt.Errorf("create session: %w", err)
				}
				si, err = handler.sm.Get(sessionID)
				if err != nil {
					c.hub.InitThrottle.RecordFailure(sessionID)
					c.sendInitError(events.ErrCodeInternalError, "session not found after creation")
					metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
					return fmt.Errorf("get session after start: %w", err)
				}
			} else {
				// Test mode
				si, err = handler.sm.CreateWithBot(context.Background(), sessionID, c.userID, c.botID, initData.WorkerType, initData.Config.AllowedTools, platformWebChat, nil, workDir, "")
				if err != nil {
					c.hub.InitThrottle.RecordFailure(sessionID)
					c.sendInitError(events.ErrCodeInternalError, "failed to create session")
					return fmt.Errorf("create session: %w", err)
				}
			}
		} else {
			c.hub.InitThrottle.RecordFailure(sessionID)
			c.sendInitError(events.ErrCodeInternalError, err.Error())
			return fmt.Errorf("get session: %w", err)
		}
	} else if si.State == events.StateCreated {
		if c.starter != nil {
			if err := c.starter.StartSession(context.Background(), sessionID, c.userID, c.botID, initData.WorkerType, initData.Config.AllowedTools, workDir, platformWebChat, nil, ""); err != nil {
				c.hub.InitThrottle.RecordFailure(sessionID)
				c.sendInitError(events.ErrCodeInternalError, "failed to start session")
				return fmt.Errorf("start unstarted session: %w", err)
			}
			si, err = handler.sm.Get(sessionID)
			if err != nil {
				c.sendInitError(events.ErrCodeInternalError, "session lost after creation")
				return fmt.Errorf("get session after start: %w", err)
			}
		}
	} else if si.State == events.StateDeleted {
		// Deleted sessions cannot be resumed. Physically remove then start fresh.
		_ = handler.sm.DeletePhysical(context.Background(), sessionID)
		if c.starter != nil {
			if err := c.starter.StartSession(context.Background(), sessionID, c.userID, c.botID,
				initData.WorkerType, initData.Config.AllowedTools, workDir, platformWebChat, nil, ""); err != nil {
				c.hub.InitThrottle.RecordFailure(sessionID)
				msg := fmt.Sprintf("failed to recreate deleted session: %v", err)
				c.sendInitError(events.ErrCodeInternalError, msg)
				metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
				return fmt.Errorf("recreate deleted session: %w", err)
			}
			si, err = handler.sm.Get(sessionID)
			if err != nil {
				c.sendInitError(events.ErrCodeInternalError, "session lost after recreation")
				return fmt.Errorf("get session after recreation: %w", err)
			}
		}
	} else if w := handler.sm.GetWorker(sessionID); w != nil {
		// Fast reconnect: worker still alive, skip terminate+resume cycle.
		if si.State != events.StateRunning {
			if err := handler.sm.Transition(context.Background(), sessionID, events.StateRunning); err != nil {
				c.log.Warn("gateway: fast reconnect transition failed", "session_id", sessionID, "from", si.State, "err", err)
			} else {
				si.State = events.StateRunning
			}
		}
	} else if si.State == events.StateIdle || si.State == events.StateTerminated ||
		(si.State == events.StateRunning) {
		if c.starter != nil {
			resumeErr := c.starter.ResumeSession(context.Background(), sessionID, workDir)
			if resumeErr != nil {
				if err := c.starter.StartSession(context.Background(), sessionID, c.userID, c.botID,
					initData.WorkerType, initData.Config.AllowedTools, workDir, platformWebChat, nil, ""); err != nil {
					c.hub.InitThrottle.RecordFailure(sessionID)
					msg := fmt.Sprintf("resume failed (%v), then start also failed (%v)", resumeErr, err)
					c.sendInitError(events.ErrCodeInternalError, msg)
					metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
					return fmt.Errorf("start session after resume fallback: %w", err)
				}
			}
			si, err = handler.sm.Get(sessionID)
			if err != nil {
				c.sendInitError(events.ErrCodeInternalError, "session lost after resume")
				return fmt.Errorf("get session after resume: %w", err)
			}
		}
	}

	// SEC-007: reject cross-bot access.
	if c.botID != "" && si.BotID != "" && c.botID != si.BotID {
		c.hub.InitThrottle.RecordFailure(sessionID)
		c.sendInitError(events.ErrCodeUnauthorized, "bot_id mismatch")
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeUnauthorized)).Inc()
		return fmt.Errorf("bot_id mismatch: connection=%s session=%s", c.botID, si.BotID)
	}

	// Success!
	c.hub.InitThrottle.RecordSuccess(sessionID)

	// Update connection's session ID.
	c.mu.Lock()
	c.sessionID = sessionID
	c.userID = si.UserID
	c.mu.Unlock()

	// Send init_ack.
	ack := BuildInitAck(sessionID, si.State, initData.WorkerType)
	ack.Seq = c.hub.NextSeq(sessionID)
	if err := c.WriteCtx(context.Background(), ack); err != nil {
		metrics.GatewayErrorsTotal.WithLabelValues(string(events.ErrCodeInternalError)).Inc()
		return fmt.Errorf("send init_ack: %w", err)
	}
	metrics.GatewayMessagesTotal.WithLabelValues("outgoing", InitAck).Inc()

	c.markInitDone()

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
				c.log.Debug("gateway: ping failed", "session_id", c.sessionID, "err", err)
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
// During the AEP init handshake, events are buffered instead of written
// to ensure init_ack is always the first message the client receives.
func (c *Conn) WriteMessage(msgType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("conn closed")
	}
	if !c.initDone {
		buf := make([]byte, len(data))
		copy(buf, data)
		c.initPending = append(c.initPending, buf)
		return nil
	}
	_ = c.wc.SetWriteDeadline(time.Now().Add(writeWait))
	return c.wc.WriteMessage(msgType, data)
}

// markInitDone signals that the init handshake is complete and flushes
// any events buffered by WriteMessage during init. After this call,
// WriteMessage writes events to the WebSocket immediately.
func (c *Conn) markInitDone() {
	c.mu.Lock()
	c.initDone = true
	for _, data := range c.initPending {
		if c.closed {
			break
		}
		_ = c.wc.SetWriteDeadline(time.Now().Add(writeWait))
		if err := c.wc.WriteMessage(websocket.TextMessage, data); err != nil {
			break
		}
	}
	c.initPending = nil
	c.mu.Unlock()
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
