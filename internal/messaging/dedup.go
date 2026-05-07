package messaging

import (
	"sync"
	"time"
)

// Dedup is a bounded TTL dedup map that prevents duplicate message processing.
// When maxEntries is exceeded, the oldest entries are evicted in FIFO order.
// Supports both self-cleanup (via StartCleanup/Close) and manual Sweep.
type Dedup struct {
	mu         sync.Mutex
	entries    map[string]time.Time
	order      []string // FIFO eviction order
	maxEntries int
	ttl        time.Duration
	done       chan struct{}
	closeOnce  sync.Once
}

// NewDedup creates a new bounded dedup map.
// If maxEntries <= 0, defaults to 5000. If ttl <= 0, defaults to 12h.
func NewDedup(maxEntries int, ttl time.Duration) *Dedup {
	if maxEntries <= 0 {
		maxEntries = 5000
	}
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &Dedup{
		entries:    make(map[string]time.Time),
		order:      make([]string, 0, maxEntries),
		maxEntries: maxEntries,
		ttl:        ttl,
	}
}

// StartCleanup launches a background goroutine that periodically sweeps expired entries.
// Call Close to stop the goroutine. The sweep interval is ttl/2.
func (d *Dedup) StartCleanup() {
	d.done = make(chan struct{})
	go d.cleanupLoop()
}

// TryRecord records an id and returns true if it was not previously seen.
// Returns false if the id is a duplicate.
func (d *Dedup) TryRecord(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, seen := d.entries[id]; seen {
		return false
	}

	for len(d.entries) >= d.maxEntries && len(d.order) > 0 {
		// Use writeIdx compaction to release backing array references.
		writeIdx := 0
		target := len(d.entries) - d.maxEntries + 1
		for _, id := range d.order {
			if target > 0 {
				delete(d.entries, id)
				target--
			} else {
				d.order[writeIdx] = id
				writeIdx++
			}
		}
		d.order = d.order[:writeIdx]
	}

	d.entries[id] = time.Now()
	d.order = append(d.order, id)
	return true
}

// Sweep removes expired entries. Can be called manually or from a background goroutine.
func (d *Dedup) Sweep() {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-d.ttl)
	writeIdx := 0
	for _, id := range d.order {
		if t, ok := d.entries[id]; ok && t.After(cutoff) {
			d.order[writeIdx] = id
			writeIdx++
		} else {
			delete(d.entries, id)
		}
	}
	d.order = d.order[:writeIdx]
}

// Len returns the number of tracked entries.
func (d *Dedup) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.entries)
}

// Close stops the cleanup goroutine started by StartCleanup.
func (d *Dedup) Close() {
	d.closeOnce.Do(func() {
		if d.done != nil {
			close(d.done)
		}
	})
}

func (d *Dedup) cleanupLoop() {
	ticker := time.NewTicker(d.ttl / 2)
	defer ticker.Stop()

	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			d.Sweep()
		}
	}
}
