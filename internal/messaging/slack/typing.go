package slack

import (
	"context"
	"sync"
	"time"

	"github.com/slack-go/slack"
)

// DefaultStages defines multi-stage emoji progress indicators.
var DefaultStages = []TypingStage{
	{0 * time.Second, "eyes"},
	{2 * time.Minute, "clock1"},
	{7 * time.Minute, "hourglass_flowing_sand"},
	{12 * time.Minute, "gear"},
	{17 * time.Minute, "hourglass_flowing_sand"},
}

// TypingStage defines a single emoji reaction stage.
type TypingStage struct {
	After time.Duration
	Emoji string
}

// TypingIndicator manages multi-stage emoji reactions for a single message.
type TypingIndicator struct {
	adapter   *Adapter
	channelID string
	threadTS  string
	messageTS string
	stages    []TypingStage

	mu     sync.Mutex
	done   bool
	added  []string
	stopCh chan struct{}
}

// NewTypingIndicator creates a new typing indicator.
func NewTypingIndicator(adapter *Adapter, channelID, threadTS, messageTS string, stages []TypingStage) *TypingIndicator {
	if len(stages) == 0 {
		stages = DefaultStages
	}
	return &TypingIndicator{
		adapter:   adapter,
		channelID: channelID,
		threadTS:  threadTS,
		messageTS: messageTS,
		stages:    stages,
		stopCh:    make(chan struct{}),
	}
}

// Start adds the first emoji immediately and schedules subsequent stages.
func (ti *TypingIndicator) Start(ctx context.Context) {
	if ti.adapter == nil {
		return
	}
	ti.doAddReaction(ctx, ti.stages[0].Emoji)
	go ti.runStages(ctx)
}

// Stop removes all added reactions. Safe to call multiple times.
func (ti *TypingIndicator) Stop(ctx context.Context) {
	ti.mu.Lock()
	if ti.done {
		ti.mu.Unlock()
		return
	}
	ti.done = true
	close(ti.stopCh)
	var added []string
	if len(ti.added) > 0 {
		added = make([]string, len(ti.added))
		copy(added, ti.added)
	}
	ti.mu.Unlock()

	for _, emoji := range added {
		ti.removeReaction(ctx, emoji)
	}
}

func (ti *TypingIndicator) runStages(ctx context.Context) {
	for i := 1; i < len(ti.stages); i++ {
		stage := ti.stages[i]
		wait := stage.After - ti.stages[i-1].After
		if wait <= 0 {
			continue
		}

		select {
		case <-ti.stopCh:
			return
		case <-ctx.Done():
			return
		case <-time.After(wait):
			ti.doAddReaction(ctx, stage.Emoji)
		}
	}
}

func (ti *TypingIndicator) doAddReaction(ctx context.Context, emoji string) {
	if ti.adapter.client == nil || ti.messageTS == "" {
		return
	}
	err := ti.adapter.client.AddReactionContext(ctx, emoji, slack.ItemRef{
		Channel:   ti.channelID,
		Timestamp: ti.messageTS,
	})
	if err == nil {
		ti.mu.Lock()
		ti.added = append(ti.added, emoji)
		ti.mu.Unlock()
	}
}

func (ti *TypingIndicator) removeReaction(ctx context.Context, emoji string) {
	if ti.adapter.client == nil || ti.messageTS == "" {
		return
	}
	_ = ti.adapter.client.RemoveReactionContext(ctx, emoji, slack.ItemRef{
		Channel:   ti.channelID,
		Timestamp: ti.messageTS,
	})
}

// ActiveIndicators manages all active typing indicators.
type ActiveIndicators struct {
	mu         sync.Mutex
	indicators map[string]*TypingIndicator // key: "channelID:messageTS"
}

// NewActiveIndicators creates a new indicator manager.
func NewActiveIndicators() *ActiveIndicators {
	return &ActiveIndicators{
		indicators: make(map[string]*TypingIndicator),
	}
}

// Start begins a typing indicator for a message. Only starts if not already active.
func (ai *ActiveIndicators) Start(ctx context.Context, adapter *Adapter, channelID, threadTS, messageTS string) {
	key := channelID + ":" + messageTS
	ai.mu.Lock()
	defer ai.mu.Unlock()

	if _, exists := ai.indicators[key]; exists {
		return
	}

	ti := NewTypingIndicator(adapter, channelID, threadTS, messageTS, DefaultStages)
	ai.indicators[key] = ti
	ti.Start(ctx)
}

// Stop stops and removes the indicator for a message.
func (ai *ActiveIndicators) Stop(ctx context.Context, channelID, messageTS string) {
	key := channelID + ":" + messageTS
	ai.mu.Lock()
	ti, exists := ai.indicators[key]
	if exists {
		delete(ai.indicators, key)
	}
	ai.mu.Unlock()

	if exists {
		ti.Stop(ctx)
	}
}
