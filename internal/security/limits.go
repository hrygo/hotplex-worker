package security

import (
	"fmt"
	"sync"
)

const (
	// MaxLineBytes is the maximum allowed size of a single output line.
	// This prevents individual very large lines from consuming memory.
	MaxLineBytes = 10 * 1024 * 1024 // 10 MB

	// MaxSessionBytes is the maximum total output bytes per session.
	// This prevents runaway workers from exhausting gateway memory.
	MaxSessionBytes = 20 * 1024 * 1024 // 20 MB

	// MaxEnvelopeBytes is the maximum size of a single AEP envelope.
	MaxEnvelopeBytes = 1 * 1024 * 1024 // 1 MB
)

// OutputLimiter tracks and enforces per-session output limits.
type OutputLimiter struct {
	mu         sync.Mutex
	totalBytes int64
}

// Check verifies that appending the given line stays within session limits.
// Returns nil if the line is accepted, or an error describing the violation.
func (l *OutputLimiter) Check(line []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if int64(len(line)) > MaxLineBytes {
		return fmt.Errorf("security: line exceeds %d byte limit (%d bytes)", MaxLineBytes, len(line))
	}

	if l.totalBytes+int64(len(line)) > MaxSessionBytes {
		return fmt.Errorf("security: session output exceeds %d byte limit", MaxSessionBytes)
	}

	l.totalBytes += int64(len(line))
	return nil
}

// Total returns the total bytes consumed so far.
func (l *OutputLimiter) Total() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.totalBytes
}

// Reset clears the byte counter. Use when starting a new session on the same limiter.
func (l *OutputLimiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.totalBytes = 0
}
