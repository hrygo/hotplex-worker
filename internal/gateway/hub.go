// Package gateway implements the WebSocket gateway that speaks AEP v1 to clients.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/tracing"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// isReadTimeout reports whether err is a read deadline exceeded error.
func isReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrDeadlineExceeded)
}

// isDroppable reports whether an event kind can be dropped under backpressure.
func isDroppable(kind events.Kind) bool {
	return kind == events.MessageDelta || kind == events.Raw
}

// broadcastQueueSize returns the broadcast channel buffer size from config.
// A value of 0 means unbounded (not recommended for production).
func broadcastQueueSize(cfg *config.Config) int {
	if cfg.Gateway.BroadcastQueueSize <= 0 {
		return 256 // default
	}
	return cfg.Gateway.BroadcastQueueSize
}

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 32 * 1024
)

// SessionWriter is the minimal interface satisfied by both *Conn and
// platform connection wrappers. It is used as the value type in the
// sessions routing map.
type SessionWriter interface {
	WriteCtx(ctx context.Context, env *events.Envelope) error
	Close() error
}

// Hub is the central message router and connection registry.
// All WebSocket connections and session→connection mappings are managed here.
type Hub struct {
	log      *slog.Logger
	cfgStore *config.ConfigStore

	upgrader websocket.Upgrader

	mu       sync.RWMutex
	conns    map[*Conn]struct{}                // all active connections
	sessions map[string]map[SessionWriter]bool // sessionID → connections

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

	// InitThrottle prevents handshake loops.
	InitThrottle *handshakeThrottle
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
func NewHub(log *slog.Logger, cfgStore *config.ConfigStore) *Hub {
	if log == nil {
		log = slog.Default()
	}
	if cfgStore == nil {
		panic("gateway: Hub requires ConfigStore")
	}
	cfg := cfgStore.Load()
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{
		log:            log.With("service.name", "hotplex-gateway"),
		cfgStore:       cfgStore,
		conns:          make(map[*Conn]struct{}),
		sessions:       make(map[string]map[SessionWriter]bool),
		seqGen:         NewSeqGen(),
		sessionDropped: make(map[string]bool),
		broadcast:      make(chan *EnvelopeWithConn, broadcastQueueSize(cfg)),
		ctx:            ctx,
		cancel:         cancel,
		InitThrottle:   newHandshakeThrottle(),
	}

	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  cfg.Gateway.ReadBufferSize,
		WriteBufferSize: cfg.Gateway.WriteBufferSize,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			for _, allowed := range h.cfgStore.Load().Security.AllowedOrigins {
				if allowed == "*" || allowed == origin {
					return true
				}
			}
			return false
		},
	}
	go h.Run()
	return h
}

// RegisterConn registers a new WebSocket connection.
func (h *Hub) RegisterConn(conn *Conn) {
	h.mu.Lock()
	h.conns[conn] = struct{}{}
	h.mu.Unlock()
	metrics.GatewayConnectionsOpen.Inc()
	h.log.Debug("gateway: conn registered", "remote", conn.RemoteAddr(), "session_id", conn.sessionID)
}

// UnregisterConn removes a connection and cleans up session mappings.
// Session-level entries (seqGen, sessionDropped) are cleaned up when a session
// has no remaining connections.
func (h *Hub) UnregisterConn(conn *Conn) {
	h.mu.Lock()
	delete(h.conns, conn)
	for sid := range h.sessions {
		h.removeSession(sid, conn)
	}
	h.mu.Unlock()
	metrics.GatewayConnectionsOpen.Dec()
	h.log.Debug("gateway: conn unregistered", "remote", conn.RemoteAddr(), "session_id", conn.sessionID)
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
				h.log.Info("gateway: removed stale conn from session", "session_id", sessionID, "remote", conn.RemoteAddr())
			}
		}
	}

	if h.sessions[sessionID] == nil {
		h.sessions[sessionID] = make(map[SessionWriter]bool)
	}
	h.sessions[sessionID][conn] = true
}

// LeaveSession unsubscribes conn from a session.
// If the session has no remaining connections, session-level entries (seqGen,
// sessionDropped) are cleaned up to prevent memory leaks.
func (h *Hub) LeaveSession(sessionID string, conn *Conn) {
	h.mu.Lock()
	h.removeSession(sessionID, conn)
	h.mu.Unlock()
}

// removeSession removes conn from sessionID and cleans up empty sessions.
// Caller must hold h.mu.
func (h *Hub) removeSession(sessionID string, conn SessionWriter) {
	if conns, ok := h.sessions[sessionID]; ok {
		delete(conns, conn)
		if len(conns) == 0 {
			delete(h.sessions, sessionID)
			delete(h.sessionDropped, sessionID)
			h.seqGen.Remove(sessionID)
		}
	}
}

// JoinPlatformSession subscribes a PlatformConn to receive events for a session.
// Unlike JoinSession, it does not register the connection in h.conns (no WS tracking)
// and does not remove stale connections (platform SDK handles its own lifecycle).
// Deduplicates: if the same PlatformConn is already subscribed, this is a no-op.
func (h *Hub) JoinPlatformSession(sessionID string, pc messaging.PlatformConn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.sessions[sessionID] == nil {
		h.sessions[sessionID] = make(map[SessionWriter]bool)
	}

	for sw := range h.sessions[sessionID] {
		if pce, ok := sw.(*pcEntry); ok && pce.pc == pc {
			return
		}
	}

	h.sessions[sessionID][newPCEntry(pc, defaultPCEntryConfig(h.cfgStore.Load()))] = true
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
		h.sendControlToSession(spanCtx, env)
		return nil
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

func (h *Hub) sendControlToSession(ctx context.Context, env *events.Envelope) {
	h.mu.RLock()
	sessionConns := h.sessions[env.SessionID]
	conns := make([]SessionWriter, 0, len(sessionConns))
	for conn := range sessionConns {
		conns = append(conns, conn)
	}
	h.mu.RUnlock()

	if len(conns) == 0 {
		return
	}

	env = events.Clone(env)
	for _, conn := range conns {
		if err := conn.WriteCtx(ctx, env); err != nil {
			h.log.Warn("gateway: send to conn failed", "session_id", env.SessionID, "err", err)
		}
	}
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
	// Start periodic cleanup for throttler
	throttleCleanup := time.NewTicker(10 * time.Minute)
	defer throttleCleanup.Stop()

	for {
		select {
		case <-h.ctx.Done():
			h.drainBroadcast()
			return
		case <-throttleCleanup.C:
			h.InitThrottle.Cleanup()
		case msg := <-h.broadcast:
			if msg == nil || msg.Env == nil {
				continue
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						h.log.Error("hub: panic in routeMessage", "session_id", msg.Env.SessionID, "panic", r, "stack", string(debug.Stack()))
					}
				}()
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
			}()
		}
	}
}

func (h *Hub) routeMessage(msg *EnvelopeWithConn) {
	h.mu.RLock()
	sessionConns := h.sessions[msg.Env.SessionID]
	conns := make([]SessionWriter, 0, len(sessionConns))
	for conn := range sessionConns {
		conns = append(conns, conn)
	}
	h.mu.RUnlock()

	if len(conns) == 0 {
		return
	}

	if h.LogHandler != nil {
		level := "INFO"
		switch msg.Env.Event.Type {
		case events.Error:
			level = "ERROR"
		case events.State:
			level = "WARN"
		}
		h.LogHandler(level, fmt.Sprintf("event %s seq=%d", msg.Env.Event.Type, msg.Env.Seq), msg.Env.SessionID)
	}

	var encoded []byte
	var err error
	for _, conn := range conns {
		metrics.GatewayMessagesTotal.WithLabelValues("outgoing", string(msg.Env.Event.Type)).Inc()
		if c, ok := conn.(*Conn); ok {
			// Lazy encode: only compute when first WS conn is seen.
			if encoded == nil {
				encoded, err = aep.EncodeJSON(msg.Env)
				if err != nil {
					h.log.Error("gateway: encode failed", "err", err)
					return
				}
			}
			if err := c.WriteMessage(websocket.TextMessage, encoded); err != nil {
				h.log.Warn("gateway: write failed", "session_id", msg.Env.SessionID, "err", err)
				_ = conn.Close()
			}
		} else {
			if err := conn.WriteCtx(context.Background(), msg.Env); err != nil {
				h.log.Warn("gateway: platform write enqueue failed", "session_id", msg.Env.SessionID, "err", err)
				_ = conn.Close()
				h.mu.Lock()
				h.removeSession(msg.Env.SessionID, conn)
				h.mu.Unlock()
			}
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
	// Collect platform connections from sessions map. These are not in h.conns
	// and must be closed here since Hub.Shutdown is the canonical shutdown point.
	seenPC := make(map[*pcEntry]bool)
	var pcConns []*pcEntry
	for _, conns := range h.sessions {
		for sw := range conns {
			if pce, ok := sw.(*pcEntry); ok && !seenPC[pce] {
				seenPC[pce] = true
				pcConns = append(pcConns, pce)
			}
		}
	}
	h.mu.RUnlock()

	var errs []error
	for _, c := range conns {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, pce := range pcConns {
		if err := pce.Close(); err != nil {
			errs = append(errs, fmt.Errorf("platform conn close: %w", err))
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

// Remove deletes the sequence counter for a session.
func (g *SeqGen) Remove(sessionID string) {
	g.mu.Lock()
	delete(g.seq, sessionID)
	g.mu.Unlock()
}

// pcEntry wraps a PlatformConn with an async writeLoop goroutine and delta
// coalescing. It satisfies the SessionWriter interface.
//
// WriteCtx sends envelopes to a buffered channel; a dedicated writeLoop
// goroutine reads from the channel, coalesces consecutive droppable events
// (message.delta / raw), and forwards merged or individual envelopes to the
// underlying PlatformConn. This decouples Hub.Run() from blocking platform
// HTTP API calls.
type pcEntry struct {
	pc      messaging.PlatformConn
	cfg     pcEntryConfig
	ch      chan *events.Envelope
	done    chan struct{}
	closeMu sync.Once
}

type pcEntryConfig struct {
	WriteBuffer   int
	DropThreshold int
	CoalesceIntvl time.Duration
	CoalesceSize  int
}

func defaultPCEntryConfig(cfg *config.Config) pcEntryConfig {
	c := pcEntryConfig{
		WriteBuffer:   cfg.Gateway.PlatformWriteBuffer,
		DropThreshold: cfg.Gateway.PlatformDropThreshold,
		CoalesceIntvl: cfg.Gateway.DeltaCoalesceInterval,
		CoalesceSize:  cfg.Gateway.DeltaCoalesceSize,
	}
	if c.WriteBuffer <= 0 {
		c.WriteBuffer = 64
	}
	if c.DropThreshold <= 0 {
		c.DropThreshold = 56
	}
	if c.CoalesceIntvl <= 0 {
		c.CoalesceIntvl = 120 * time.Millisecond
	}
	if c.CoalesceSize <= 0 {
		c.CoalesceSize = 200
	}
	return c
}

func newPCEntry(pc messaging.PlatformConn, cfg pcEntryConfig) *pcEntry {
	e := &pcEntry{
		pc:   pc,
		ch:   make(chan *events.Envelope, cfg.WriteBuffer),
		done: make(chan struct{}),
		cfg:  cfg,
	}
	go e.writeLoop()
	return e
}

func (e *pcEntry) WriteCtx(_ context.Context, env *events.Envelope) error {
	if isDroppable(env.Event.Type) {
		if len(e.ch) >= e.cfg.DropThreshold {
			metrics.GatewayPlatformDroppedTotal.WithLabelValues(string(env.Event.Type)).Inc()
			return nil
		}
		select {
		case e.ch <- env:
			return nil
		default:
			metrics.GatewayPlatformDroppedTotal.WithLabelValues(string(env.Event.Type)).Inc()
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case e.ch <- env:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("platform conn write timeout: buffer full")
	case <-e.done:
		return errors.New("platform conn closed")
	}
}

// Close signals writeLoop to drain pending deltas and exit, waits for
// completion, then closes the underlying PlatformConn.
func (e *pcEntry) Close() error {
	var err error
	e.closeMu.Do(func() {
		close(e.ch)
		<-e.done
		err = e.pc.Close()
	})
	return err
}

// writeLoop reads envelopes from the channel, coalesces consecutive droppable
// events into merged deltas, and forwards them to the underlying PlatformConn.
func (e *pcEntry) writeLoop() {
	defer close(e.done)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("pcEntry writeLoop panic", "panic", r, "stack", string(debug.Stack()))
		}
	}()

	var db strings.Builder
	var timer *time.Timer
	var timerCh <-chan time.Time
	var pendingSID string // tracks SessionID for pending coalesced deltas

	flush := func(sid string) {
		if db.Len() == 0 {
			return
		}
		merged := &events.Envelope{
			Version:   events.Version,
			ID:        aep.NewID(),
			SessionID: sid,
			Event: events.Event{
				Type: events.MessageDelta,
				Data: events.MessageDeltaData{
					Content: db.String(),
				},
			},
		}
		metrics.GatewayDeltaFlushTotal.WithLabelValues(sid).Inc()
		db.Reset()
		if timer != nil {
			timer.Stop()
			timerCh = nil
		}
		e.writeOne(merged)
	}

	for {
		select {
		case env, ok := <-e.ch:
			if !ok {
				flush(pendingSID)
				return
			}

			if isDroppable(env.Event.Type) {
				content := extractDeltaContent(env)
				if db.Len() == 0 {
					pendingSID = env.SessionID
				}
				db.WriteString(content)
				metrics.GatewayDeltaCoalescedTotal.WithLabelValues(env.SessionID).Inc()

				if utf8.RuneCountInString(db.String()) >= e.cfg.CoalesceSize {
					flush(pendingSID)
				} else if timer == nil {
					timer = time.NewTimer(e.cfg.CoalesceIntvl)
					timerCh = timer.C
				} else {
					timer.Reset(e.cfg.CoalesceIntvl)
				}
			} else {
				flush(pendingSID)
				e.writeOne(env)
			}

		case <-timerCh:
			flush(pendingSID)
		}
	}
}

func (e *pcEntry) writeOne(env *events.Envelope) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.pc.WriteCtx(ctx, env); err != nil {
		slog.Warn("platform async write failed",
			"event_type", env.Event.Type,
			"session_id", env.SessionID,
			"err", err)
	}
}

func extractDeltaContent(env *events.Envelope) string {
	switch env.Event.Type {
	case events.MessageDelta:
		// Data arrives as map[string]any from JSON unmarshal; struct type never matches.
		if m, ok := env.Event.Data.(map[string]any); ok {
			if c, _ := m["content"].(string); c != "" {
				return c
			}
		}
	case events.Raw:
		if d, ok := env.Event.Data.(events.RawData); ok {
			if m, ok := d.Raw.(map[string]any); ok {
				if t, _ := m["text"].(string); t != "" {
					return t
				}
			}
		}
	}
	if s, ok := env.Event.Data.(string); ok {
		return s
	}
	return ""
}
