// Package gateway implements the WebSocket gateway that speaks AEP v1 to clients.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/codes"

	"hotplex-worker/internal/aep"
	"hotplex-worker/internal/config"
	"hotplex-worker/internal/metrics"
	"hotplex-worker/internal/security"
	"hotplex-worker/internal/session"
	"hotplex-worker/internal/tracing"
	"hotplex-worker/pkg/events"
)

// isReadTimeout reports whether err is a read deadline exceeded error.
func isReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrDeadlineExceeded)
}

// broadcastQueueSize returns the broadcast channel buffer size from config.
// A value of 0 means unbounded (not recommended for production).
func broadcastQueueSize(cfg *config.Config) int {
	if cfg.Gateway.BroadcastQueueSize <= 0 {
		return 256 // default
	}
	return cfg.Gateway.BroadcastQueueSize
}

// isDroppable reports whether an event kind can be dropped under backpressure.
func isDroppable(kind events.Kind) bool {
	return kind == events.MessageDelta || kind == events.Raw
}

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 32 * 1024
)

// Hub is the central message router and connection registry.
// All WebSocket connections and session→connection mappings are managed here.
type Hub struct {
	log *slog.Logger
	cfg *config.Config

	upgrader websocket.Upgrader

	mu       sync.RWMutex
	conns    map[*Conn]struct{}        // all active connections
	sessions map[string]map[*Conn]bool // sessionID → connections

	// Incoming messages from all connections.
	broadcast chan *EnvelopeWithConn

	// Sequence generation per session
	seqGen *SeqGen
	// Backpressure drop tracking per session
	sessionDropped map[string]bool

	// Shutdown signals.
	ctx    context.Context
	cancel context.CancelFunc

	// LogHandler is an optional callback invoked by routeMessage for each forwarded event.
	// Use it to capture events into an external ring buffer (e.g. /admin/logs).
	// If nil, no events are captured.
	LogHandler func(level, msg, sessionID string)
}

// EnvelopeWithConn pairs a message with its originating connection.
type EnvelopeWithConn struct {
	Env  *events.Envelope
	Conn *Conn
}

// NewHub creates a new Hub.
func NewHub(log *slog.Logger, cfg *config.Config) *Hub {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		log: log,
		cfg: cfg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  cfg.Gateway.ReadBufferSize,
			WriteBufferSize: cfg.Gateway.WriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				for _, allowed := range cfg.Security.AllowedOrigins {
					if allowed == "*" || allowed == origin {
						return true
					}
				}
				return false
			},
		},
		conns:          make(map[*Conn]struct{}),
		sessions:       make(map[string]map[*Conn]bool),
		seqGen:         NewSeqGen(),
		sessionDropped: make(map[string]bool),
		broadcast:      make(chan *EnvelopeWithConn, broadcastQueueSize(cfg)),
		ctx:            ctx,
		cancel:         cancel,
	}
}

// RegisterConn registers a new WebSocket connection.
func (h *Hub) RegisterConn(conn *Conn) {
	h.mu.Lock()
	h.conns[conn] = struct{}{}
	h.mu.Unlock()
	metrics.GatewayConnectionsOpen.Inc()
	h.log.Debug("gateway: conn registered", "remote", conn.RemoteAddr())
}

// UnregisterConn removes a connection and cleans up session mappings.
func (h *Hub) UnregisterConn(conn *Conn) {
	h.mu.Lock()
	delete(h.conns, conn)
	// Remove from all session maps.
	for sid, conns := range h.sessions {
		delete(conns, conn)
		if len(conns) == 0 {
			delete(h.sessions, sid)
		}
	}
	h.mu.Unlock()
	metrics.GatewayConnectionsOpen.Dec()
	h.log.Debug("gateway: conn unregistered", "remote", conn.RemoteAddr())
}

// JoinSession subscribes conn to receive events for a session.
// If the session already has another connection, the old one is disconnected
// (per the "按 session_id 去重连接，只保留最新连接" rule).
func (h *Hub) JoinSession(sessionID string, conn *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Kick out existing connections for this session.
	if existing, ok := h.sessions[sessionID]; ok {
		for c := range existing {
			if c != conn {
				h.log.Info("gateway: disconnecting stale connection", "session_id", sessionID)
				c.Close()
			}
		}
	}

	if h.sessions[sessionID] == nil {
		h.sessions[sessionID] = make(map[*Conn]bool)
	}
	h.sessions[sessionID][conn] = true
}

// LeaveSession unsubscribes conn from a session.
func (h *Hub) LeaveSession(sessionID string, conn *Conn) {
	h.mu.Lock()
	if conns, ok := h.sessions[sessionID]; ok {
		delete(conns, conn)
		if len(conns) == 0 {
			delete(h.sessions, sessionID)
		}
	}
	h.mu.Unlock()
}

// SendToSession delivers a message to all connections subscribed to a session.
// Control-priority messages bypass the broadcast queue.
func (h *Hub) SendToSession(ctx context.Context, env *events.Envelope) error {
	spanCtx, span := tracing.SpanFromContext(ctx).Start(ctx, "hub.send_to_session")
	defer span.End()
	span.SetAttributes(
		tracing.Attr("session_id", env.SessionID),
		tracing.Attr("event_type", string(env.Event.Type)),
		tracing.Attr("priority", string(env.Priority)),
	)

	// Assign sequence number before sending to broadcast queue or clients.
	// We skip assignment if seq is already set (eg. by Handler for direct replies).
	if env.Seq == 0 {
		env.Seq = h.seqGen.Next(env.SessionID)
	}

	if env.Priority == events.PriorityControl {
		return h.sendControlToSession(spanCtx, env)
	}

	// Backpressure strategy (per AEP spec §11.5):
	// - Droppable events (message.delta): try non-blocking send; drop silently if full.
	// - Guaranteed events (message/done/error/state): try non-blocking send; return error if full.
	if isDroppable(env.Event.Type) {
		select {
		case h.broadcast <- &EnvelopeWithConn{Env: env}:
			return nil
		default:
			// Silently drop delta — seq is NOT incremented for dropped events,
			// so client will not see seq gaps from intentional drops.
			h.mu.Lock()
			h.sessionDropped[env.SessionID] = true
			h.mu.Unlock()
			metrics.GatewayDeltasDropped.Inc()
			h.log.Debug("gateway: dropped delta (backpressure)", "session_id", env.SessionID)
			return nil
		}
	}

	// Guaranteed delivery path.
	select {
	case h.broadcast <- &EnvelopeWithConn{Env: env}:
		return nil
	default:
		err := errors.New("gateway: broadcast queue full")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
}

func (h *Hub) sendControlToSession(ctx context.Context, env *events.Envelope) error {
	h.mu.RLock()
	conns := h.sessions[env.SessionID]
	h.mu.RUnlock()

	if len(conns) == 0 {
		return nil
	}

	for conn := range conns {
		if err := conn.WriteCtx(ctx, env); err != nil {
			h.log.Warn("gateway: send to conn failed", "err", err, "conn", conn.RemoteAddr())
		}
	}
	return nil
}

// HandleHTTP serves WebSocket upgrade requests at the gateway endpoint.
// It authenticates the request, upgrades to WebSocket, and starts read/write pumps.
func (h *Hub) HandleHTTP(
	auth *security.Authenticator,
	sm *session.Manager,
	handler *Handler,
	bridge *Bridge,
) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Authenticate at HTTP upgrade time.
		userID, err := auth.AuthenticateRequest(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			sessionID = aep.NewSessionID()
		}

		wc, err := h.upgrader.Upgrade(w, r, nil)
		if err != nil {
			h.log.Error("gateway: upgrade failed", "err", err)
			return
		}

		c := newConn(h, wc, sessionID)
		c.userID = userID
		h.RegisterConn(c)
		h.JoinSession(sessionID, c)

		// Give the handler access to sm for the init handshake.
		handler.sm = sm

		// Start read and write pumps in background.
		go c.ReadPump(handler)
		go c.WritePump()

		h.log.Info("gateway: WS connected", "session_id", sessionID, "user_id", userID)
	})
}

// Run starts the hub's run loop. It blocks until the context is cancelled.
func (h *Hub) Run() {
	h.log.Info("gateway: hub running")

	for {
		select {
		case <-h.ctx.Done():
			h.drainBroadcast()
			return
		case msg := <-h.broadcast:
			_, span := tracing.SpanFromContext(h.ctx).Start(h.ctx, "hub.broadcast")
			span.SetAttributes(
				tracing.Attr("session_id", msg.Env.SessionID),
				tracing.Attr("event_type", string(msg.Env.Event.Type)),
				tracing.Attr("seq", msg.Env.Seq),
			)
			h.routeMessage(msg)
			span.End()
		}
	}
}

func (h *Hub) routeMessage(msg *EnvelopeWithConn) {
	h.mu.RLock()
	conns := h.sessions[msg.Env.SessionID]
	h.mu.RUnlock()

	if len(conns) == 0 {
		return
	}

	// ADMIN-008: capture event to ring buffer if LogHandler is configured.
	if h.LogHandler != nil {
		level := "INFO"
		if msg.Env.Event.Type == events.Error {
			level = "ERROR"
		} else if msg.Env.Event.Type == events.State {
			level = "WARN"
		}
		h.LogHandler(level, fmt.Sprintf("event %s seq=%d", msg.Env.Event.Type, msg.Env.Seq), msg.Env.SessionID)
	}

	encoded, err := aep.EncodeJSON(msg.Env)
	if err != nil {
		h.log.Error("gateway: encode failed", "err", err)
		return
	}

	for conn := range conns {
		metrics.GatewayMessagesTotal.WithLabelValues("outgoing", string(msg.Env.Event.Type)).Inc()
		if err := conn.WriteMessage(websocket.TextMessage, encoded); err != nil {
			h.log.Warn("gateway: write failed", "err", err)
			conn.Close()
		}
	}
}

func (h *Hub) drainBroadcast() {
	for msg := range h.broadcast {
		h.routeMessage(msg)
	}
}

// NextSeq returns the next sequence number for a session from the central generator.
func (h *Hub) NextSeq(sessionID string) int64 {
	return h.seqGen.Next(sessionID)
}

// NextSeqPeek returns the current sequence number for a session without incrementing.
func (h *Hub) NextSeqPeek(sessionID string) int64 {
	return h.seqGen.Peek(sessionID)
}

// ConnectionsOpen returns the number of currently open WebSocket connections.
func (h *Hub) ConnectionsOpen() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}

// GetAndClearDropped returns true if the session experienced any message.delta drops
// since the last time this method was called, and clears the dropped flag.
func (h *Hub) GetAndClearDropped(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	dropped := h.sessionDropped[sessionID]
	if dropped {
		delete(h.sessionDropped, sessionID)
	}
	return dropped
}

// Shutdown gracefully shuts down all connections and stops the hub.
// It drains in-flight messages from the broadcast queue, then closes
// all WebSocket connections. The ctx deadline controls the maximum wait time.
func (h *Hub) Shutdown(ctx context.Context) error {
	h.cancel()

	// Drain in-flight messages with a deadline.
	drainDone := make(chan struct{})
	go func() {
		h.drainBroadcast()
		close(drainDone)
	}()
	select {
	case <-drainDone:
	case <-ctx.Done():
		h.log.Warn("gateway: broadcast drain timed out")
	}

	// Close all connections.
	h.mu.RLock()
	conns := make([]*Conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	var errs []error
	for _, c := range conns {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// SeqGen generates monotonically increasing sequence numbers per session.
type SeqGen struct {
	mu  sync.Mutex
	seq map[string]int64
}

// NewSeqGen creates a new sequence generator.
func NewSeqGen() *SeqGen {
	return &SeqGen{seq: make(map[string]int64)}
}

// Peek returns the current sequence number for a session without incrementing.
func (g *SeqGen) Peek(sessionID string) int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.seq[sessionID]
}

// Next returns the next sequence number for a session.
func (g *SeqGen) Next(sessionID string) int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.seq[sessionID] + 1
	g.seq[sessionID] = n
	return n
}
