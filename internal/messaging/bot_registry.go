package messaging

import (
	"sync"
	"time"
)

// BotStatus represents the lifecycle state of a bot adapter.
type BotStatus string

const (
	BotStatusStarting BotStatus = "starting"
	BotStatusRunning  BotStatus = "running"
	BotStatusStopped  BotStatus = "stopped"
	BotStatusError    BotStatus = "error"
)

// BotEntry holds runtime state for an active bot.
type BotEntry struct {
	Name        string
	Platform    PlatformType
	BotID       string
	Status      BotStatus
	Adapter     PlatformAdapterInterface
	Bridge      *Bridge
	Soul        string
	WorkerType  string
	ConnectedAt time.Time
}

// BotRegistry tracks active bot adapters and bridges.
type BotRegistry struct {
	mu      sync.RWMutex
	entries map[string]*BotEntry // key: "platform/name"
}

// newBotRegistry creates an empty registry.
func newBotRegistry() *BotRegistry {
	return &BotRegistry{entries: make(map[string]*BotEntry)}
}

func botKey(platform PlatformType, name string) string {
	return string(platform) + "/" + name
}

// Register adds a bot entry to the registry.
func (r *BotRegistry) Register(e *BotEntry) {
	r.mu.Lock()
	r.entries[botKey(e.Platform, e.Name)] = e
	r.mu.Unlock()
}

// Unregister removes a bot entry from the registry.
func (r *BotRegistry) Unregister(platform PlatformType, name string) {
	r.mu.Lock()
	delete(r.entries, botKey(platform, name))
	r.mu.Unlock()
}

// UpdateStatus updates the status and BotID of a registered bot.
func (r *BotRegistry) UpdateStatus(platform PlatformType, name string, status BotStatus, botID string) {
	r.mu.Lock()
	if e, ok := r.entries[botKey(platform, name)]; ok {
		e.Status = status
		if botID != "" {
			e.BotID = botID
		}
	}
	r.mu.Unlock()
}

// Get returns a single bot entry by platform and name.
func (r *BotRegistry) Get(platform PlatformType, name string) (*BotEntry, bool) {
	r.mu.RLock()
	e, ok := r.entries[botKey(platform, name)]
	r.mu.RUnlock()
	return e, ok
}

// GetByName returns a bot entry by name (globally unique across platforms).
func (r *BotRegistry) GetByName(name string) (*BotEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.Name == name {
			return e, true
		}
	}
	return nil, false
}

// ListAll returns all registered bot entries.
func (r *BotRegistry) ListAll() []*BotEntry {
	r.mu.RLock()
	result := make([]*BotEntry, 0, len(r.entries))
	for _, e := range r.entries {
		result = append(result, e)
	}
	r.mu.RUnlock()
	return result
}

// ListByPlatform returns all bot entries for a given platform.
func (r *BotRegistry) ListByPlatform(platform PlatformType) []*BotEntry {
	r.mu.RLock()
	var result []*BotEntry
	for _, e := range r.entries {
		if e.Platform == platform {
			result = append(result, e)
		}
	}
	r.mu.RUnlock()
	return result
}

// UnregisterAll removes all bot entries. Used during gateway shutdown.
func (r *BotRegistry) UnregisterAll() {
	r.mu.Lock()
	clear(r.entries)
	r.mu.Unlock()
}

// Global registry instance.
var defaultRegistry = newBotRegistry()

// DefaultBotRegistry returns the global bot registry.
func DefaultBotRegistry() *BotRegistry {
	return defaultRegistry
}
