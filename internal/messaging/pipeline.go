package messaging

import (
	"strings"
	"sync"
	"time"
)

// abortTriggers is a set of normalized trigger words for abort detection.
// Source: OpenClaw abort-detect.ts (core triggers, covering English/Chinese/Japanese/Russian).
var abortTriggers = map[string]bool{
	// English
	"stop": true, "abort": true, "halt": true, "cancel": true,
	"wait": true, "exit": true, "interrupt": true,
	"please stop": true, "stop please": true,
	// Chinese
	"停止": true, "取消": true, "中断": true, "等一下": true,
	"别说了": true, "停下来": true,
	// Japanese
	"やめて": true, "止めて": true,
	// Russian
	"стоп": true,
}

// abortTriggersMu protects abortTriggers for concurrent access.
var abortTriggersMu sync.RWMutex

// RegisterAbortTrigger adds a custom abort trigger word.
func RegisterAbortTrigger(word string) {
	abortTriggersMu.Lock()
	abortTriggers[word] = true
	abortTriggersMu.Unlock()
}

// IsAbortCommand checks if the message text is an abort command.
// Normalization: trim → lowercase → strip trailing punctuation.
func IsAbortCommand(text string) bool {
	t := strings.TrimSpace(strings.ToLower(text))
	t = strings.TrimRight(t, ".!?…,，。;；:：\"')]")
	abortTriggersMu.RLock()
	ok := abortTriggers[t]
	abortTriggersMu.RUnlock()
	return ok
}

// ParsedMessage is the normalized representation of a platform message,
// produced by adapter-specific parsing and consumed by the shared pipeline.
type ParsedMessage struct {
	PlatformMsgID string
	ChannelID     string // Slack: channelID, Feishu: chatID
	ThreadKey     string // Slack: threadTS, Feishu: threadKey
	UserID        string
	TeamID        string // Slack-only, empty for Feishu
	Text          string // raw text, before sanitization
	ChatType      string // normalized: "im", "mpim", "group", "channel", "p2p"
	BotMentioned  bool
	CreateTime    time.Time
}

// CommandAction indicates what command was detected in a message.
type CommandAction int

const (
	CmdNone        CommandAction = iota // no command detected
	CmdAbort                            // abort/stop command
	CmdHelp                             // help command
	CmdControl                          // control command (/gc, /reset, /cd, $休眠, etc.)
	CmdWorker                           // worker command (/context, /mcp, /model, /perm)
	CmdPassthrough                      // passthrough worker command → forward to worker
)

// CommandResult holds the detected command details.
type CommandResult struct {
	Action  CommandAction
	Control *ControlCommandResult
	Worker  *WorkerCommandResult
}

// DetectCommand checks for commands in the message text.
// Returns the detected command type and any associated result data.
func DetectCommand(text string) CommandResult {
	if IsAbortCommand(text) {
		return CommandResult{Action: CmdAbort}
	}
	if IsHelpCommand(text) {
		return CommandResult{Action: CmdHelp}
	}
	if ctrl := ParseControlCommand(text); ctrl != nil {
		return CommandResult{Action: CmdControl, Control: ctrl}
	}
	if wc := ParseWorkerCommand(text); wc != nil {
		if wc.Command.IsPassthrough() {
			return CommandResult{Action: CmdPassthrough, Worker: wc}
		}
		return CommandResult{Action: CmdWorker, Worker: wc}
	}
	return CommandResult{Action: CmdNone}
}

// ValidateMessage performs shared validation: text sanitization + access control gate.
// Mutates msg.Text (sanitizes in place). Returns false if the message is rejected.
func (a *PlatformAdapter) ValidateMessage(msg *ParsedMessage) bool {
	msg.Text = SanitizeText(msg.Text)

	if a.Gate != nil {
		isDM := msg.ChatType == "im" || msg.ChatType == "p2p"
		result := a.Gate.Check(isDM, msg.UserID, msg.BotMentioned)
		if !result.Allowed {
			a.Log.Debug("gate rejected message",
				"reason", result.Reason,
				"user", msg.UserID,
				"channel", msg.ChannelID)
			return false
		}
	}
	return true
}
