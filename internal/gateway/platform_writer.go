package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// pcEntry wraps a PlatformConn with an async writeLoop goroutine and delta
// coalescing. It satisfies the SessionWriter interface.
//
// WriteCtx sends envelopes to a buffered channel; a dedicated writeLoop
// goroutine reads from the channel, coalesces consecutive droppable events
// (message.delta / raw), and forwards merged or individual envelopes to the
// underlying PlatformConn. This decouples Hub.Run() from blocking platform
// HTTP API calls.
type pcEntry struct {
	pc      messaging.PlatformConn
	cfg     pcEntryConfig
	ch      chan *events.Envelope
	closeCh chan struct{} // signals Close() was called
	done    chan struct{}
	closeMu sync.Once
	log     *slog.Logger
}

type pcEntryConfig struct {
	WriteBuffer   int
	DropThreshold int
	CoalesceIntvl time.Duration
	CoalesceSize  int
}

func defaultPCEntryConfig(cfg *config.Config) pcEntryConfig {
	c := pcEntryConfig{
		WriteBuffer:   cfg.Gateway.PlatformWriteBuffer,
		DropThreshold: cfg.Gateway.PlatformDropThreshold,
		CoalesceIntvl: cfg.Gateway.DeltaCoalesceInterval,
		CoalesceSize:  cfg.Gateway.DeltaCoalesceSize,
	}
	if c.WriteBuffer <= 0 {
		c.WriteBuffer = 64
	}
	if c.DropThreshold <= 0 {
		c.DropThreshold = 56
	}
	if c.CoalesceIntvl <= 0 {
		c.CoalesceIntvl = 120 * time.Millisecond
	}
	if c.CoalesceSize <= 0 {
		c.CoalesceSize = 200
	}
	return c
}

func newPCEntry(pc messaging.PlatformConn, cfg pcEntryConfig, log *slog.Logger) *pcEntry {
	e := &pcEntry{
		pc:      pc,
		ch:      make(chan *events.Envelope, cfg.WriteBuffer),
		closeCh: make(chan struct{}),
		done:    make(chan struct{}),
		cfg:     cfg,
		log:     log,
	}
	go e.writeLoop()
	return e
}

func (e *pcEntry) WriteCtx(_ context.Context, env *events.Envelope) error {
	if isDroppable(env.Event.Type) {
		if len(e.ch) >= e.cfg.DropThreshold {
			metrics.GatewayPlatformDroppedTotal.WithLabelValues(string(env.Event.Type)).Inc()
			return nil
		}
		select {
		case e.ch <- env:
			return nil
		default:
			metrics.GatewayPlatformDroppedTotal.WithLabelValues(string(env.Event.Type)).Inc()
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case e.ch <- env:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("platform conn write timeout: buffer full")
	case <-e.closeCh: // check close signal instead of relying solely on done
		return errors.New("platform conn closed")
	case <-e.done:
		return errors.New("platform conn closed")
	}
}

// Close signals writeLoop to drain pending deltas and exit, waits for
// completion, then closes the underlying PlatformConn.
func (e *pcEntry) Close() error {
	var err error
	e.closeMu.Do(func() {
		close(e.closeCh)
		close(e.ch) // signal writeLoop to drain and exit
		<-e.done
		err = e.pc.Close()
	})
	return err
}

// writeLoop reads envelopes from the channel, coalesces consecutive droppable
// events into merged deltas, and forwards them to the underlying PlatformConn.
func (e *pcEntry) writeLoop() {
	defer close(e.done)
	defer func() {
		if r := recover(); r != nil {
			e.log.Error("pcEntry writeLoop panic", "panic", r, "stack", string(debug.Stack()))
		}
	}()

	var db strings.Builder
	var timer *time.Timer
	var timerCh <-chan time.Time
	var pendingSID string // tracks SessionID for pending coalesced deltas

	flush := func(sid string) {
		if db.Len() == 0 {
			return
		}
		merged := &events.Envelope{
			Version:   events.Version,
			ID:        aep.NewID(),
			SessionID: sid,
			Event: events.Event{
				Type: events.MessageDelta,
				Data: events.MessageDeltaData{
					Content: db.String(),
				},
			},
		}
		metrics.GatewayDeltaFlushTotal.Inc()
		db.Reset()
		if timer != nil {
			timer.Stop()
			timerCh = nil
		}
		e.writeOne(merged)
	}

	for {
		select {
		case env, ok := <-e.ch:
			if !ok {
				flush(pendingSID)
				return
			}

			if isDroppable(env.Event.Type) {
				content := extractDeltaContent(env)
				if db.Len() == 0 {
					pendingSID = env.SessionID
				}
				db.WriteString(content)
				metrics.GatewayDeltaCoalescedTotal.Inc()

				if utf8.RuneCountInString(db.String()) >= e.cfg.CoalesceSize {
					flush(pendingSID)
				} else if timer == nil {
					timer = time.NewTimer(e.cfg.CoalesceIntvl)
					timerCh = timer.C
				} else {
					timer.Reset(e.cfg.CoalesceIntvl)
				}
			} else {
				flush(pendingSID)
				e.writeOne(env)
			}

		case <-timerCh:
			flush(pendingSID)
		}
	}
}

func (e *pcEntry) writeOne(env *events.Envelope) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.pc.WriteCtx(ctx, env); err != nil {
		e.log.Warn("platform async write failed",
			"event_type", env.Event.Type,
			"session_id", env.SessionID,
			"err", err)
	}
}

func extractDeltaContent(env *events.Envelope) string {
	switch env.Event.Type {
	case events.MessageDelta:
		// Data arrives as map[string]any from JSON unmarshal; struct type never matches.
		if m, ok := env.Event.Data.(map[string]any); ok {
			if c, _ := m["content"].(string); c != "" {
				return c
			}
		}
	case events.Raw:
		if d, ok := env.Event.Data.(events.RawData); ok {
			if m, ok := d.Raw.(map[string]any); ok {
				if t, _ := m["text"].(string); t != "" {
					return t
				}
			}
		}
	}
	if s, ok := env.Event.Data.(string); ok {
		return s
	}
	return ""
}
