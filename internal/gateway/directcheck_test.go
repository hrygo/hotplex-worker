package gateway

import (
	"testing"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestValidateInit_DirectCheck(t *testing.T) {
	data := map[string]any{
		"version":     events.Version,
		"worker_type": "claude-code",
	}

	env := envFromData(data)
	initData, err := ValidateInit(env)

	if err != nil {
		t.Errorf("ValidateInit returned non-nil error: %v (type=%T)", err, err)
	}
	if initData.WorkerType == "" {
		t.Errorf("WorkerType is empty, want non-empty")
	}
	t.Logf("err=%v, WorkerType=%q", err, initData.WorkerType)
}
