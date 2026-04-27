package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// chatTaskTimeout bounds the maximum execution time of a single chat task.
// Prevents goroutine leaks from tasks blocked on external APIs indefinitely.
const chatTaskTimeout = 10 * time.Minute

// chatIdleTimeout is how long a worker goroutine waits for new tasks before
// self-cleaning. Prevents goroutine leaks from one-off chats.
const chatIdleTimeout = 5 * time.Minute

// ChatQueue serializes message sends per chatID to prevent reordering.
// Each chatID gets a dedicated goroutine that processes tasks sequentially
// through a buffered channel, eliminating race conditions that existed in the
// previous goroutine-chaining approach.
type ChatQueue struct {
	log     *slog.Logger
	mu      sync.Mutex
	workers map[string]*chatWorker
	wg      sync.WaitGroup // track worker goroutines for graceful shutdown
}

type chatWorker struct {
	tasks  chan func(ctx context.Context) error
	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewChatQueue(log *slog.Logger) *ChatQueue {
	return &ChatQueue{
		log:     log,
		workers: make(map[string]*chatWorker),
	}
}

// Enqueue submits a task for serial execution on the given chatID.
// If no worker exists for the chatID, one is created.
// Returns an error if the per-chatID task channel is full.
func (q *ChatQueue) Enqueue(chatID string, task func(ctx context.Context) error) error {
	q.mu.Lock()
	w, exists := q.workers[chatID]
	if !exists {
		w = &chatWorker{
			tasks: make(chan func(ctx context.Context) error, 64),
		}
		q.workers[chatID] = w
	}
	q.mu.Unlock()

	if !exists {
		q.wg.Add(1)
		go q.runWorker(chatID, w)
	}

	select {
	case w.tasks <- task:
		return nil
	default:
		return fmt.Errorf("feishu: chat queue full for %s", chatID)
	}
}

// runWorker processes tasks from the channel sequentially for a single chatID.
// It exits when: (1) the channel is closed (via Close), or (2) idle timeout
// elapses with no new tasks. On exit, it removes itself from the workers map.
func (q *ChatQueue) runWorker(chatID string, w *chatWorker) {
	defer q.wg.Done()

	idleTimer := time.NewTimer(chatIdleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case task, ok := <-w.tasks:
			if !ok {
				q.mu.Lock()
				delete(q.workers, chatID)
				q.mu.Unlock()
				return
			}
			if !idleTimer.Stop() {
				<-idleTimer.C
			}
			idleTimer.Reset(chatIdleTimeout)

			ctx, cancel := context.WithTimeout(context.Background(), chatTaskTimeout)
			w.mu.Lock()
			w.cancel = cancel
			w.mu.Unlock()

			if err := task(ctx); err != nil && q.log != nil {
				if ctx.Err() != nil {
					q.log.Warn("feishu: chat queue task timed out", "chat_id", chatID, "err", err)
				} else {
					q.log.Warn("feishu: chat queue task error", "chat_id", chatID, "err", err)
				}
			}
			cancel()

		case <-idleTimer.C:
			q.mu.Lock()
			delete(q.workers, chatID)
			q.mu.Unlock()
			return
		}
	}
}

// Abort cancels the currently running task for the given chatID.
func (q *ChatQueue) Abort(chatID string) {
	q.mu.Lock()
	w, exists := q.workers[chatID]
	q.mu.Unlock()

	if !exists {
		return
	}

	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()

	if q.log != nil {
		q.log.Debug("feishu: aborted task for chat", "chat_id", chatID)
	}
}

// Close shuts down all worker goroutines by closing their task channels.
// It waits for all in-flight tasks to complete.
func (q *ChatQueue) Close() {
	q.mu.Lock()
	workers := make(map[string]*chatWorker, len(q.workers))
	for k, v := range q.workers {
		workers[k] = v
	}
	q.mu.Unlock()

	for _, w := range workers {
		close(w.tasks)
	}
	q.wg.Wait() // wait for all workers to finish
}
