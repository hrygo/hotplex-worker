package gateway

import (
	"testing"
	"time"

	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

func TestValidateInit_Inline(t *testing.T) {
	env := &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		Seq:       1,
		SessionID: "sess_test",
		Timestamp: time.Now().UnixMilli(),
		Event: events.Event{Type: events.Init, Data: map[string]any{
			"version":     events.Version,
			"worker_type": "claude-code",
		}},
	}

	data, err := ValidateInit(env)
	t.Logf("err=%v type=%T nilcheck=%v", err, err, err == nil)
	if err != nil {
		t.Logf("err.Code=%v", err.Code)
	}
	t.Logf("data.WorkerType=%q", data.WorkerType)
	if data.WorkerType == "" {
		t.Error("WorkerType is empty")
	}
}
