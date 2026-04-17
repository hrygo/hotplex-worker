// Package feishu provides bounded deduplication with FIFO eviction.
package feishu

import (
	"sync"
	"time"
)

const (
	dedupDefaultTTL        = 12 * time.Hour
	dedupDefaultMaxEntries = 5000
	dedupSweepInterval     = 5 * time.Minute
	dedupMessageExpiry     = 30 * time.Minute
)

// Dedup provides bounded deduplication for message IDs.
// It uses FIFO eviction when at capacity and periodic TTL cleanup.
// This replaces the unbounded map[string]time.Time in the adapter.
type Dedup struct {
	mu         sync.Mutex
	entries    map[string]time.Time // messageID → seenAt
	order      []string             // FIFO eviction order
	maxEntries int
	ttl        time.Duration
}

// NewDedup creates a new Dedup with the given capacity and TTL.
func NewDedup(maxEntries int, ttl time.Duration) *Dedup {
	if maxEntries <= 0 {
		maxEntries = dedupDefaultMaxEntries
	}
	if ttl <= 0 {
		ttl = dedupDefaultTTL
	}
	return &Dedup{
		entries:    make(map[string]time.Time),
		order:      make([]string, 0, maxEntries),
		maxEntries: maxEntries,
		ttl:        ttl,
	}
}

// TryRecord attempts to record a message ID.
// Returns true if the ID is new (accepted), false if it's a duplicate (rejected).
// When at capacity, the oldest entry is evicted (FIFO).
func (d *Dedup) TryRecord(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, seen := d.entries[id]; seen {
		return false
	}

	// FIFO eviction when at capacity.
	for len(d.entries) >= d.maxEntries && len(d.order) > 0 {
		oldest := d.order[0]
		d.order = d.order[1:]
		delete(d.entries, oldest)
	}

	d.entries[id] = time.Now()
	d.order = append(d.order, id)
	return true
}

// Sweep removes expired entries. Call periodically from a background goroutine.
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

// IsExpired checks if a message's create time is beyond the expiry threshold.
// Returns true if the message should be discarded as stale.
func IsMessageExpired(createTimeMs int64) bool {
	if createTimeMs <= 0 {
		return false
	}
	return time.Since(time.UnixMilli(createTimeMs)) > dedupMessageExpiry
}
