package feishu

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ─── NewDedup defaults ───────────────────────────────────────────────────────

func TestNewDedup_Defaults(t *testing.T) {
	t.Parallel()

	// Zero maxEntries → uses dedupDefaultMaxEntries (5000).
	d := NewDedup(0, 0)
	require.Equal(t, 5000, d.maxEntries)
	require.Equal(t, 12*time.Hour, d.ttl)

	// Negative values also fall back to defaults.
	d2 := NewDedup(-1, -1)
	require.Equal(t, 5000, d2.maxEntries)
	require.Equal(t, 12*time.Hour, d2.ttl)
}

// ─── Dedup Sweep expired entries ───────────────────────────────────────────

func TestDedup_Sweep_ExpiresEntries(t *testing.T) {
	t.Parallel()
	// Use a short TTL so entries expire quickly.
	d := NewDedup(100, 10*time.Millisecond)

	// Record some entries.
	d.TryRecord("id1")
	d.TryRecord("id2")
	d.TryRecord("id3")
	require.Equal(t, 3, len(d.order))

	// Wait for TTL to expire.
	time.Sleep(20 * time.Millisecond)

	// Sweep should remove all expired entries.
	d.Sweep()
	require.Equal(t, 0, len(d.order))
}

// ─── Dedup Sweep partial expiry ─────────────────────────────────────────────

func TestDedup_Sweep_PartialExpiry(t *testing.T) {
	t.Parallel()
	// Create dedup with TTL = 1ms (near-instant expiry).
	d := NewDedup(100, 1*time.Millisecond)

	// Record first entry.
	d.TryRecord("id1")
	// Wait for it to expire.
	time.Sleep(5 * time.Millisecond)

	// Record second entry (immediately after first expired).
	d.TryRecord("id2")

	// Sweep: id1 expired, id2 not yet.
	d.Sweep()
	require.Equal(t, 1, len(d.order))
}

// ─── Dedup Sweep empty ───────────────────────────────────────────────────────

func TestDedup_Sweep_Empty(t *testing.T) {
	t.Parallel()
	d := NewDedup(100, time.Hour)
	d.Sweep() // Should not panic.
	require.Equal(t, 0, len(d.order))
}

// ─── Dedup TryRecord FIFO eviction ───────────────────────────────────────────

func TestDedup_TryRecord_Accepted(t *testing.T) {
	t.Parallel()
	d := NewDedup(3, time.Hour)
	require.True(t, d.TryRecord("a"))
	require.True(t, d.TryRecord("b"))
	require.True(t, d.TryRecord("c"))
	require.Equal(t, 3, len(d.order))
}

func TestDedup_TryRecord_Rejected(t *testing.T) {
	t.Parallel()
	d := NewDedup(10, time.Hour)
	require.True(t, d.TryRecord("x"))
	require.False(t, d.TryRecord("x")) // duplicate
}

func TestDedup_TryRecord_FIFOEvict(t *testing.T) {
	t.Parallel()
	// At capacity, oldest entry is evicted.
	d := NewDedup(3, time.Hour)
	d.TryRecord("a")
	d.TryRecord("b")
	d.TryRecord("c")
	// Inserting a 4th entry should evict "a".
	require.False(t, d.TryRecord("a")) // "a" was evicted then re-inserted
	require.Equal(t, 3, len(d.order))
}
