package messaging

import (
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// testConn is a minimal connection type for BaseAdapter tests.
type testConn struct{ key string }

func newTestBaseAdapter(t *testing.T) *BaseAdapter[*testConn] {
	t.Helper()
	return &BaseAdapter[*testConn]{
		PlatformAdapter: PlatformAdapter{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
	}
}

func TestBaseAdapter_Construct(t *testing.T) {
	t.Parallel()

	b := newTestBaseAdapter(t)
	require.NotNil(t, b)
	require.NotNil(t, b.Log)
	require.Nil(t, b.ConnPool, "ConnPool should be nil before InitConnPool")
}

func TestBaseAdapter_InitConnPool(t *testing.T) {
	t.Parallel()

	b := newTestBaseAdapter(t)
	var created atomic.Int32
	b.InitConnPool(func(key string) *testConn {
		created.Add(1)
		return &testConn{key: key}
	})
	require.NotNil(t, b.ConnPool)
	require.Equal(t, int32(0), created.Load(), "factory should not be called during init")
}

func TestBaseAdapter_GetOrCreateConn(t *testing.T) {
	t.Parallel()

	b := newTestBaseAdapter(t)
	b.InitConnPool(func(key string) *testConn {
		return &testConn{key: key}
	})

	c1 := b.GetOrCreateConn("ch1", "th1")
	require.NotNil(t, c1)
	require.Equal(t, "ch1#th1", c1.key)

	c2 := b.GetOrCreateConn("ch1", "th1")
	require.Same(t, c1, c2, "same key should return same conn")

	c3 := b.GetOrCreateConn("ch2", "")
	require.NotNil(t, c3)
	require.Equal(t, "ch2#", c3.key)
	require.Equal(t, 2, b.ConnPool.Len())
}

func TestBaseAdapter_DrainConns(t *testing.T) {
	t.Parallel()

	b := newTestBaseAdapter(t)
	b.InitConnPool(func(key string) *testConn {
		return &testConn{key: key}
	})

	_ = b.GetOrCreateConn("ch1", "th1")
	_ = b.GetOrCreateConn("ch2", "th2")
	require.Equal(t, 2, b.ConnPool.Len())

	conns := b.DrainConns()
	require.Len(t, conns, 2)
	require.True(t, b.ConnPool.IsClosed())
	require.Equal(t, 0, b.ConnPool.Len())
}

func TestBaseAdapter_DrainConns_NilPool(t *testing.T) {
	t.Parallel()

	b := newTestBaseAdapter(t)
	conns := b.DrainConns()
	require.Nil(t, conns)
}

func TestBaseAdapter_DeleteConn(t *testing.T) {
	t.Parallel()

	b := newTestBaseAdapter(t)
	b.InitConnPool(func(key string) *testConn {
		return &testConn{key: key}
	})

	_ = b.GetOrCreateConn("ch1", "th1")
	require.Equal(t, 1, b.ConnPool.Len())

	b.DeleteConn("ch1", "th1")
	require.Equal(t, 0, b.ConnPool.Len())
	require.Nil(t, b.ConnPool.Get("ch1#th1"))
}

func TestBaseAdapter_DeleteConn_NilPool(t *testing.T) {
	t.Parallel()

	b := newTestBaseAdapter(t)
	require.NotPanics(t, func() { b.DeleteConn("a", "b") })
}

func TestBaseAdapter_GetOrCreateConn_NilPool(t *testing.T) {
	t.Parallel()

	b := newTestBaseAdapter(t)
	result := b.GetOrCreateConn("a", "b")
	require.Nil(t, result)
}

func TestBaseAdapter_PromotedPlatformAdapter(t *testing.T) {
	t.Parallel()

	b := newTestBaseAdapter(t)
	require.False(t, b.IsClosed())
	b.MarkClosed()
	require.True(t, b.IsClosed())
	require.True(t, b.StartGuard())
	require.False(t, b.StartGuard())
}
