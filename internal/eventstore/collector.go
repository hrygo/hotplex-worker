package eventstore

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/pkg/events"
)

const (
	collectorChanCap       = 2048
	collectorBatchMax      = 100
	collectorFlushInterval = 100 * time.Millisecond
)

// StorableTypes is the set of AEP event types eligible for event storage replay.
var StorableTypes = map[events.Kind]bool{
	events.Init:                true,
	events.Error:               true,
	events.State:               true,
	events.Input:               true,
	events.Done:                true,
	events.Message:             true,
	events.MessageStart:        true,
	events.MessageEnd:          true,
	events.ToolCall:            true,
	events.ToolResult:          true,
	events.Reasoning:           true,
	events.Step:                true,
	events.PermissionRequest:   true,
	events.PermissionResponse:  true,
	events.QuestionRequest:     true,
	events.QuestionResponse:    true,
	events.ElicitationRequest:  true,
	events.ElicitationResponse: true,
	events.ContextUsage:        true,
	events.Control:             true,
}

// IsStorable returns true if the event type should be persisted for replay.
func IsStorable(eventType events.Kind) bool {
	return StorableTypes[eventType]
}

// captureRequest is a pending write for the background batch writer.
type captureRequest struct {
	event *StoredEvent
}

// Collector captures AEP events, merges message.delta streams, and writes
// them asynchronously to the underlying EventStore.
type Collector struct {
	store    EventStore
	captureC chan *captureRequest
	closeC   chan struct{}
	closeWg  sync.WaitGroup
	log      *slog.Logger

	accumMu sync.Mutex
	accum   map[string]*deltaAccumulator // sessionID → active delta accumulator
}

// NewCollector creates a Collector that writes events to store.
func NewCollector(store EventStore, log *slog.Logger) *Collector {
	c := &Collector{
		store:    store,
		captureC: make(chan *captureRequest, collectorChanCap),
		closeC:   make(chan struct{}),
		log:      log.With("component", "eventstore-collector"),
		accum:    make(map[string]*deltaAccumulator),
	}
	c.closeWg.Add(1)
	go c.runWriter()
	return c
}

// Capture sends an event to the collector for async persistence.
// If the event is a message.delta, it is accumulated in-memory and merged
// on message.end or next non-delta event.
func (c *Collector) Capture(sessionID string, seq int64, eventType events.Kind, data json.RawMessage, direction string) {
	if !IsStorable(eventType) {
		return
	}

	now := time.Now().UnixMilli()

	if eventType == events.MessageDelta {
		c.accumMu.Lock()
		acc := c.accum[sessionID]
		if acc == nil {
			acc = newDeltaAccumulator()
			c.accum[sessionID] = acc
		}
		acc.append(seq, data)
		c.accumMu.Unlock()
		return
	}

	// Flush any pending delta accumulation for this session before processing
	// the current event.
	c.flushDelta(sessionID)

	// Send the current event.
	req := &captureRequest{event: &StoredEvent{
		SessionID: sessionID,
		Seq:       seq,
		Type:      string(eventType),
		Data:      data,
		Direction: direction,
		CreatedAt: now,
	}}
	c.send(req)
}

// CaptureDeltaEnd is called on message.end to flush the accumulated delta.
func (c *Collector) CaptureDeltaEnd(sessionID string, endSeq int64, data json.RawMessage, direction string) {
	c.flushDelta(sessionID)

	req := &captureRequest{event: &StoredEvent{
		SessionID: sessionID,
		Seq:       endSeq,
		Type:      string(events.MessageEnd),
		Data:      data,
		Direction: direction,
		CreatedAt: time.Now().UnixMilli(),
	}}
	c.send(req)
}

func (c *Collector) flushDelta(sessionID string) {
	c.accumMu.Lock()
	acc := c.accum[sessionID]
	delete(c.accum, sessionID)
	c.accumMu.Unlock()

	if acc == nil || acc.len() == 0 {
		return
	}

	merged, seq, firstSeq, lastSeq := acc.flush()
	mergedData, _ := json.Marshal(map[string]any{
		"content":      merged,
		"merged_count": lastSeq - firstSeq + 1,
		"seq_range":    []int64{firstSeq, lastSeq},
	})

	req := &captureRequest{event: &StoredEvent{
		SessionID: sessionID,
		Seq:       seq,
		Type:      string(events.Message),
		Data:      mergedData,
		Direction: "outbound",
		CreatedAt: time.Now().UnixMilli(),
	}}
	c.send(req)
}

func (c *Collector) send(req *captureRequest) {
	select {
	case c.captureC <- req:
	default:
		c.log.Warn("eventstore: capture channel full, dropping event",
			"session_id", req.event.SessionID,
			"seq", req.event.Seq,
			"type", req.event.Type,
		)
	}
}

// Close drains the capture channel and flushes remaining events.
func (c *Collector) Close() error {
	// Flush any remaining delta accumulators.
	c.accumMu.Lock()
	for sid := range c.accum {
		c.flushDelta(sid)
	}
	c.accumMu.Unlock()

	close(c.closeC)
	c.closeWg.Wait()
	return nil
}

func (c *Collector) runWriter() {
	defer c.closeWg.Done()

	ticker := time.NewTicker(collectorFlushInterval)
	defer ticker.Stop()

	var batch []*captureRequest
	flush := func() {
		if len(batch) == 0 {
			return
		}
		c.flushBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-c.closeC:
			// Drain remaining items in channel.
			for {
				select {
				case req := <-c.captureC:
					batch = append(batch, req)
				default:
					flush()
					return
				}
			}
		case req := <-c.captureC:
			batch = append(batch, req)
			if len(batch) >= collectorBatchMax {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (c *Collector) flushBatch(batch []*captureRequest) {
	if len(batch) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := c.store.BeginTx(ctx)
	if err != nil {
		c.log.Error("eventstore: batch tx begin", "err", err)
		return
	}

	for _, req := range batch {
		if err := tx.Append(ctx, req.event); err != nil {
			c.log.Warn("eventstore: batch append failed",
				"session_id", req.event.SessionID,
				"seq", req.event.Seq,
				"type", req.event.Type,
				"err", err,
			)
		}
	}

	if err := tx.Commit(); err != nil {
		c.log.Error("eventstore: batch commit", "err", err)
	}
}

// deltaAccumulator merges message.delta content in-memory.
type deltaAccumulator struct {
	content  strings.Builder
	seq      int64
	firstSeq int64
	lastSeq  int64
	count    int
}

func newDeltaAccumulator() *deltaAccumulator {
	return &deltaAccumulator{}
}

func (a *deltaAccumulator) append(seq int64, data json.RawMessage) {
	var delta struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(data, &delta)

	a.content.WriteString(delta.Content)
	a.lastSeq = seq
	if a.count == 0 {
		a.firstSeq = seq
		a.seq = seq
	}
	a.count++
}

func (a *deltaAccumulator) flush() (content string, seq, firstSeq, lastSeq int64) {
	content = a.content.String()
	seq = a.seq
	firstSeq = a.firstSeq
	lastSeq = a.lastSeq
	return
}

func (a *deltaAccumulator) len() int {
	return a.count
}
