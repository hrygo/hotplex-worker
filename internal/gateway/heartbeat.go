// Package gateway implements the WebSocket gateway that speaks AEP v1 to clients.
package gateway

import (
	"log/slog"
	"sync"
)

// heartbeat tracks missed pong count for a connection.
// Actual ping/pong I/O is handled by Conn.ReadPump / Conn.WritePump.
// heartbeat is safe for concurrent use.
type heartbeat struct {
	log *slog.Logger

	mu        sync.Mutex
	missed    int // consecutive missed pongs
	maxMiss   int
	stopped   bool
	stoppedCh chan struct{}
}

// newHeartbeat creates a heartbeat tracker.
func newHeartbeat(log *slog.Logger) *heartbeat {
	if log == nil {
		log = slog.Default()
	}
	return &heartbeat{
		log:       log,
		maxMiss:   3,
		stoppedCh: make(chan struct{}),
	}
}

// MarkAlive records that the remote responded to a ping.
func (h *heartbeat) MarkAlive() {
	h.mu.Lock()
	h.missed = 0
	h.mu.Unlock()
}

// MarkMissed increments the missed-pong counter.
// Returns true if the maximum consecutive misses has been reached.
func (h *heartbeat) MarkMissed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return false
	}
	h.missed++
	exceeded := h.missed >= h.maxMiss
	h.log.Debug("gateway: pong missed", "count", h.missed, "max", h.maxMiss)
	return exceeded
}

// MissedCount returns the current consecutive missed-pong count.
func (h *heartbeat) MissedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.missed
}

// Stop stops the heartbeat. Idempotent.
func (h *heartbeat) Stop() {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	h.stopped = true
	close(h.stoppedCh)
	h.mu.Unlock()
}

// Stopped returns a channel that is closed when the heartbeat is stopped.
func (h *heartbeat) Stopped() <-chan struct{} {
	return h.stoppedCh
}
