package messaging

import (
	"sync"
	"sync/atomic"
)

type ConnPool[C any] struct {
	mu      sync.RWMutex
	conns   map[string]C
	closed  atomic.Bool
	factory func(key string) C
}

func NewConnPool[C any](factory func(key string) C) *ConnPool[C] {
	return &ConnPool[C]{
		conns:   make(map[string]C),
		factory: factory,
	}
}

// Fast-path: check closed before acquiring lock to avoid contention in hot paths.
func (p *ConnPool[C]) GetOrCreate(key string) C {
	if p.closed.Load() {
		var zero C
		return zero
	}
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

func (p *ConnPool[C]) Get(key string) C {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.conns[key]
}

func (p *ConnPool[C]) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.conns)
}

// ClearAndClose drains all connections and marks the pool as closed.
// Returns collected connections for cleanup outside the lock.
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

func (p *ConnPool[C]) Delete(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.conns, key)
}

func (p *ConnPool[C]) IsClosed() bool {
	return p.closed.Load()
}
