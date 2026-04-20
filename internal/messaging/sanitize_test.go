package messaging

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// AC-4.1: Null bytes removal
		{
			name:     "null_bytes_removed",
			input:    "hello\x00world",
			expected: "helloworld",
		},
		{
			name:     "multiple_null_bytes",
			input:    "\x00hello\x00world\x00",
			expected: "helloworld",
		},

		// AC-4.2: Control characters removal
		{
			name:     "control_chars_01_08_removed",
			input:    "hello\x01\x02\x03\x04\x05\x06\x07\x08world",
			expected: "helloworld",
		},
		{
			name:     "control_chars_0B_0C_removed",
			input:    "hello\x0B\x0Cworld",
			expected: "helloworld",
		},
		{
			name:     "control_chars_0E_1F_removed",
			input:    "hello\x0E\x0F\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1A\x1B\x1C\x1D\x1E\x1Fworld",
			expected: "helloworld",
		},

		// AC-4.3: Preserved whitespace
		{
			name:     "tab_preserved",
			input:    "hello\tworld",
			expected: "hello\tworld",
		},
		{
			name:     "newline_preserved",
			input:    "line1\nline2",
			expected: "line1\nline2",
		},
		{
			name:     "carriage_return_preserved",
			input:    "line1\r\nline2",
			expected: "line1\r\nline2",
		},
		{
			name:     "all_whitespace_preserved",
			input:    "line1\nline2\ttab",
			expected: "line1\nline2\ttab",
		},

		// AC-4.4: BOM removal
		{
			name:     "bom_at_start_removed",
			input:    "\uFEFFhello world",
			expected: "hello world",
		},
		{
			name:     "bom_only_string",
			input:    "\uFEFF",
			expected: "",
		},
		{
			name:     "bom_in_middle_preserved",
			input:    "hello\uFEFFworld",
			expected: "hello\uFEFFworld",
		},

		// AC-4.5: Surrogate pair halves removal (Go replaces surrogates with U+FFFD)
		{
			name:     "replacement_char_removed",
			input:    "hello\uFFFDworld",
			expected: "helloworld",
		},
		{
			name:     "multiple_replacement_chars_removed",
			input:    "hello\uFFFD\uFFFD\uFFFDworld",
			expected: "helloworld",
		},

		// Edge cases
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
		{
			name:     "only_whitespace_preserved",
			input:    " \t\n\r ",
			expected: " \t\n\r ",
		},
		{
			name:     "unicode_text_preserved",
			input:    "Hello 世界 🌍 émojis",
			expected: "Hello 世界 🌍 émojis",
		},
		{
			name:     "mixed_control_and_text",
			input:    "\x00\x01start\x0B\x0Cmiddle\x0E\x1Fend\x00",
			expected: "startmiddleend",
		},
		{
			name:     "bom_with_control_chars",
			input:    "\uFEFF\x00hello\x01\x02\x03world\uFEFF",
			expected: "helloworld\uFEFF",
		},
		{
			name:     "normal_text_unchanged",
			input:    "The quick brown fox jumps over the lazy dog.",
			expected: "The quick brown fox jumps over the lazy dog.",
		},
		{
			name:     "code_with_tabs_and_newlines",
			input:    "func main() {\n\tprintln(\"hello\")\n}",
			expected: "func main() {\n\tprintln(\"hello\")\n}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeText(tt.input)
			require.Equal(t, tt.expected, result, "SanitizeText(%q) = %q, want %q", tt.input, result, tt.expected)
		})
	}
}

func TestSanitizeText_ACVerification(t *testing.T) {
	// AC-4.1: Null bytes removed
	t.Run("AC-4.1_null_bytes", func(t *testing.T) {
		result := SanitizeText("hello\x00world")
		require.Equal(t, "helloworld", result)
	})

	// AC-4.2: Control chars removed
	t.Run("AC-4.2_control_chars", func(t *testing.T) {
		// Test all control chars that should be removed
		for r := rune(0x01); r <= 0x08; r++ {
			input := string(r)
			result := SanitizeText(input)
			require.Empty(t, result, "control char 0x%02X should be removed", r)
		}
		for _, r := range []rune{0x0B, 0x0C} {
			input := string(r)
			result := SanitizeText(input)
			require.Empty(t, result, "control char 0x%02X should be removed", r)
		}
		for r := rune(0x0E); r <= 0x1F; r++ {
			input := string(r)
			result := SanitizeText(input)
			require.Empty(t, result, "control char 0x%02X should be removed", r)
		}
	})

	// AC-4.3: Preserved whitespace
	t.Run("AC-4.3_preserved_whitespace", func(t *testing.T) {
		input := "line1\nline2\ttab"
		result := SanitizeText(input)
		require.Equal(t, input, result)
	})

	// AC-4.4: BOM removal
	t.Run("AC-4.4_bom_removal", func(t *testing.T) {
		result := SanitizeText("\uFEFF hello")
		require.Equal(t, " hello", result)
	})

	// AC-4.5: Surrogate halves removal (Go replaces with U+FFFD which we also remove)
	t.Run("AC-4.5_replacement_char", func(t *testing.T) {
		result := SanitizeText("\uFFFD")
		require.Empty(t, result, "replacement character should be removed")
	})
}

func TestIsControlChar(t *testing.T) {
	tests := []struct {
		r        rune
		expected bool
	}{
		{0x00, true},  // null
		{0x01, true},  // SOH
		{0x08, true},  // BS
		{0x09, false}, // TAB (preserved)
		{0x0A, false}, // LF (preserved)
		{0x0B, true},  // VT
		{0x0C, true},  // FF
		{0x0D, false}, // CR (preserved)
		{0x0E, true},  // SO
		{0x1F, true},  // US
		{0x20, false}, // space
		{'a', false},
		{'世', false},
	}

	for _, tt := range tests {
		t.Run(string(tt.r), func(t *testing.T) {
			result := IsControlChar(tt.r)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsSurrogate(t *testing.T) {
	tests := []struct {
		r        rune
		expected bool
	}{
		{0xD7FF, false}, // just before surrogate range
		{0xD800, true},  // start of high surrogates
		{0xDBFF, true},  // end of high surrogates
		{0xDC00, true},  // start of low surrogates
		{0xDFFF, true},  // end of low surrogates
		{0xE000, false}, // just after surrogate range
		{'a', false},
		{'世', false},
	}

	for _, tt := range tests {
		t.Run(string(tt.r), func(t *testing.T) {
			result := IsSurrogate(tt.r)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestStripBOM(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"with_bom", "\uFEFFhello", "hello"},
		{"without_bom", "hello", "hello"},
		{"only_bom", "\uFEFF", ""},
		{"empty", "", ""},
		{"bom_in_middle", "hello\uFEFFworld", "hello\uFEFFworld"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripBOM(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeText_Performance(t *testing.T) {
	// Test with a large string to ensure performance is acceptable
	largeString := strings.Repeat("Hello world! ", 10000)
	result := SanitizeText(largeString)
	require.Equal(t, largeString, result)
}
