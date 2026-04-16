package gateway

import (
	"testing"
	"time"

	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

func TestValidateInit_Debug(t *testing.T) {
	data := map[string]any{
		"version":     events.Version,
		"worker_type": "claude-code",
	}

	env := &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		Seq:       1,
		SessionID: "sess_test",
		Timestamp: time.Now().UnixMilli(),
		Event:     events.Event{Type: events.Init, Data: data},
	}

	t.Logf("env.Event.Data type: %T", env.Event.Data)

	dataOut, ok := env.Event.Data.(map[string]any)
	t.Logf("type assertion ok: %v, data: %+v", ok, dataOut)

	result, err := ValidateInit(env)
	t.Logf("err == nil: %v", err == nil)
	t.Logf("err != nil: %v", err != nil)
	t.Logf("err type: %T, value: %v", err, err)
	if err != nil {
		t.Logf("err.Code: %v", err.Code)
	}
	t.Logf("result.WorkerType: %q", result.WorkerType)
}
