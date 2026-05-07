package brain

import "unicode/utf8"

func truncate(s string) string {
	const maxLen = 500
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 4 {
		return s[:maxLen]
	}
	if utf8.ValidString(s) {
		runes := []rune(s)
		if len(runes) > maxLen {
			return string(runes[:maxLen-3]) + "..."
		}
		return s
	}
	return s[:maxLen-3] + "..."
}
