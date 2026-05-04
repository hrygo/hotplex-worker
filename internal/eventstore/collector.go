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

	deltaFlushSize     = 4096 // bytes — flush accumulator when content exceeds this
	deltaFlushInterval = 2 * time.Second
)

// StorableTypes is the set of AEP event types eligible for event storage replay.
var StorableTypes = map[events.Kind]bool{
	events.Init:                true,
	events.Error:               true,
	events.State:               true,
	events.Input:               true,
	events.Done:                true,
	events.Message:             true,
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
//
// Delta accumulation uses three flush triggers:
//   - Size: content exceeds deltaFlushSize (hot path, synchronous)
//   - Timer: accumulator age exceeds deltaFlushInterval (runWriter ticker)
//   - Event: MessageEnd or next storable event (hot path, synchronous)
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

	// MessageEnd triggers flush but is not stored itself.
	if eventType == events.MessageEnd {
		c.flushDelta(sessionID)
		return
	}

	if !IsStorable(eventType) {
		return
	}

	c.flushDelta(sessionID)

	req := &captureRequest{event: &StoredEvent{
		SessionID: sessionID,
		Seq:       seq,
		Type:      string(eventType),
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

	if acc == nil || acc.count == 0 {
		return
	}
	c.send(acc.toRequest(sessionID))
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
	// Swap accumulator map under lock, flush outside to avoid deadlock.
	c.accumMu.Lock()
	pending := c.accum
	c.accum = nil
	c.accumMu.Unlock()

	for sid, acc := range pending {
		if acc.count > 0 {
			c.send(acc.toRequest(sid))
		}
	}

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
			c.flushTimedOutAccumulators(&batch)
			flush()
		}
	}
}

// flushTimedOutAccumulators scans all accumulators and flushes those whose
// age exceeds deltaFlushInterval. Bypasses captureC to avoid deadlock in
// runWriter (which both reads from and would write to captureC).
func (c *Collector) flushTimedOutAccumulators(batch *[]*captureRequest) {
	now := time.Now()
	c.accumMu.Lock()
	for sid, acc := range c.accum {
		if now.Sub(acc.firstSeenAt) >= deltaFlushInterval {
			delete(c.accum, sid)
			*batch = append(*batch, acc.toRequest(sid))
		}
	}
	c.accumMu.Unlock()
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

// CaptureDeltaString accumulates a message.delta content string directly,
// skipping the json.Marshal/Unmarshal round-trip of Capture.
// Flushes immediately when accumulated content exceeds deltaFlushSize.
func (c *Collector) CaptureDeltaString(sessionID string, seq int64, content string) {
	c.accumMu.Lock()
	acc := c.accum[sessionID]
	if acc == nil {
		acc = newDeltaAccumulator()
		c.accum[sessionID] = acc
	}
	acc.appendRaw(seq, content)

	if acc.content.Len() >= deltaFlushSize {
		delete(c.accum, sessionID)
		c.accumMu.Unlock()
		c.send(acc.toRequest(sessionID))
		return
	}
	c.accumMu.Unlock()
}

// ResetSession discards any accumulated delta content for the given session.
func (c *Collector) ResetSession(sessionID string) {
	c.accumMu.Lock()
	delete(c.accum, sessionID)
	c.accumMu.Unlock()
}

// deltaAccumulator merges message.delta content in-memory.
type deltaAccumulator struct {
	content     strings.Builder
	seq         int64
	firstSeq    int64
	lastSeq     int64
	count       int
	firstSeenAt time.Time
}

func newDeltaAccumulator() *deltaAccumulator {
	return &deltaAccumulator{}
}

func (a *deltaAccumulator) append(seq int64, data json.RawMessage) {
	var delta struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(data, &delta)

	a.appendRaw(seq, delta.Content)
}

func (a *deltaAccumulator) appendRaw(seq int64, content string) {
	a.content.WriteString(content)
	a.lastSeq = seq
	if a.count == 0 {
		a.firstSeq = seq
		a.seq = seq
		a.firstSeenAt = time.Now()
	}
	a.count++
}

func (a *deltaAccumulator) toRequest(sessionID string) *captureRequest {
	mergedData, _ := json.Marshal(map[string]any{
		"content":      a.content.String(),
		"merged_count": a.lastSeq - a.firstSeq + 1,
		"seq_range":    []int64{a.firstSeq, a.lastSeq},
	})
	return &captureRequest{event: &StoredEvent{
		SessionID: sessionID,
		Seq:       a.seq,
		Type:      string(events.Message),
		Data:      mergedData,
		Direction: "outbound",
		CreatedAt: a.firstSeenAt.UnixMilli(),
	}}
}
