package admin

import (
	"sync"
	"time"
)

type logEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Msg     string `json:"msg"`
	Session string `json:"session_id,omitempty"`
}

type logRingBuffer struct {
	mu   sync.Mutex
	ent  []logEntry
	head int
	n    int // total entries ever added
}

func newLogRing(cap int) *logRingBuffer {
	return &logRingBuffer{ent: make([]logEntry, cap)}
}

func (r *logRingBuffer) Add(level, msg, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ent[r.head%len(r.ent)] = logEntry{
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Level:   level,
		Msg:     msg,
		Session: sessionID,
	}
	r.head++
	r.n++
}

func (r *logRingBuffer) Total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func (r *logRingBuffer) Recent(limit int) ([]logEntry, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.n == 0 {
		return nil, 0
	}
	size := len(r.ent)
	size = min(r.n, size)
	if limit > 0 && limit < size {
		size = limit
	}
	// start from oldest
	start := (r.head - size) % len(r.ent)
	out := make([]logEntry, 0, size)
	for i := 0; i < size; i++ {
		idx := (start + i) % len(r.ent)
		out = append(out, r.ent[idx])
	}
	return out, r.n
}

type LogCollector interface {
	Recent(limit int) ([]logEntry, int)
}

var LogRing = newLogRing(100)

func AddLog(level, msg, sessionID string) {
	LogRing.Add(level, msg, sessionID)
}
