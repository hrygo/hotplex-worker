package messaging

import (
	"context"
	"strings"
	"sync"
	"time"
)

// StreamingAPI defines the platform-specific streaming operations.
// Each adapter implements this to wrap its native streaming API.
type StreamingAPI interface {
	// StartStream initiates a new streaming session.
	// Returns a streamID for subsequent operations.
	StartStream(ctx context.Context, initialContent string) (streamID string, err error)

	// AppendContent adds content to an active stream.
	AppendContent(ctx context.Context, streamID, content string) error

	// StopStream terminates a streaming session.
	StopStream(ctx context.Context, streamID string) error

	// PatchFallback sends content via a non-streaming fallback path.
	PatchFallback(ctx context.Context, streamID, content string) error
}

// StreamState tracks shared streaming state: buffering, TTL, integrity.
type StreamState struct {
	mu            sync.Mutex
	buf           strings.Builder
	lastFlushed   string
	bytesWritten  int64
	bytesFlushed  int64
	failedFlushes int
	streamStart   time.Time
	streamExpired bool
	started       bool
}

// Write appends content to the buffer and returns the total buffered size.
func (s *StreamState) Write(content string) (totalRunes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.WriteString(content)
	s.bytesWritten += int64(len(content))
	return s.buf.Len()
}

// Snapshot returns the current buffered content and its length.
func (s *StreamState) Snapshot() (content string, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content = s.buf.String()
	changed = content != s.lastFlushed
	return
}

// MarkFlushed records the last flushed content.
func (s *StreamState) MarkFlushed(content string, bytesFlushed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFlushed = content
	s.bytesFlushed += int64(bytesFlushed)
}

// RecordFlushFailure increments the failed flush counter.
func (s *StreamState) RecordFlushFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failedFlushes++
}

// Integrity returns the ratio of flushed to written bytes.
func (s *StreamState) Integrity() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bytesWritten == 0 {
		return 1.0
	}
	return float64(s.bytesFlushed) / float64(s.bytesWritten)
}

// CheckTTL checks whether the stream has exceeded its TTL.
func (s *StreamState) CheckTTL(ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamStart.IsZero() {
		s.streamStart = time.Now()
		s.started = true
		return false
	}
	if time.Since(s.streamStart) > ttl {
		s.streamExpired = true
		return true
	}
	return false
}

// Expired reports whether the stream has been marked as expired.
func (s *StreamState) Expired() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streamExpired
}

// Content returns the full buffered content.
func (s *StreamState) Content() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// BytesWritten returns total bytes written to the buffer.
func (s *StreamState) BytesWritten() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bytesWritten
}

// StreamStateMetrics provides read-only metrics for logging.
type StreamStateMetrics struct {
	BytesWritten  int64
	BytesFlushed  int64
	FailedFlushes int
	Integrity     float64
	Expired       bool
}

// Metrics returns a snapshot of streaming state metrics.
func (s *StreamState) Metrics() StreamStateMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := StreamStateMetrics{
		BytesWritten:  s.bytesWritten,
		BytesFlushed:  s.bytesFlushed,
		FailedFlushes: s.failedFlushes,
		Expired:       s.streamExpired,
	}
	if s.bytesWritten > 0 {
		m.Integrity = float64(s.bytesFlushed) / float64(s.bytesWritten)
	} else {
		m.Integrity = 1.0
	}
	return m
}
