package messaging

import (
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// BuildControlEnvelope creates a control event envelope from a parsed command result.
func BuildControlEnvelope(result *ControlCommandResult, sessionID, userID string) *events.Envelope {
	ctrlData := events.ControlData{Action: result.Action}
	if result.Arg != "" {
		ctrlData.Details = map[string]any{"path": result.Arg}
	}
	return &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		SessionID: sessionID,
		Event: events.Event{
			Type: events.Control,
			Data: ctrlData,
		},
		OwnerID: userID,
	}
}

// BuildWorkerCommandEnvelope creates a worker command envelope from a parsed command result.
func BuildWorkerCommandEnvelope(result *WorkerCommandResult, sessionID, userID string) *events.Envelope {
	return &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		SessionID: sessionID,
		Event: events.Event{
			Type: events.WorkerCmd,
			Data: events.WorkerCommandData{
				Command: result.Command,
				Args:    result.Args,
				Extra:   result.Extra,
			},
		},
		OwnerID: userID,
	}
}

// ControlFeedbackCN maps control actions to Chinese feedback messages.
var ControlFeedbackCN = map[events.ControlAction]string{
	events.ControlActionGC:    "✅ 会话已休眠，发消息即可恢复。",
	events.ControlActionReset: "✅ 上下文已重置。",
	events.ControlActionCD:    "📁 正在切换工作目录...",
}

// ControlFeedbackEN maps control actions to English feedback messages.
var ControlFeedbackEN = map[events.ControlAction]string{
	events.ControlActionGC:    "🗑️ Session parked. Send a message to resume.",
	events.ControlActionReset: "🔄 Context reset.",
	events.ControlActionCD:    "📁 Switching work directory...",
}

// ControlFeedbackMessage returns the feedback message for a control action,
// using the provided locale map. Returns fallback if action is not in the map.
func ControlFeedbackMessage(action events.ControlAction, msgs map[events.ControlAction]string, fallback string) string {
	if msg, ok := msgs[action]; ok {
		return msg
	}
	return fallback
}
