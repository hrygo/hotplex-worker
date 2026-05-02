package messaging

import "log/slog"

// BaseAdapter provides shared connection pool lifecycle management for platform adapters.
// Platform adapters embed BaseAdapter[ConnType] to reuse connPool initialization,
// connection lookup, and shutdown logic.
type BaseAdapter[C any] struct {
	PlatformAdapter
	ConnPool *ConnPool[C]
}

// NewBaseAdapter creates a BaseAdapter with the given logger.
func NewBaseAdapter[C any](log *slog.Logger) *BaseAdapter[C] {
	return &BaseAdapter[C]{
		PlatformAdapter: PlatformAdapter{Log: log},
	}
}

// InitConnPool creates the connection pool with the given factory function.
// The factory receives a composite key "id#thread" and should return a new connection.
func (b *BaseAdapter[C]) InitConnPool(factory func(key string) C) {
	b.ConnPool = NewConnPool[C](factory)
}

// GetOrCreateConn returns an existing connection or creates a new one using
// the composite key format "id#thread". Returns zero value if pool is nil.
func (b *BaseAdapter[C]) GetOrCreateConn(id, thread string) C {
	if b.ConnPool == nil {
		var zero C
		return zero
	}
	return b.ConnPool.GetOrCreate(id + "#" + thread)
}

// DrainConns drains all connections from the pool and returns them for cleanup.
func (b *BaseAdapter[C]) DrainConns() []C {
	if b.ConnPool != nil {
		return b.ConnPool.ClearAndClose()
	}
	return nil
}

// DeleteConn removes a connection from the pool by composite key.
func (b *BaseAdapter[C]) DeleteConn(id, thread string) {
	if b.ConnPool != nil {
		b.ConnPool.Delete(id + "#" + thread)
	}
}
