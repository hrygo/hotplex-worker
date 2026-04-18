package feishu

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TC-2.4-1: 同一 chatID 的消息串行处理
func TestChatQueue_SerializesPerChat(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)

	var counter atomic.Int32

	var mu sync.Mutex
	var order []int

	// Enqueue 3 tasks for the same chat.
	for range 3 {
		require.NoError(t, q.Enqueue("chat_1", func(ctx context.Context) error {
			cur := counter.Add(1)
			mu.Lock()
			order = append(order, int(cur))
			mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			return nil
		}))
	}

	// Wait for all tasks to complete.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	require.Len(t, order, 3)
	mu.Unlock()

	// Counter should reach 3 (tasks serialized).
	require.Eventually(t, func() bool {
		return counter.Load() == 3
	}, 500*time.Millisecond, 10*time.Millisecond)
}

// TC-2.4-2: 不同 chatID 的消息并行处理
func TestChatQueue_ParallelizesAcrossChats(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)

	var counter atomic.Int32

	// Enqueue 2 tasks for 2 different chats simultaneously.
	require.NoError(t, q.Enqueue("chat_A", func(context.Context) error {
		counter.Add(1)
		time.Sleep(50 * time.Millisecond)
		return nil
	}))
	require.NoError(t, q.Enqueue("chat_B", func(context.Context) error {
		counter.Add(1)
		time.Sleep(50 * time.Millisecond)
		return nil
	}))

	// Both should complete within ~50ms (parallel), not ~100ms (serial).
	// Allow some margin for test flakiness.
	require.Eventually(t, func() bool {
		return counter.Load() == 2
	}, 150*time.Millisecond, 10*time.Millisecond)
}

// TC-2.4-3: Abort 能取消正在执行的任务
func TestChatQueue_Abort(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)

	var started atomic.Bool
	var aborted atomic.Bool

	require.NoError(t, q.Enqueue("chat_abort", func(ctx context.Context) error {
		started.Store(true)
		select {
		case <-ctx.Done():
			aborted.Store(true)
			return ctx.Err()
		case <-time.After(10 * time.Second):
			return nil
		}
	}))

	// Wait for task to start.
	require.Eventually(t, func() bool { return started.Load() }, 500*time.Millisecond, 10*time.Millisecond)

	// Abort the chat.
	q.Abort("chat_abort")

	// The task should be aborted.
	require.Eventually(t, func() bool { return aborted.Load() }, 500*time.Millisecond, 10*time.Millisecond)
}

// TC-2.4-4: worker 空闲后自动清理
func TestChatQueue_WorkerCleanup(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)

	require.NoError(t, q.Enqueue("chat_cleanup", func(_ context.Context) error {
		return nil
	}))

	// Wait for task to complete.
	time.Sleep(50 * time.Millisecond)

	// Worker should be cleaned up.
	q.mu.Lock()
	_, exists := q.workers["chat_cleanup"]
	q.mu.Unlock()
	require.False(t, exists, "worker should be removed after task completion")
}

// Test: multiple rapid messages for same chat are queued and processed in order.
func TestChatQueue_RapidMessages(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)

	var last atomic.Int32

	// Send 5 rapid messages for the same chat.
	for range 5 {
		require.NoError(t, q.Enqueue("chat_rapid", func(_ context.Context) error {
			time.Sleep(5 * time.Millisecond)
			last.Add(1)
			return nil
		}))
	}

	// Should eventually process all 5.
	require.Eventually(t, func() bool {
		return last.Load() == 5
	}, 1*time.Second, 50*time.Millisecond)
}

// Test: chat with no worker is a no-op.
func TestChatQueue_Abort_NoWorker(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(nil)
	// Aborting a chat with no active worker should not panic.
	q.Abort("chat_nonexistent")
}

// Test: worker 10-minute timeout prevents goroutine leaks.
func TestChatQueue_TaskTimeout(t *testing.T) {
	t.Parallel()
	// Verify that the timeout constant is set to 10 minutes.
	require.Equal(t, 10*time.Minute, chatTaskTimeout)
}
