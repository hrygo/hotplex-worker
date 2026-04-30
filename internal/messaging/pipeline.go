package messaging

import (
	"strings"
	"sync"
)

// Source: OpenClaw abort-detect.ts (core triggers, covering English/Chinese/Japanese/Russian).
var abortTriggers = map[string]bool{
	"stop": true, "abort": true, "halt": true, "cancel": true,
	"wait": true, "exit": true, "interrupt": true,
	"please stop": true, "stop please": true,
	"停止": true, "取消": true, "中断": true, "等一下": true,
	"别说了": true, "停下来": true,
	"やめて": true, "止めて": true,
	"стоп": true,
}

var abortTriggersMu sync.RWMutex

func RegisterAbortTrigger(word string) {
	abortTriggersMu.Lock()
	abortTriggers[word] = true
	abortTriggersMu.Unlock()
}

// Normalization: trim → lowercase → strip trailing punctuation.
func IsAbortCommand(text string) bool {
	t := strings.TrimSpace(strings.ToLower(text))
	t = trimTrailingPunct(t)
	abortTriggersMu.RLock()
	ok := abortTriggers[t]
	abortTriggersMu.RUnlock()
	return ok
}

type CommandAction int

const (
	CmdNone CommandAction = iota
	CmdAbort
	CmdHelp
	CmdControl
	CmdWorker
	CmdPassthrough
)

type CommandResult struct {
	Action  CommandAction
	Control *ControlCommandResult
	Worker  *WorkerCommandResult
}

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
