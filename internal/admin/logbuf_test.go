package admin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogRingBuffer_Add(t *testing.T) {
	r := newLogRing(3)

	r.Add("info", "msg1", "sess1")
	r.Add("warn", "msg2", "sess2")
	r.Add("error", "msg3", "sess3")

	require.Equal(t, 3, r.Total())
	entries := r.Recent(0)
	require.Len(t, entries, 3)
	require.Equal(t, "msg1", entries[0].Msg)
	require.Equal(t, "msg2", entries[1].Msg)
	require.Equal(t, "msg3", entries[2].Msg)
}

func TestLogRingBuffer_Wraparound(t *testing.T) {
	r := newLogRing(3)

	r.Add("info", "msg1", "")
	r.Add("info", "msg2", "")
	r.Add("info", "msg3", "")
	r.Add("info", "msg4", "") // overwrites msg1

	require.Equal(t, 4, r.Total())
	entries := r.Recent(0)
	require.Len(t, entries, 3)
	require.Equal(t, "msg2", entries[0].Msg)
	require.Equal(t, "msg3", entries[1].Msg)
	require.Equal(t, "msg4", entries[2].Msg)
}

func TestLogRingBuffer_RecentLimit(t *testing.T) {
	r := newLogRing(10)
	for i := 0; i < 5; i++ {
		r.Add("info", "msg", "")
	}

	entries := r.Recent(3)
	require.Len(t, entries, 3)
	require.Equal(t, 5, r.Total())
}

func TestLogRingBuffer_Empty(t *testing.T) {
	r := newLogRing(5)
	require.Equal(t, 0, r.Total())
	require.Nil(t, r.Recent(0))
}
