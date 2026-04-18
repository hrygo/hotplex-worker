// Package feishu provides abort command detection for Feishu messages.
package feishu

import "strings"

// abortTriggers is a set of normalized trigger words for abort detection.
// Source: OpenClaw abort-detect.ts (core triggers, covering English/Chinese/Japanese).
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

// IsAbortCommand checks if the message text is an abort command.
// Normalization: trim → lowercase → strip trailing punctuation.
func IsAbortCommand(text string) bool {
	t := strings.TrimSpace(strings.ToLower(text))
	t = strings.TrimRight(t, ".!?…,，。;；:：\"')]")
	return abortTriggers[t]
}
