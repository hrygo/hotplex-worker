package client

import (
	"testing"
)

func TestEventAsHelpers(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		validate func(*testing.T, Event)
	}{
		{
			name: "AsDoneData",
			event: Event{
				Type: EventDone,
				Data: map[string]any{
					"success": true,
					"stats": map[string]any{
						"model": "claude-3-opus",
					},
				},
			},
			validate: func(t *testing.T, e Event) {
				d, ok := e.AsDoneData()
				if !ok {
					t.Fatal("expected ok")
				}
				if !d.Success {
					t.Error("expected Success=true")
				}
				if d.Stats["model"] != "claude-3-opus" {
					t.Errorf("expected model=claude-3-opus, got %v", d.Stats["model"])
				}
			},
		},
		{
			name: "AsErrorData",
			event: Event{
				Type: EventError,
				Data: map[string]any{
					"code":    "SESSION_NOT_FOUND",
					"message": "not found",
				},
			},
			validate: func(t *testing.T, e Event) {
				d, ok := e.AsErrorData()
				if !ok {
					t.Fatal("expected ok")
				}
				if d.Code != "SESSION_NOT_FOUND" {
					t.Errorf("expected code=SESSION_NOT_FOUND, got %s", d.Code)
				}
			},
		},
		{
			name: "AsToolCallData",
			event: Event{
				Type: EventToolCall,
				Data: map[string]any{
					"id":   "tc_123",
					"name": "bash",
					"input": map[string]any{
						"command": "ls",
					},
				},
			},
			validate: func(t *testing.T, e Event) {
				d, ok := e.AsToolCallData()
				if !ok {
					t.Fatal("expected ok")
				}
				if d.ID != "tc_123" {
					t.Errorf("expected ID=tc_123, got %s", d.ID)
				}
				if d.Input["command"] != "ls" {
					t.Errorf("expected command=ls, got %v", d.Input["command"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.validate(t, tt.event)
		})
	}
}
