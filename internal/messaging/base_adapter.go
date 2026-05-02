package messaging

// BaseAdapter provides shared connection pool lifecycle management for platform adapters.
// Platform adapters embed BaseAdapter[ConnType] to reuse connPool initialization,
// connection lookup, and shutdown logic.
type BaseAdapter[C any] struct {
	PlatformAdapter
	ConnPool *ConnPool[C]
}

// InitConnPool creates the connection pool. The factory receives composite key "id#thread".
func (b *BaseAdapter[C]) InitConnPool(factory func(key string) C) {
	b.ConnPool = NewConnPool[C](factory)
}

// GetOrCreateConn returns the zero value of C when ConnPool is nil.
func (b *BaseAdapter[C]) GetOrCreateConn(id, thread string) C {
	if b.ConnPool == nil {
		var zero C
		return zero
	}
	return b.ConnPool.GetOrCreate(id + "#" + thread)
}

func (b *BaseAdapter[C]) DrainConns() []C {
	if b.ConnPool != nil {
		return b.ConnPool.ClearAndClose()
	}
	return nil
}

func (b *BaseAdapter[C]) DeleteConn(id, thread string) {
	if b.ConnPool != nil {
		b.ConnPool.Delete(id + "#" + thread)
	}
}
