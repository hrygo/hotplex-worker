package slack

import "strings"

var abortTriggers = map[string]bool{
	// English
	"stop": true, "abort": true, "halt": true, "cancel": true,
	"wait": true, "exit": true,
	"please stop": true, "stop please": true,
	// Chinese
	"停止": true, "取消": true, "中断": true, "等一下": true,
	"别说了": true, "停下来": true,
}

// IsAbortCommand checks if text is an abort trigger.
func IsAbortCommand(text string) bool {
	t := strings.TrimSpace(strings.ToLower(text))
	t = trimTrailingPunct(t)
	return abortTriggers[t]
}

func trimTrailingPunct(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool {
		switch r {
		case '.', '!', '?', ',', ';', ':', '"', '\'', ')', ']',
			'…', '，', '。', '；', '：':
			return true
		}
		return false
	})
}
