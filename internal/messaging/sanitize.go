// Package messaging provides shared utilities for platform messaging adapters.
package messaging

import (
	"strings"
)

// SanitizeText removes potentially problematic characters from text input.
// It sanitizes the text before it enters envelopes or card content.
//
// Sanitization rules:
//   - Removes null bytes (\x00)
//   - Removes control characters \x01-\x08, \x0B, \x0C, \x0E-\x1F
//     (preserves \t=\x09, \n=\x0A, \r=\x0D)
//   - Removes Unicode BOM (\uFEFF) at start of string
//   - Removes surrogate pair halves (\uD800-\uDFFF) and replacement chars (\uFFFD)
func SanitizeText(s string) string {
	if s == "" {
		return ""
	}

	s = strings.TrimPrefix(s, "\uFEFF")

	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		if r == 0x00 {
			continue
		}
		if r >= 0x01 && r <= 0x08 {
			continue
		}
		if r == 0x0B || r == 0x0C {
			continue
		}
		if r >= 0x0E && r <= 0x1F {
			continue
		}
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		if r == 0xFFFD {
			continue
		}

		b.WriteRune(r)
	}

	return b.String()
}

// IsControlChar reports whether r is a control character that should be
// removed by SanitizeText (excluding preserved whitespace: \t, \n, \r).
func IsControlChar(r rune) bool {
	if r == 0x00 {
		return true
	}
	if r >= 0x01 && r <= 0x08 {
		return true
	}
	if r == 0x0B || r == 0x0C {
		return true
	}
	if r >= 0x0E && r <= 0x1F {
		return true
	}
	return false
}

// IsSurrogate reports whether r is a Unicode surrogate pair half.
func IsSurrogate(r rune) bool {
	return r >= 0xD800 && r <= 0xDFFF
}

// StripBOM removes the Unicode Byte Order Mark (BOM) from the start of a string.
func StripBOM(s string) string {
	return strings.TrimPrefix(s, "\uFEFF")
}
