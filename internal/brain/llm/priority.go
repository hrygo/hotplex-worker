package llm

import (
	"container/heap"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.uber.org/atomic"
)

// Priority defines request priority levels.
type Priority int

const (
	// PriorityHigh - High priority (critical requests).
	PriorityHigh Priority = 1
	// PriorityMedium - Medium priority (normal requests).
	PriorityMedium Priority = 2
	// PriorityLow - Low priority (can be dropped).
	PriorityLow Priority = 3
)

// String returns string representation of priority.
func (p Priority) String() string {
	switch p {
	case PriorityHigh:
		return "high"
	case PriorityMedium:
		return "medium"
	case PriorityLow:
		return "low"
	default:
		return "unknown"
	}
}

// PriorityRequest represents a request in the priority queue.
type PriorityRequest struct {
	index     int
	ID        string
	Priority  Priority
	Context   context.Context
	Execute   func() error
	CreatedAt time.Time
	Deadline  time.Time
	MaxWait   time.Duration
}

// PriorityQueue implements a priority queue for requests.
type PriorityQueue struct {
	items []*PriorityRequest
}

// Len returns the number of items in the queue.
func (pq *PriorityQueue) Len() int {
	return len(pq.items)
}

// Less compares two items (lower priority value = higher priority).
func (pq *PriorityQueue) Less(i, j int) bool {
	// First compare by priority
	if pq.items[i].Priority != pq.items[j].Priority {
		return pq.items[i].Priority < pq.items[j].Priority
	}
	// Then by creation time (FIFO within same priority)
	return pq.items[i].CreatedAt.Before(pq.items[j].CreatedAt)
}

// Swap swaps two items.
func (pq *PriorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].index = i
	pq.items[j].index = j
}

// Push adds an item to the queue.
func (pq *PriorityQueue) Push(x interface{}) {
	item, ok := x.(*PriorityRequest)
	if !ok {
		return
	}
	item.index = len(pq.items)
	pq.items = append(pq.items, item)
}

// Pop removes and returns the highest priority item.
func (pq *PriorityQueue) Pop() interface{} {
	old := pq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	pq.items = old[0 : n-1]
	return item
}

// Peek returns the highest priority item without removing it.
func (pq *PriorityQueue) Peek() *PriorityRequest {
	if len(pq.items) == 0 {
		return nil
	}
	return pq.items[0]
}

// PriorityConfig holds configuration for priority queue.
type PriorityConfig struct {
	// MaxQueueSize is the maximum queue size.
	MaxQueueSize int
	// EnableLowPriorityDrop enables dropping low priority requests when queue is full.
	EnableLowPriorityDrop bool
	// HighPriorityReserve reserves queue slots for high priority.
	HighPriorityReserve int
	// MaxWaitTime is the maximum wait time for requests.
	MaxWaitTime time.Duration
	// Logger for queue events.
	Logger *slog.Logger
}

// DefaultPriorityConfig returns sensible defaults.
func DefaultPriorityConfig() PriorityConfig {
	return PriorityConfig{
		MaxQueueSize:          1000,
		EnableLowPriorityDrop: true,
		HighPriorityReserve:   100,
		MaxWaitTime:           5 * time.Minute,
	}
}

// PriorityScheduler manages priority-based request scheduling.
type PriorityScheduler struct {
	config    PriorityConfig
	queue     *PriorityQueue
	queueSize *atomic.Int32
	dropped   *atomic.Int64
	processed *atomic.Int64

	// Per-priority stats
	highProcessed   *atomic.Int64
	mediumProcessed *atomic.Int64
	lowProcessed    *atomic.Int64
	highDropped     *atomic.Int64
	mediumDropped   *atomic.Int64
	lowDropped      *atomic.Int64

	// Channel-based notification for work availability.
	// Buffered so Signal() never blocks.
	workCh chan struct{}

	// Shutdown
	shutdown   *atomic.Bool
	shutdownCh chan struct{}

	mu sync.RWMutex
}

// PriorityStats holds scheduler statistics.
type PriorityStats struct {
	QueueSize       int32
	MaxQueueSize    int
	Dropped         int64
	Processed       int64
	HighProcessed   int64
	MediumProcessed int64
	LowProcessed    int64
	HighDropped     int64
	MediumDropped   int64
	LowDropped      int64
	IsShutdown      bool
}

// NewPriorityScheduler creates a new priority scheduler.
func NewPriorityScheduler(config PriorityConfig) *PriorityScheduler {
	ps := &PriorityScheduler{
		config:          config,
		queue:           &PriorityQueue{},
		queueSize:       atomic.NewInt32(0),
		dropped:         atomic.NewInt64(0),
		processed:       atomic.NewInt64(0),
		highProcessed:   atomic.NewInt64(0),
		mediumProcessed: atomic.NewInt64(0),
		lowProcessed:    atomic.NewInt64(0),
		highDropped:     atomic.NewInt64(0),
		mediumDropped:   atomic.NewInt64(0),
		lowDropped:      atomic.NewInt64(0),
		shutdown:        atomic.NewBool(false),
		shutdownCh:      make(chan struct{}),
	}

	ps.workCh = make(chan struct{}, 1)
	heap.Init(ps.queue)

	// Start cleanup goroutine
	go ps.startCleanup()

	return ps
}

// startCleanup periodically removes expired requests.
func (ps *PriorityScheduler) startCleanup() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ps.cleanupExpired()
		case <-ps.shutdownCh:
			return
		}
	}
}

// cleanupExpired removes expired requests from the queue.
func (ps *PriorityScheduler) cleanupExpired() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	now := time.Now()
	var expired []*PriorityRequest

	// Collect expired items
	for ps.queue.Len() > 0 {
		item := ps.queue.Peek()
		if item == nil {
			break
		}

		// Check deadline
		if !item.Deadline.IsZero() && now.After(item.Deadline) {
			heap.Pop(ps.queue)
			ps.queueSize.Dec()
			expired = append(expired, item)
			continue
		}

		// Check max wait
		if item.MaxWait > 0 && now.Sub(item.CreatedAt) > item.MaxWait {
			heap.Pop(ps.queue)
			ps.queueSize.Dec()
			expired = append(expired, item)
			continue
		}

		break // Found non-expired item
	}

	// Record dropped stats
	for _, item := range expired {
		ps.recordDrop(item.Priority)
		if ps.config.Logger != nil {
			ps.config.Logger.Debug("request expired and dropped",
				"id", item.ID,
				"priority", item.Priority)
		}
	}
}

// Enqueue adds a request to the priority queue.
func (ps *PriorityScheduler) Enqueue(ctx context.Context, id string, priority Priority, execute func() error, maxWait time.Duration) error {
	if ps.shutdown.Load() {
		return fmt.Errorf("scheduler is shutdown")
	}

	// Create request
	request := &PriorityRequest{
		ID:        id,
		Priority:  priority,
		Context:   ctx,
		Execute:   execute,
		CreatedAt: time.Now(),
		MaxWait:   maxWait,
	}

	if maxWait > 0 {
		request.Deadline = request.CreatedAt.Add(maxWait)
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Check queue size
	currentSize := ps.queueSize.Load()

	// Check if we need to drop low priority requests
	if int(currentSize) >= ps.config.MaxQueueSize {
		if ps.config.EnableLowPriorityDrop && priority == PriorityLow {
			ps.recordDrop(priority)
			ps.lowDropped.Inc()

			if ps.config.Logger != nil {
				ps.config.Logger.Warn("low priority request dropped (queue full)",
					"id", id)
			}
			return fmt.Errorf("queue full: low priority request dropped")
		}

		// Check if high priority can use reserved slots
		if priority == PriorityHigh && int(currentSize) < ps.config.MaxQueueSize+ps.config.HighPriorityReserve {
			// Allow high priority to use reserved slots
		} else {
			return fmt.Errorf("queue full: max size %d reached", ps.config.MaxQueueSize)
		}
	}

	// Add to queue
	heap.Push(ps.queue, request)
	ps.queueSize.Inc()
	// Non-blocking signal to wake up a waiting Dequeue
	select {
	case ps.workCh <- struct{}{}:
	default:
	}

	if ps.config.Logger != nil {
		ps.config.Logger.Debug("request enqueued",
			"id", id,
			"priority", priority,
			"queueSize", ps.queueSize.Load())
	}

	return nil
}

// Dequeue removes and returns the highest priority request.
// It uses a channel-based wait that properly integrates with context cancellation.
func (ps *PriorityScheduler) Dequeue(ctx context.Context) (*PriorityRequest, error) {
	for {
		ps.mu.Lock()
		if ps.shutdown.Load() {
			ps.mu.Unlock()
			return nil, fmt.Errorf("scheduler is shutdown")
		}
		if ps.queue.Len() > 0 {
			req, ok := heap.Pop(ps.queue).(*PriorityRequest)
			if !ok {
				ps.mu.Unlock()
				return nil, fmt.Errorf("invalid priority request in queue")
			}
			ps.queueSize.Dec()
			ps.processed.Inc()
			switch req.Priority {
			case PriorityHigh:
				ps.highProcessed.Inc()
			case PriorityMedium:
				ps.mediumProcessed.Inc()
			case PriorityLow:
				ps.lowProcessed.Inc()
			}
			ps.mu.Unlock()
			return req, nil
		}
		ps.mu.Unlock()

		// Wait for work or context cancellation without holding the lock
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ps.workCh:
			// Work may be available; loop will re-check queue
		}
	}
}

// TryDequeue tries to dequeue without blocking.
func (ps *PriorityScheduler) TryDequeue() *PriorityRequest {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.queue.Len() == 0 {
		return nil
	}

	request, ok := heap.Pop(ps.queue).(*PriorityRequest)
	if !ok {
		return nil
	}
	ps.queueSize.Dec()
	ps.processed.Inc()

	switch request.Priority {
	case PriorityHigh:
		ps.highProcessed.Inc()
	case PriorityMedium:
		ps.mediumProcessed.Inc()
	case PriorityLow:
		ps.lowProcessed.Inc()
	}

	return request
}

// recordDrop records a dropped request.
func (ps *PriorityScheduler) recordDrop(priority Priority) {
	ps.dropped.Inc()
	switch priority {
	case PriorityHigh:
		ps.highDropped.Inc()
	case PriorityMedium:
		ps.mediumDropped.Inc()
	case PriorityLow:
		ps.lowDropped.Inc()
	}
}

// GetStats returns scheduler statistics.
func (ps *PriorityScheduler) GetStats() PriorityStats {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	return PriorityStats{
		QueueSize:       ps.queueSize.Load(),
		MaxQueueSize:    ps.config.MaxQueueSize,
		Dropped:         ps.dropped.Load(),
		Processed:       ps.processed.Load(),
		HighProcessed:   ps.highProcessed.Load(),
		MediumProcessed: ps.mediumProcessed.Load(),
		LowProcessed:    ps.lowProcessed.Load(),
		HighDropped:     ps.highDropped.Load(),
		MediumDropped:   ps.mediumDropped.Load(),
		LowDropped:      ps.lowDropped.Load(),
		IsShutdown:      ps.shutdown.Load(),
	}
}

// Size returns the current queue size.
func (ps *PriorityScheduler) Size() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.queue.Len()
}

// Shutdown gracefully shuts down the scheduler.
func (ps *PriorityScheduler) Shutdown() {
	if ps.shutdown.Swap(true) {
		return // Already shutdown
	}

	close(ps.shutdownCh)
	// Non-blocking send to wake up pending Dequeue (best-effort)
	select {
	case ps.workCh <- struct{}{}:
	default:
	}

	if ps.config.Logger != nil {
		ps.config.Logger.Info("priority scheduler shutdown")
	}
}

// Clear clears all items from the queue.
func (ps *PriorityScheduler) Clear() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	dropped := ps.queue.Len()
	ps.queue = &PriorityQueue{}
	heap.Init(ps.queue)
	ps.queueSize.Store(0)
	ps.dropped.Add(int64(dropped))

	if ps.config.Logger != nil {
		ps.config.Logger.Info("priority queue cleared",
			"dropped", dropped)
	}
}

// PriorityClient wraps a scheduler with execution capabilities.
type PriorityClient struct {
	scheduler *PriorityScheduler
	timeout   time.Duration
	logger    *slog.Logger
}

// NewPriorityClient creates a new priority client.
func NewPriorityClient(scheduler *PriorityScheduler, timeout time.Duration, logger *slog.Logger) *PriorityClient {
	return &PriorityClient{
		scheduler: scheduler,
		timeout:   timeout,
		logger:    logger,
	}
}

// Submit submits a request for execution.
func (pc *PriorityClient) Submit(ctx context.Context, id string, priority Priority, execute func() error) error {
	maxWait := pc.timeout
	if maxWait == 0 {
		maxWait = 5 * time.Minute
	}

	return pc.scheduler.Enqueue(ctx, id, priority, execute, maxWait)
}

// SubmitWithResult submits a request and returns the result.
func (pc *PriorityClient) SubmitWithResult(ctx context.Context, id string, priority Priority, execute func() error) (<-chan error, error) {
	resultCh := make(chan error, 1)

	err := pc.scheduler.Enqueue(ctx, id, priority, func() error {
		defer close(resultCh)
		return execute()
	}, pc.timeout)

	if err != nil {
		close(resultCh)
		return nil, err
	}

	return resultCh, nil
}

// ProcessNext processes the next request in the queue.
func (pc *PriorityClient) ProcessNext(ctx context.Context) error {
	request, err := pc.scheduler.Dequeue(ctx)
	if err != nil {
		return err
	}

	if request == nil {
		return fmt.Errorf("no requests in queue")
	}

	// Check if request context is already done
	select {
	case <-request.Context.Done():
		return request.Context.Err()
	default:
	}

	// Execute the request
	return request.Execute()
}

// ProcessAll processes all requests in the queue.
func (pc *PriorityClient) ProcessAll(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		request := pc.scheduler.TryDequeue()
		if request == nil {
			return nil // Queue empty
		}

		// Check if request context is done
		select {
		case <-request.Context.Done():
			continue // Skip expired request
		default:
		}

		// Execute
		if err := request.Execute(); err != nil && pc.logger != nil {
			pc.logger.Error("request execution failed",
				"id", request.ID,
				"error", err)
		}
	}
}
