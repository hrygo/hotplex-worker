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

	"github.com/hotplex/hotplex-worker/internal/config"
	"github.com/hotplex/hotplex-worker/internal/metrics"
	"github.com/hotplex/hotplex-worker/internal/security"
	"github.com/hotplex/hotplex-worker/internal/session"
	"github.com/hotplex/hotplex-worker/internal/tracing"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
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
	// afterDrain is called (blocking) by Run after routeMessage finishes processing this item.
	// Tests use it to synchronize against the drain goroutine.
	afterDrain func()
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
// If the session already has another connection, the old ones are removed from
// the session routing map (no longer receive events) and left to close
// naturally when their WebSocket read loop encounters the closed socket.
// This prevents the race where worker responses go to a stale connection,
// while avoiding the reconnect storms caused by forcibly closing connections
// (which triggers client WebSocket onclose → reconnect loops).
// This implements the "按 session_id 去重连接，只保留最新连接" rule.
func (h *Hub) JoinSession(sessionID string, conn *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Remove stale connections from session routing only — do NOT call Close().
	// Each removed conn's ReadPump goroutine will exit naturally when the
	// underlying TCP connection is torn down (either by the client closing
	// its end, or by WritePump detecting the dead socket on next write).
	// This avoids triggering the client's WebSocket onclose → reconnect logic.
	if existing, ok := h.sessions[sessionID]; ok {
		for c := range existing {
			if c != conn {
				delete(existing, c)
				h.log.Info("gateway: removed stale conn from session", "session_id", sessionID)
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

// sendBroadcast sends to the broadcast channel. Returns false if the hub is
// shutting down (ctx cancelled). Uses select with ctx.Done() instead of
// close(channel)+recover() to avoid the send-on-closed-channel data race.
func (h *Hub) sendBroadcast(msg *EnvelopeWithConn) (sent bool) {
	select {
	case h.broadcast <- msg:
		return true
	case <-h.ctx.Done():
		return false
	}
}

// SendToSession delivers a message to all connections subscribed to a session.
// Control-priority messages bypass the broadcast queue.
// afterDrain functions are called sequentially after the item is routed by Run.
func (h *Hub) SendToSession(ctx context.Context, env *events.Envelope, afterDrain ...func()) error {
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
	// afterDrainCallback is called by Run after the item is routed; nil if not supplied.
	var afterDrainCallback func()
	if len(afterDrain) > 0 {
		afterDrainCallback = afterDrain[0]
	}

	if env.Priority == events.PriorityControl {
		return h.sendControlToSession(spanCtx, env)
	}

	// Clone before broadcast: Bridge.forwardEvents holds a reference to the
	// original envelope and may call aep.EncodeJSON(env) concurrently (e.g.,
	// for msgStore.Append). Cloning here ensures the channel holds an
	// independent copy, eliminating the race with Bridge.forwardEvents.
	// The clone is created inside the select to keep the backpressure check
	// and send atomic (same as the original code).
	if isDroppable(env.Event.Type) {
		if h.sendBroadcast(&EnvelopeWithConn{Env: events.Clone(env), afterDrain: afterDrainCallback}) {
			return nil
		}
		// sendBroadcast returned false = channel closed; drop delta silently.
		h.mu.Lock()
		h.sessionDropped[env.SessionID] = true
		h.mu.Unlock()
		metrics.GatewayDeltasDropped.Inc()
		return nil
	}

	// Guaranteed delivery path.
	if h.sendBroadcast(&EnvelopeWithConn{Env: events.Clone(env), afterDrain: afterDrainCallback}) {
		return nil
	}
	return errors.New("gateway: broadcast channel closed")
}

func (h *Hub) sendControlToSession(ctx context.Context, env *events.Envelope) error {
	h.mu.RLock()
	sessionConns := h.sessions[env.SessionID]
	conns := make([]*Conn, 0, len(sessionConns))
	for conn := range sessionConns {
		conns = append(conns, conn)
	}
	h.mu.RUnlock()

	if len(conns) == 0 {
		return nil
	}

	env = events.Clone(env)
	for _, conn := range conns {
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

	// Give the handler access to sm for the init handshake.
	handler.sm = sm

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Authenticate at HTTP upgrade time.
		userID, botID, err := auth.AuthenticateRequest(r)
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

		c := newConn(h, wc, sessionID, bridge)
		c.userID = userID
		c.botID = botID // SEC-007: carry botID from HTTP-level JWT extraction
		h.RegisterConn(c)
		h.JoinSession(sessionID, c)

		// Start read and write pumps in background.
		go c.ReadPump(handler)
		go c.WritePump()

		h.log.Info("gateway: WS connected", "session_id", sessionID, "user_id", userID, "bot_id", botID)
	})
}

// Run starts the hub's run loop. It blocks until the context is cancelled.
// The broadcast channel is never closed — sendBroadcast uses ctx.Done() to
// detect shutdown, and this function drains remaining messages non-blockingly.
func (h *Hub) Run() {
	h.log.Info("gateway: hub running")

	for {
		select {
		case <-h.ctx.Done():
			h.drainBroadcast()
			return
		case msg := <-h.broadcast:
			if msg == nil || msg.Env == nil {
				continue
			}
			_, span := tracing.SpanFromContext(h.ctx).Start(h.ctx, "hub.broadcast")
			span.SetAttributes(
				tracing.Attr("session_id", msg.Env.SessionID),
				tracing.Attr("event_type", string(msg.Env.Event.Type)),
				tracing.Attr("seq", msg.Env.Seq),
			)
			h.routeMessage(msg)
			span.End()
			if msg.afterDrain != nil {
				msg.afterDrain()
			}
		}
	}
}

func (h *Hub) routeMessage(msg *EnvelopeWithConn) {
	// Snapshot connections under RLock to avoid iterating a shared map
	// that UnregisterConn may modify concurrently.
	h.mu.RLock()
	sessionConns := h.sessions[msg.Env.SessionID]
	conns := make([]*Conn, 0, len(sessionConns))
	for conn := range sessionConns {
		conns = append(conns, conn)
	}
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

	// Note: msg.Env is already a clone created by SendToSession before
	// placing it on the broadcast channel, so aep.EncodeJSON can safely
	// mutate its Version field without racing with Bridge.forwardEvents.
	encoded, err := aep.EncodeJSON(msg.Env)
	if err != nil {
		h.log.Error("gateway: encode failed", "err", err)
		return
	}

	for _, conn := range conns {
		metrics.GatewayMessagesTotal.WithLabelValues("outgoing", string(msg.Env.Event.Type)).Inc()
		if err := conn.WriteMessage(websocket.TextMessage, encoded); err != nil {
			h.log.Warn("gateway: write failed", "err", err)
			conn.Close()
		}
	}
}

// drainBroadcast processes remaining messages in the broadcast channel.
// Non-blocking: returns when the channel is empty. Since sendBroadcast checks
// ctx.Done() before sending, no new messages arrive after context cancellation.
func (h *Hub) drainBroadcast() {
	for {
		select {
		case msg := <-h.broadcast:
			if msg != nil && msg.Env != nil {
				h.routeMessage(msg)
				if msg.afterDrain != nil {
					msg.afterDrain()
				}
			}
		default:
			return
		}
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
// It signals Run() to stop via context cancellation, waits for in-flight
// broadcast messages to drain, then closes all WebSocket connections.
// The ctx deadline controls the maximum wait time.
func (h *Hub) Shutdown(ctx context.Context) error {
	h.cancel()

	// Wait briefly for Run() to drain remaining messages.
	// Run() handles drain in its ctx.Done() path. The broadcast channel
	// is never closed — it's GC'd with the Hub.
	drainDone := make(chan struct{})
	go func() {
		// Give Run() a moment to process its ctx.Done path.
		// This also handles the case where Run() was never started.
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
