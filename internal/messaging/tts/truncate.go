package tts

import "strings"

// TruncateText truncates s to maxLen runes. When truncated, it scans backward
// for the last sentence-ending punctuation (period, question mark, exclamation,
// or comma) to produce a grammatically coherent fragment. Falls back to raw
// truncation if no punctuation is found.
func TruncateText(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 0 {
		return ""
	}
	// Scan backward for a sentence-ending character.
	for i := maxLen - 1; i >= 0; i-- {
		switch runes[i] {
		case '。', '？', '！', '，', '.', '?', '!', ',':
			return strings.TrimSpace(string(runes[:i+1]))
		}
	}
	// No punctuation found — raw truncation.
	return string(runes[:maxLen])
}
