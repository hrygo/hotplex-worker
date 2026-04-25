package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStyle(t *testing.T) {
	t.Parallel()

	got := Style("\033[1m", "hello")
	require.Equal(t, "\033[1mhello\033[0m", got)
}

func TestColorStyles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   func(string) string
		code string
	}{
		{"Bold", Bold, ansiBold},
		{"Dim", Dim, ansiDim},
		{"Green", Green, ansiGreen},
		{"Yellow", Yellow, ansiYellow},
		{"Red", Red, ansiRed},
		{"Cyan", Cyan, ansiCyan},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.fn("text")
			require.Contains(t, got, tt.code)
			require.Contains(t, got, "text")
			require.True(t, strings.HasSuffix(got, ansiReset))
		})
	}
}

func TestSprintf(t *testing.T) {
	t.Parallel()

	got := Sprintf(ansiBold, "count: %d, name: %s", 42, "test")
	require.Contains(t, got, ansiBold)
	require.Contains(t, got, "count: 42, name: test")
	require.True(t, strings.HasSuffix(got, ansiReset))
}

func TestFprintf(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	Fprintf(&buf, ansiGreen, "value=%d", 7)

	got := buf.String()
	require.Contains(t, got, ansiGreen)
	require.Contains(t, got, "value=7")
	require.Contains(t, got, ansiReset)
}

func TestStatusSymbol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   string
	}{
		{"pass", "✓"},
		{"fail", "✗"},
		{"skip", "○"},
		{"warn", "⚠"},
		{"unknown", "?"},
		{"", "?"},
	}

	for _, tc := range tests {
		got := StatusSymbol(tc.status)
		require.Contains(t, got, tc.want, "status=%q", tc.status)
	}

	// Verify pass/fail/skip/warn carry ANSI codes
	require.Contains(t, StatusSymbol("pass"), ansiGreen)
	require.Contains(t, StatusSymbol("fail"), ansiRed)
	require.Contains(t, StatusSymbol("skip"), ansiYellow)
	require.Contains(t, StatusSymbol("warn"), ansiYellow)
}

func TestSectionHeader(t *testing.T) {
	t.Parallel()

	got := SectionHeader("Diagnostics")
	require.Contains(t, got, "\n")
	require.Contains(t, got, "Diagnostics")
	require.Contains(t, got, ansiBold)
	require.Contains(t, got, ansiReset)
	require.Contains(t, got, "────")

	// Must end with newline
	require.True(t, strings.HasSuffix(got, "\n"))
}

func TestStepLine(t *testing.T) {
	t.Parallel()

	t.Run("with_detail", func(t *testing.T) {
		t.Parallel()

		got := StepLine("Config", "pass", "loaded from /etc")
		require.Contains(t, got, "✓")
		require.Contains(t, got, "Config")
		require.Contains(t, got, "loaded from /etc")
		require.Contains(t, got, ansiBold)
		require.Contains(t, got, ansiDim)
	})

	t.Run("without_detail", func(t *testing.T) {
		t.Parallel()

		got := StepLine("Network", "fail", "")
		require.Contains(t, got, "✗")
		require.Contains(t, got, "Network")
		require.NotContains(t, got, ansiDim)
	})
}

func TestCommandBox(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "", CommandBox())
	})

	t.Run("single", func(t *testing.T) {
		t.Parallel()

		got := CommandBox("hotplex start")
		require.Contains(t, got, "Next steps:")
		require.Contains(t, got, "1.")
		require.Contains(t, got, "hotplex start")
		require.Contains(t, got, ansiCyan)
	})

	t.Run("multiple", func(t *testing.T) {
		t.Parallel()

		got := CommandBox("step one", "step two", "step three")
		require.Contains(t, got, "1.")
		require.Contains(t, got, "2.")
		require.Contains(t, got, "3.")
		require.Contains(t, got, "step one")
		require.Contains(t, got, "step two")
		require.Contains(t, got, "step three")
	})
}

func TestNoteBox(t *testing.T) {
	t.Parallel()

	t.Run("single_line", func(t *testing.T) {
		t.Parallel()

		got := NoteBox("Tip", "Check your config")
		require.Contains(t, got, "Tip")
		require.Contains(t, got, "Check your config")
		require.Contains(t, got, strings.Repeat("─", 40))
	})

	t.Run("multi_line", func(t *testing.T) {
		t.Parallel()

		got := NoteBox("Note", "line1\nline2\nline3")
		require.Contains(t, got, "line1")
		require.Contains(t, got, "line2")
		require.Contains(t, got, "line3")
		// Each line indented
		for _, line := range []string{"line1", "line2", "line3"} {
			require.Contains(t, got, "    "+line)
		}
	})

	t.Run("trailing_newline_trimmed", func(t *testing.T) {
		t.Parallel()

		got := NoteBox("Title", "content\n\n")
		// TrimRight strips trailing newlines from message before splitting
		require.Contains(t, got, "content")
	})
}

func TestConfigLine(t *testing.T) {
	t.Parallel()

	got := ConfigLine("Port", "8080")
	require.Contains(t, got, "Port:")
	require.Contains(t, got, "8080")
	// Label is left-aligned with width 8
	require.Contains(t, got, "Port:   ")
}

func TestFdivider(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	Fdivider(&buf)

	got := buf.String()
	require.Contains(t, got, strings.Repeat("─", 45))
	require.True(t, strings.HasSuffix(got, "\n"))
}
