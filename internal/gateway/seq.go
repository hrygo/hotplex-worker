package gateway

import (
	"sync"
	"sync/atomic"
)

// SeqGen generates monotonically increasing sequence numbers per session.
// Uses sync.Map + per-session atomic.Int64 to eliminate cross-session contention.
type SeqGen struct {
	seq sync.Map // sessionID → *atomic.Int64
}

// NewSeqGen creates a new sequence generator.
func NewSeqGen() *SeqGen {
	return &SeqGen{}
}

// Peek returns the current sequence number for a session without incrementing.
func (g *SeqGen) Peek(sessionID string) int64 {
	val, ok := g.seq.Load(sessionID)
	if !ok {
		return 0
	}
	return val.(*atomic.Int64).Load() //nolint:errcheck // LoadOrStore guarantees *atomic.Int64
}

// Next returns the next sequence number for a session.
func (g *SeqGen) Next(sessionID string) int64 {
	val, _ := g.seq.LoadOrStore(sessionID, new(atomic.Int64))
	return val.(*atomic.Int64).Add(1) //nolint:errcheck // LoadOrStore guarantees *atomic.Int64
}

// Remove deletes the sequence counter for a session.
func (g *SeqGen) Remove(sessionID string) {
	g.seq.Delete(sessionID)
}
