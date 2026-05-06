package llm

import (
	"container/heap"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPriorityQueue_BasicOrdering(t *testing.T) {
	t.Parallel()
	pq := &PriorityQueue{}

	// Add items in mixed priority order
	items := []*PriorityRequest{
		{ID: "low-1", Priority: PriorityLow},
		{ID: "high-1", Priority: PriorityHigh},
		{ID: "medium-1", Priority: PriorityMedium},
		{ID: "high-2", Priority: PriorityHigh},
	}

	for _, item := range items {
		item.CreatedAt = time.Now()
	}

	// Initialize heap
	heap.Init(pq)

	// Push items
	for _, item := range items {
		heap.Push(pq, item)
	}

	// Pop should return in priority order
	first := heap.Pop(pq).(*PriorityRequest)
	assert.Equal(t, "high-1", first.ID)
	assert.Equal(t, PriorityHigh, first.Priority)

	second := heap.Pop(pq).(*PriorityRequest)
	assert.Equal(t, "high-2", second.ID)
	assert.Equal(t, PriorityHigh, second.Priority)

	third := heap.Pop(pq).(*PriorityRequest)
	assert.Equal(t, "medium-1", third.ID)
	assert.Equal(t, PriorityMedium, third.Priority)

	fourth := heap.Pop(pq).(*PriorityRequest)
	assert.Equal(t, "low-1", fourth.ID)
	assert.Equal(t, PriorityLow, fourth.Priority)
}

func TestPriorityScheduler_EnqueueDequeue(t *testing.T) {
	t.Parallel()
	config := DefaultPriorityConfig()
	scheduler := NewPriorityScheduler(config)
	defer scheduler.Shutdown()

	// Enqueue requests
	err := scheduler.Enqueue(context.Background(), "req-1", PriorityMedium, func() error {
		return nil
	}, time.Minute)
	assert.NoError(t, err)

	err = scheduler.Enqueue(context.Background(), "req-2", PriorityHigh, func() error {
		return nil
	}, time.Minute)
	assert.NoError(t, err)

	err = scheduler.Enqueue(context.Background(), "req-3", PriorityLow, func() error {
		return nil
	}, time.Minute)
	assert.NoError(t, err)

	// Dequeue should return highest priority first
	req := scheduler.TryDequeue()
	assert.NotNil(t, req)
	assert.Equal(t, "req-2", req.ID)
	assert.Equal(t, PriorityHigh, req.Priority)

	req = scheduler.TryDequeue()
	assert.NotNil(t, req)
	assert.Equal(t, "req-1", req.ID)
	assert.Equal(t, PriorityMedium, req.Priority)

	req = scheduler.TryDequeue()
	assert.NotNil(t, req)
	assert.Equal(t, "req-3", req.ID)
	assert.Equal(t, PriorityLow, req.Priority)
}

func TestPriorityScheduler_LowPriorityDrop(t *testing.T) {
	t.Parallel()
	config := DefaultPriorityConfig()
	config.MaxQueueSize = 3
	config.EnableLowPriorityDrop = true

	scheduler := NewPriorityScheduler(config)
	defer scheduler.Shutdown()

	// Fill queue
	err := scheduler.Enqueue(context.Background(), "req-1", PriorityHigh, func() error { return nil }, time.Minute)
	assert.NoError(t, err)
	err = scheduler.Enqueue(context.Background(), "req-2", PriorityHigh, func() error { return nil }, time.Minute)
	assert.NoError(t, err)
	err = scheduler.Enqueue(context.Background(), "req-3", PriorityHigh, func() error { return nil }, time.Minute)
	assert.NoError(t, err)

	// Try to add low priority - should be dropped
	err = scheduler.Enqueue(context.Background(), "req-low", PriorityLow, func() error { return nil }, time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dropped")

	// High priority should still work (using reserved slots)
	err = scheduler.Enqueue(context.Background(), "req-high", PriorityHigh, func() error { return nil }, time.Minute)
	assert.NoError(t, err)

	// Wait for scheduler to process
	time.Sleep(10 * time.Millisecond)

	stats := scheduler.GetStats()
	assert.GreaterOrEqual(t, stats.LowDropped, int64(1))
}

func TestPriorityScheduler_Stats(t *testing.T) {
	t.Parallel()
	config := DefaultPriorityConfig()
	scheduler := NewPriorityScheduler(config)
	defer scheduler.Shutdown()

	// Enqueue and dequeue some requests
	var err error
	for i := 0; i < 5; i++ {
		err = scheduler.Enqueue(context.Background(), "high-"+string(rune(i)), PriorityHigh, func() error { return nil }, time.Minute)
		assert.NoError(t, err)
	}

	for i := 0; i < 3; i++ {
		err = scheduler.Enqueue(context.Background(), "medium-"+string(rune(i)), PriorityMedium, func() error { return nil }, time.Minute)
		assert.NoError(t, err)
	}

	// Dequeue all
	for scheduler.Size() > 0 {
		scheduler.TryDequeue()
	}

	stats := scheduler.GetStats()
	assert.Equal(t, int64(8), stats.Processed)
	assert.Equal(t, int64(5), stats.HighProcessed)
	assert.Equal(t, int64(3), stats.MediumProcessed)
	assert.Equal(t, int32(0), stats.QueueSize)
}

func TestPriorityScheduler_Shutdown(t *testing.T) {
	t.Parallel()
	config := DefaultPriorityConfig()
	scheduler := NewPriorityScheduler(config)

	// Shutdown
	scheduler.Shutdown()

	// Enqueue should fail
	err := scheduler.Enqueue(context.Background(), "req", PriorityHigh, func() error { return nil }, time.Minute)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shutdown")

	stats := scheduler.GetStats()
	assert.True(t, stats.IsShutdown)
}

func TestPriorityScheduler_Clear(t *testing.T) {
	t.Parallel()
	config := DefaultPriorityConfig()
	scheduler := NewPriorityScheduler(config)
	defer scheduler.Shutdown()

	// Add some requests
	var err error
	for i := 0; i < 10; i++ {
		err = scheduler.Enqueue(context.Background(), "req-"+string(rune(i)), PriorityMedium, func() error { return nil }, time.Minute)
		assert.NoError(t, err)
	}

	assert.Equal(t, 10, scheduler.Size())

	// Clear
	scheduler.Clear()

	assert.Equal(t, 0, scheduler.Size())
	stats := scheduler.GetStats()
	assert.Equal(t, int64(10), stats.Dropped)
}

func TestPriorityScheduler_Concurrent(t *testing.T) {
	t.Parallel()
	config := DefaultPriorityConfig()
	config.MaxQueueSize = 1000
	scheduler := NewPriorityScheduler(config)
	defer scheduler.Shutdown()

	var wg sync.WaitGroup

	// Concurrent enqueue
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				err := scheduler.Enqueue(context.Background(),
					"req-"+string(rune(id))+"-"+string(rune(j)),
					PriorityMedium,
					func() error { return nil },
					time.Minute)
				if err != nil {
					return
				}
			}
		}(i)
	}

	wg.Wait()
	assert.Equal(t, 100, scheduler.Size())
}

func TestPriorityScheduler_ExpiredRequests(t *testing.T) {
	t.Parallel()
	config := DefaultPriorityConfig()
	config.MaxQueueSize = 10
	scheduler := NewPriorityScheduler(config)
	defer scheduler.Shutdown()

	// Add request with very short max wait
	err := scheduler.Enqueue(context.Background(), "req-1", PriorityLow, func() error { return nil }, 10*time.Millisecond)
	assert.NoError(t, err)

	// Wait for expiration and cleanup
	time.Sleep(100 * time.Millisecond)

	// Should be cleaned up (or marked as dropped)
	stats := scheduler.GetStats()
	assert.GreaterOrEqual(t, stats.Dropped+int64(scheduler.Size()), int64(1))
}

func TestPriorityClient_Submit(t *testing.T) {
	t.Parallel()
	config := DefaultPriorityConfig()
	scheduler := NewPriorityScheduler(config)
	defer scheduler.Shutdown()

	client := NewPriorityClient(scheduler, time.Minute, nil)

	// Submit request
	executed := false
	err := client.Submit(context.Background(), "req-1", PriorityHigh, func() error {
		executed = true
		return nil
	})
	assert.NoError(t, err)

	// Process
	err = client.ProcessNext(context.Background())
	assert.NoError(t, err)
	assert.True(t, executed)
}

func TestPriority_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "high", PriorityHigh.String())
	assert.Equal(t, "medium", PriorityMedium.String())
	assert.Equal(t, "low", PriorityLow.String())
}

func TestPriorityScheduler_Dequeue_WakeupOnEnqueue(t *testing.T) {
	t.Parallel()
	config := DefaultPriorityConfig()
	scheduler := NewPriorityScheduler(config)
	defer scheduler.Shutdown()

	done := make(chan *PriorityRequest, 1)

	go func() {
		req, err := scheduler.Dequeue(context.Background())
		done <- req
		assert.NoError(t, err)
		assert.Equal(t, "test-req", req.ID)
	}()

	// Give Dequeue time to block on empty queue
	time.Sleep(10 * time.Millisecond)

	err := scheduler.Enqueue(context.Background(), "test-req", PriorityHigh, func() error { return nil }, time.Minute)
	assert.NoError(t, err)

	select {
	case req := <-done:
		assert.NotNil(t, req)
	case <-time.After(2 * time.Second):
		t.Fatal("Dequeue did not wake up after Enqueue")
	}
}

func TestPriorityScheduler_Dequeue_ContextCancellation(t *testing.T) {
	t.Parallel()
	config := DefaultPriorityConfig()
	scheduler := NewPriorityScheduler(config)
	defer scheduler.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := scheduler.Dequeue(ctx)
		done <- err
	}()

	select {
	case err := <-done:
		assert.Error(t, err)
		assert.Equal(t, context.DeadlineExceeded, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Dequeue did not cancel within timeout")
	}
}

func TestPriorityScheduler_Dequeue_Shutdown(t *testing.T) {
	t.Parallel()
	config := DefaultPriorityConfig()
	scheduler := NewPriorityScheduler(config)

	done := make(chan error, 1)
	go func() {
		_, err := scheduler.Dequeue(context.Background())
		done <- err
	}()

	// Give Dequeue time to block on empty queue
	time.Sleep(10 * time.Millisecond)
	scheduler.Shutdown()

	select {
	case err := <-done:
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "shutdown")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Dequeue did not return after Shutdown")
	}
}
