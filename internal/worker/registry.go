package worker

import (
	"fmt"
	"sync"
)

// ─── Registry ───────────────────────────────────────────────────────────────

// Builder creates a Worker instance.
type Builder func() (Worker, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[WorkerType]Builder)
)

// Register registers a new worker builder for the given worker type.
// It panics if the builder is nil or if a type is registered twice.
func Register(t WorkerType, b Builder) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if b == nil {
		panic("worker: register builder is nil")
	}
	if _, dup := registry[t]; dup {
		panic("worker: register called twice for type " + string(t))
	}
	registry[t] = b
}

// NewWorker creates a new Worker instance for the specified worker type.
// It returns an error if the type is unknown or if the builder fails.
func NewWorker(t WorkerType) (Worker, error) {
	registryMu.RLock()
	b, ok := registry[t]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("worker: unknown type %q", t)
	}
	return b()
}

// RegisteredTypes returns a list of all registered worker types.
func RegisteredTypes() []WorkerType {
	registryMu.RLock()
	defer registryMu.RUnlock()
	var types []WorkerType
	for t := range registry {
		types = append(types, t)
	}
	return types
}
