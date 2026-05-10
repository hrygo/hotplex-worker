package gateway

import (
	"log/slog"
	"testing"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// --- Component-level benchmarks (no WS dependency) ---

func BenchmarkEncodeJSON(b *testing.B) {
	env := &events.Envelope{
		Version: events.Version, ID: aep.NewID(), SessionID: "sess_bench",
		Seq: 1, Timestamp: time.Now().UnixMilli(),
		Event: events.Event{Type: events.MessageDelta, Data: events.MessageDeltaData{Content: "Hello, world! This is a streaming delta with some content."}},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = aep.EncodeJSON(env)
	}
}

func BenchmarkEventsClone(b *testing.B) {
	benchmarks := []struct {
		name string
		env  *events.Envelope
	}{
		{
			"MessageDelta",
			&events.Envelope{
				Version: events.Version, ID: aep.NewID(), SessionID: "sess_bench",
				Seq: 1, Timestamp: time.Now().UnixMilli(),
				Event: events.Event{Type: events.MessageDelta, Data: events.MessageDeltaData{Content: "Hello, world! Streaming delta content."}},
			},
		},
		{
			"Message",
			&events.Envelope{
				Version: events.Version, ID: aep.NewID(), SessionID: "sess_bench",
				Seq: 42, Timestamp: time.Now().UnixMilli(),
				Event: events.Event{Type: events.Message, Data: events.MessageData{Content: "Complete message text.", Role: "assistant"}},
			},
		},
		{
			"State",
			&events.Envelope{
				Version: events.Version, ID: aep.NewID(), SessionID: "sess_bench",
				Seq: 10, Timestamp: time.Now().UnixMilli(),
				Event: events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning, Message: "started"}},
			},
		},
		{
			"map[string]any_data",
			&events.Envelope{
				Version: events.Version, ID: aep.NewID(), SessionID: "sess_bench",
				Seq: 5, Timestamp: time.Now().UnixMilli(),
				Event: events.Event{Type: events.Pong, Data: map[string]any{"state": "running", "ts": int64(1234567890)}},
			},
		},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = events.Clone(bm.env)
			}
		})
	}
}

func BenchmarkNextSeq(b *testing.B) {
	cfg := config.Default()
	cfg.Gateway.BroadcastQueueSize = 256
	hub := NewHub(slog.Default(), config.NewConfigStore(cfg, nil))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		hub.NextSeq("sess_bench_seq")
	}
}

func BenchmarkNewEnvelope(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = events.NewEnvelope(
			aep.NewID(), "sess_bench", int64(i+1),
			events.MessageDelta, events.MessageDeltaData{Content: "benchmark delta content"},
		)
	}
}
