package feishu

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// chatTaskTimeout bounds the maximum execution time of a single chat task.
// Prevents goroutine leaks from tasks blocked on external APIs indefinitely.
const chatTaskTimeout = 10 * time.Minute

type ChatQueue struct {
	log     *slog.Logger
	mu      sync.Mutex
	workers map[string]*chatWorker
}

type chatWorker struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewChatQueue(log *slog.Logger) *ChatQueue {
	return &ChatQueue{
		log:     log,
		workers: make(map[string]*chatWorker),
	}
}

func (q *ChatQueue) Enqueue(chatID string, task func(ctx context.Context) error) error {
	q.mu.Lock()
	w, exists := q.workers[chatID]
	if !exists {
		w = &chatWorker{done: make(chan struct{})}
		q.workers[chatID] = w
	}
	q.mu.Unlock()

	if !exists {
		ctx, cancel := context.WithTimeout(context.Background(), chatTaskTimeout)
		w.mu.Lock()
		w.cancel = cancel
		w.mu.Unlock()

		go func() {
			defer close(w.done)
			defer cancel()
			if err := task(ctx); err != nil && ctx.Err() == nil {
				q.log.Warn("feishu: chat queue task error", "chat_id", chatID, "error", err)
			}
			q.mu.Lock()
			delete(q.workers, chatID)
			q.mu.Unlock()
		}()
		return nil
	}

	go func() {
		<-w.done

		q.mu.Lock()
		newW := &chatWorker{done: make(chan struct{})}
		q.workers[chatID] = newW
		q.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), chatTaskTimeout)
		newW.mu.Lock()
		newW.cancel = cancel
		newW.mu.Unlock()

		defer close(newW.done)
		defer cancel()
		if err := task(ctx); err != nil && ctx.Err() == nil {
			q.log.Warn("feishu: chat queue task error", "chat_id", chatID, "error", err)
		}
		q.mu.Lock()
		delete(q.workers, chatID)
		q.mu.Unlock()
	}()

	return nil
}

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

	q.log.Debug("feishu: aborted task for chat", "chat_id", chatID)
}
