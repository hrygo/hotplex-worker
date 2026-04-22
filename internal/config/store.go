package config

import (
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// ConfigStore is the centralized, thread-safe configuration holder.
// All components read config via Load() (lock-free atomic read).
// Updates happen via Swap(), which also notifies all registered observers.
type ConfigStore struct {
	current atomic.Pointer[Config]
	log     *slog.Logger

	mu        sync.Mutex
	observers []Observer
}

// Observer is notified when configuration changes.
// Implementations must be safe for concurrent use — OnConfigReload
// may be called from any goroutine.
type Observer interface {
	OnConfigReload(prev, next *Config)
}

// ObserverFunc is a function adapter for Observer.
type ObserverFunc func(prev, next *Config)

func (f ObserverFunc) OnConfigReload(prev, next *Config) { f(prev, next) }

// NewConfigStore creates a ConfigStore initialized with the given config.
func NewConfigStore(initial *Config, log *slog.Logger) *ConfigStore {
	if log == nil {
		log = slog.Default()
	}
	s := &ConfigStore{log: log.With("component", "config_store")}
	s.current.Store(initial)
	return s
}

// Load returns the current config snapshot (lock-free).
// The returned *Config must be treated as immutable — never modify its fields.
func (s *ConfigStore) Load() *Config {
	return s.current.Load()
}

// Swap atomically replaces the current config and notifies all observers.
// Returns the previous config. Each observer is called in a separate
// goroutine with panic recovery so a buggy observer cannot block others.
func (s *ConfigStore) Swap(newCfg *Config) *Config {
	prev := s.current.Swap(newCfg)

	s.mu.Lock()
	observers := make([]Observer, len(s.observers))
	copy(observers, s.observers)
	s.mu.Unlock()

	for _, obs := range observers {
		obs := obs // capture for goroutine
		go func() {
			defer func() {
				if r := recover(); r != nil {
					s.log.Error("config: observer panic",
						"panic", r,
						"stack", string(debug.Stack()))
				}
			}()
			obs.OnConfigReload(prev, newCfg)
		}()
	}

	return prev
}

// Register adds an observer that will be notified on config changes.
// Observers are called in the order they were registered (each in its own goroutine).
// It is safe to call Register concurrently.
func (s *ConfigStore) Register(obs Observer) {
	s.mu.Lock()
	s.observers = append(s.observers, obs)
	s.mu.Unlock()
}

// RegisterFunc is a convenience wrapper for Register with a function.
func (s *ConfigStore) RegisterFunc(fn func(prev, next *Config)) {
	s.Register(ObserverFunc(fn))
}
