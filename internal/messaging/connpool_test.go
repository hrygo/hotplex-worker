package messaging

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnPool_GetOrCreate(t *testing.T) {
	t.Parallel()

	var created atomic.Int32
	p := NewConnPool(func(key string) int {
		created.Add(1)
		return len(key)
	})

	// First access creates.
	v := p.GetOrCreate("abc")
	require.Equal(t, 3, v)
	require.Equal(t, int32(1), created.Load())

	// Second access returns cached value without calling factory.
	v2 := p.GetOrCreate("abc")
	require.Equal(t, 3, v2)
	require.Equal(t, int32(1), created.Load())

	// Different key creates a new entry.
	v3 := p.GetOrCreate("xy")
	require.Equal(t, 2, v3)
	require.Equal(t, int32(2), created.Load())
}

func TestConnPool_Get(t *testing.T) {
	t.Parallel()

	p := NewConnPool(func(key string) string { return key })
	p.GetOrCreate("k1")

	require.Equal(t, "k1", p.Get("k1"))
	var zero string
	require.Equal(t, zero, p.Get("missing"))
}

func TestConnPool_Len(t *testing.T) {
	t.Parallel()

	p := NewConnPool(func(key string) any { return nil })
	require.Equal(t, 0, p.Len())

	p.GetOrCreate("a")
	p.GetOrCreate("b")
	require.Equal(t, 2, p.Len())
}

func TestConnPool_Delete(t *testing.T) {
	t.Parallel()

	p := NewConnPool(func(key string) any { return key })
	p.GetOrCreate("x")
	require.Equal(t, 1, p.Len())

	p.Delete("x")
	require.Equal(t, 0, p.Len())
}

func TestConnPool_ClearAndClose(t *testing.T) {
	t.Parallel()

	p := NewConnPool(func(key string) int { return len(key) })
	p.GetOrCreate("a")
	p.GetOrCreate("bb")

	conns := p.ClearAndClose()
	require.Len(t, conns, 2)
	require.True(t, p.IsClosed())

	// After close, GetOrCreate returns zero value.
	var zero int
	require.Equal(t, zero, p.GetOrCreate("new"))
}

func TestConnPool_IsClosed(t *testing.T) {
	t.Parallel()

	p := NewConnPool(func(key string) any { return nil })
	require.False(t, p.IsClosed())
	p.ClearAndClose()
	require.True(t, p.IsClosed())
}
