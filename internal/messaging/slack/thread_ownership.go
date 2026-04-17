package slack

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ThreadKey uniquely identifies a thread within a channel.
type ThreadKey struct {
	channelID string
	threadTS  string
}

func NewThreadKey(channelID, threadTS string) ThreadKey {
	return ThreadKey{channelID: channelID, threadTS: threadTS}
}

// ThreadOwnershipTracker tracks which bot owns a thread to avoid response conflicts.
// Rules:
//
//	R1: First response → claim ownership
//	R2: Only owner responds to non-@ messages in thread
//	R3: @BotB in BotA's thread → BotB preempts, BotA releases
//	R4: @BotA @BotB → both independently claim ownership
//	R5: @Others (no bot mentioned) → release ownership
type ThreadOwnershipTracker struct {
	mu           sync.RWMutex
	ownedThreads map[ThreadKey]*time.Time
	ttl          time.Duration
	botID        string
	logger       *slog.Logger
	done         chan struct{}
	wg           sync.WaitGroup
}

// NewThreadOwnershipTracker creates a new tracker.
func NewThreadOwnershipTracker(ctx context.Context, botID string, logger *slog.Logger) *ThreadOwnershipTracker {
	t := &ThreadOwnershipTracker{
		ownedThreads: make(map[ThreadKey]*time.Time),
		ttl:          24 * time.Hour,
		botID:        botID,
		logger:       logger,
		done:         make(chan struct{}),
	}
	t.wg.Add(1)
	go t.cleanupLoop()
	return t
}

// ShouldRespond decides whether this bot should respond to a message.
func (t *ThreadOwnershipTracker) ShouldRespond(channelType, threadTS, text, userID string) bool {
	key := NewThreadKey(channelType, threadTS)
	inThread := threadTS != ""
	mentioned := strings.Contains(text, "<@"+t.botID+">")
	mentionsOthers := t.mentionsOtherBot(text)

	if !inThread {
		// DM or main channel: always respond if mentioned
		return true
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	_, owned := t.ownedThreads[key]

	if mentioned && !mentionsOthers {
		// R3 or R4: mentioned → claim or re-claim
		now := time.Now()
		t.ownedThreads[key] = &now
		return true
	}

	if mentioned && mentionsOthers {
		// R4: both mentioned → claim
		now := time.Now()
		t.ownedThreads[key] = &now
		return true
	}

	if mentionsOthers && !mentioned {
		// R5: other bot mentioned, not us → release
		delete(t.ownedThreads, key)
		return false
	}

	if owned {
		// R2: we own this thread, non-@ message → respond
		now := time.Now()
		t.ownedThreads[key] = &now
		return true
	}

	// R1: first message in thread, not mentioned → don't respond yet
	return false
}

func (t *ThreadOwnershipTracker) mentionsOtherBot(text string) bool {
	offset := 0
	for {
		idx := strings.Index(text[offset:], "<@")
		if idx == -1 {
			break
		}
		idx += offset
		end := strings.Index(text[idx:], ">")
		if end == -1 {
			break
		}
		end += idx
		mention := text[idx+2 : end]
		if mention != t.botID && strings.HasPrefix(mention, "U") {
			return true
		}
		offset = end + 1
	}
	return false
}

// Stop shuts down the cleanup goroutine.
func (t *ThreadOwnershipTracker) Stop() {
	close(t.done)
	t.wg.Wait()
}

func (t *ThreadOwnershipTracker) cleanupLoop() {
	defer t.wg.Done()
	ticker := time.NewTicker(t.ttl / 10)
	defer ticker.Stop()

	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			t.mu.Lock()
			now := time.Now()
			for key, ts := range t.ownedThreads {
				if now.Sub(*ts) > t.ttl {
					delete(t.ownedThreads, key)
				}
			}
			t.mu.Unlock()
		}
	}
}
