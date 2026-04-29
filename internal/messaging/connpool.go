package messaging

import (
	"sync"
	"sync/atomic"
)

// ConnPool is a generic connection pool that manages platform connections by key.
// It provides thread-safe get-or-create semantics with closed-state protection.
type ConnPool[C any] struct {
	mu      sync.RWMutex
	conns   map[string]C
	closed  atomic.Bool
	factory func(key string) C
}

// NewConnPool creates a connection pool with the given factory function.
func NewConnPool[C any](factory func(key string) C) *ConnPool[C] {
	return &ConnPool[C]{
		conns:   make(map[string]C),
		factory: factory,
	}
}

// GetOrCreate returns an existing connection or creates a new one.
// Returns the zero value of C if the pool is closed.
func (p *ConnPool[C]) GetOrCreate(key string) C {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed.Load() {
		var zero C
		return zero
	}
	if c, ok := p.conns[key]; ok {
		return c
	}
	c := p.factory(key)
	p.conns[key] = c
	return c
}

// Get returns a connection by key, or the zero value if not found.
func (p *ConnPool[C]) Get(key string) C {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.conns[key]
}

// Range calls f for each connection in the pool. Locks for reading.
func (p *ConnPool[C]) Range(f func(key string, conn C)) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for k, c := range p.conns {
		f(k, c)
	}
}

// Len returns the number of connections in the pool.
func (p *ConnPool[C]) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.conns)
}

// ClearAndClose drains all connections, calls closer on each, and marks the pool as closed.
// Returns the connections that were collected for callers that need to perform
// cleanup outside the lock.
func (p *ConnPool[C]) ClearAndClose() []C {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed.Store(true)
	conns := make([]C, 0, len(p.conns))
	for _, c := range p.conns {
		conns = append(conns, c)
	}
	p.conns = nil
	return conns
}

// Delete removes a connection by key. No-op if the pool is closed or key not found.
func (p *ConnPool[C]) Delete(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.conns, key)
}

// IsClosed reports whether the pool has been closed.
func (p *ConnPool[C]) IsClosed() bool {
	return p.closed.Load()
}
