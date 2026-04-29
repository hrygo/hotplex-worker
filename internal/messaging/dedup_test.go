package messaging

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewDedup_Defaults(t *testing.T) {
	t.Parallel()

	d := NewDedup(0, 0)
	require.Equal(t, 5000, d.maxEntries)
	require.Equal(t, 12*time.Hour, d.ttl)

	d2 := NewDedup(-1, -1)
	require.Equal(t, 5000, d2.maxEntries)
	require.Equal(t, 12*time.Hour, d2.ttl)
}

func TestDedup_Sweep_ExpiresEntries(t *testing.T) {
	t.Parallel()

	d := NewDedup(100, 10*time.Millisecond)
	d.TryRecord("id1")
	d.TryRecord("id2")
	d.TryRecord("id3")
	require.Equal(t, 3, len(d.order))

	time.Sleep(20 * time.Millisecond)
	d.Sweep()
	require.Equal(t, 0, len(d.order))
}

func TestDedup_Sweep_PartialExpiry(t *testing.T) {
	t.Parallel()

	d := NewDedup(100, 1*time.Millisecond)
	d.TryRecord("id1")
	time.Sleep(5 * time.Millisecond)
	d.TryRecord("id2")

	d.Sweep()
	require.Equal(t, 1, len(d.order))
}

func TestDedup_Sweep_Empty(t *testing.T) {
	t.Parallel()

	d := NewDedup(100, time.Hour)
	d.Sweep()
	require.Equal(t, 0, len(d.order))
}

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
	require.False(t, d.TryRecord("x"))
}

func TestDedup_TryRecord_FIFOEvict(t *testing.T) {
	t.Parallel()

	d := NewDedup(3, time.Hour)
	d.TryRecord("a")
	d.TryRecord("b")
	d.TryRecord("c")
	require.False(t, d.TryRecord("a"))
	require.Equal(t, 3, len(d.order))
}
