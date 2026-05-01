package gateway

import "sync"

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
