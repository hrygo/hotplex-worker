package slack

import (
	"sync"
	"time"
)

// Dedup is a bounded TTL dedup map that prevents duplicate message processing.
// When maxEntries is exceeded, the oldest entries are evicted in FIFO order.
type Dedup struct {
	mu         sync.Mutex
	entries    map[string]time.Time
	order      []string // FIFO eviction order
	maxEntries int
	ttl        time.Duration
	done       chan struct{}
}

// NewDedup creates a new bounded dedup map.
func NewDedup(maxEntries int, ttl time.Duration) *Dedup {
	d := &Dedup{
		entries:    make(map[string]time.Time),
		maxEntries: maxEntries,
		ttl:        ttl,
		done:       make(chan struct{}),
	}
	go d.cleanupLoop()
	return d
}

// TryRecord records an id and returns true if it was not previously seen.
// Returns false if the id is a duplicate.
func (d *Dedup) TryRecord(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, seen := d.entries[id]; seen {
		return false
	}

	// Evict oldest entries if at capacity
	for len(d.entries) >= d.maxEntries && len(d.order) > 0 {
		oldest := d.order[0]
		d.order = d.order[1:]
		delete(d.entries, oldest)
	}

	d.entries[id] = time.Now()
	d.order = append(d.order, id)
	return true
}

// Close stops the cleanup goroutine.
func (d *Dedup) Close() {
	close(d.done)
}

func (d *Dedup) cleanupLoop() {
	ticker := time.NewTicker(d.ttl / 2)
	defer ticker.Stop()

	for {
		select {
		case <-d.done:
			return
		case <-ticker.C:
			d.mu.Lock()
			now := time.Now()
			writeIdx := 0
			for _, id := range d.order {
				if ts, ok := d.entries[id]; ok {
					if now.Sub(ts) > d.ttl {
						delete(d.entries, id)
					} else {
						d.order[writeIdx] = id
						writeIdx++
					}
				}
			}
			d.order = d.order[:writeIdx]
			d.mu.Unlock()
		}
	}
}
