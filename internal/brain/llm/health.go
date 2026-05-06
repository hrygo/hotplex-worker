package llm

import (
	"context"
	"sync"
	"time"
)

// HealthMonitor wraps a client with health monitoring capabilities.
type HealthMonitor struct {
	client interface {
		Chat(ctx context.Context, prompt string) (string, error)
		Analyze(ctx context.Context, prompt string, target any) error
		ChatStream(ctx context.Context, prompt string) (<-chan string, error)
		HealthCheck(ctx context.Context) HealthStatus
	}
	mu            sync.RWMutex
	lastStatus    HealthStatus
	lastCheckTime time.Time
	checkInterval time.Duration
}

// NewHealthMonitor creates a health monitor wrapper.
func NewHealthMonitor(client interface {
	Chat(ctx context.Context, prompt string) (string, error)
	Analyze(ctx context.Context, prompt string, target any) error
	ChatStream(ctx context.Context, prompt string) (<-chan string, error)
	HealthCheck(ctx context.Context) HealthStatus
}, checkInterval time.Duration) *HealthMonitor {
	return &HealthMonitor{
		client:        client,
		checkInterval: checkInterval,
	}
}

// Chat delegates to the underlying client.
func (h *HealthMonitor) Chat(ctx context.Context, prompt string) (string, error) {
	return h.client.Chat(ctx, prompt)
}

// Analyze delegates to the underlying client.
func (h *HealthMonitor) Analyze(ctx context.Context, prompt string, target any) error {
	return h.client.Analyze(ctx, prompt, target)
}

// ChatStream delegates to the underlying client.
func (h *HealthMonitor) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	return h.client.ChatStream(ctx, prompt)
}

// HealthCheck performs a health check with optional caching.
func (h *HealthMonitor) HealthCheck(ctx context.Context) HealthStatus {
	h.mu.RLock()
	// Return cached status if recent enough
	if time.Since(h.lastCheckTime) < h.checkInterval {
		status := h.lastStatus
		h.mu.RUnlock()
		return status
	}
	h.mu.RUnlock()

	// Perform fresh health check
	h.mu.Lock()
	defer h.mu.Unlock()

	status := h.client.HealthCheck(ctx)
	h.lastStatus = status
	h.lastCheckTime = time.Now()

	return status
}

// IsHealthy returns true if the last health check passed.
func (h *HealthMonitor) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastStatus.Healthy
}

// LastHealthCheck returns the time of the last health check.
func (h *HealthMonitor) LastHealthCheck() time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastCheckTime
}
