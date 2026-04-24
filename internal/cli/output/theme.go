package output

import (
	"fmt"
	"io"
	"strings"
)

// Style wraps text with ANSI style, respecting terminal capability.
func Style(code, text string) string {
	return code + text + ansiReset
}

// Bold returns bold text.
func Bold(text string) string { return Style(ansiBold, text) }

// Dim returns dimmed text.
func Dim(text string) string { return Style(ansiDim, text) }

// Green returns green text.
func Green(text string) string { return Style(ansiGreen, text) }

// Yellow returns yellow text.
func Yellow(text string) string { return Style(ansiYellow, text) }

// Red returns red text.
func Red(text string) string { return Style(ansiRed, text) }

// Cyan returns cyan text.
func Cyan(text string) string { return Style(ansiCyan, text) }

// Sprintf applies ANSI styles and returns the formatted string with reset.
func Sprintf(style, format string, args ...any) string {
	return style + fmt.Sprintf(format, args...) + ansiReset
}

// Fprintf writes styled text to w.
func Fprintf(w io.Writer, style, format string, args ...any) {
	_, _ = fmt.Fprint(w, Sprintf(style, format, args...))
}

// StatusSymbol returns a colored status symbol.
func StatusSymbol(status string) string {
	switch status {
	case "pass":
		return Green("✓")
	case "fail":
		return Red("✗")
	case "skip":
		return Yellow("○")
	case "warn":
		return Yellow("⚠")
	default:
		return "?"
	}
}

// SectionHeader returns a formatted section header for the wizard.
func SectionHeader(title string) string {
	bar := strings.Repeat("─", 4)
	return "\n  " + Bold(fmt.Sprintf("%s %s %s", bar, title, bar)) + "\n"
}

// StepLine formats a wizard step line with status and detail.
func StepLine(name, status, detail string) string {
	sym := StatusSymbol(status)
	if detail != "" {
		return fmt.Sprintf("  %s  %-18s %s", sym, Bold(name), Dim(detail))
	}
	return fmt.Sprintf("  %s  %s", sym, Bold(name))
}

// CommandBox formats a command suggestion with styling.
func CommandBox(cmds ...string) string {
	if len(cmds) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n  " + Bold("Next steps:") + "\n")
	for i, cmd := range cmds {
		fmt.Fprintf(&b, "    %d. %s\n", i+1, Cyan(cmd))
	}
	return b.String()
}

// NoteBox renders a titled info box.
func NoteBox(title, message string) string {
	var b strings.Builder
	b.WriteString("\n  " + Bold(title) + "\n")
	b.WriteString("  " + strings.Repeat("─", 40) + "\n")
	for _, line := range strings.Split(strings.TrimRight(message, "\n"), "\n") {
		b.WriteString("    " + line + "\n")
	}
	return b.String()
}

// ConfigLine formats a configuration status line.
func ConfigLine(label, status string) string {
	return fmt.Sprintf("    %-8s %s", label+":", status)
}

// Fdivider writes a horizontal rule to w.
func Fdivider(w io.Writer) {
	_, _ = fmt.Fprintln(w, "  "+strings.Repeat("─", 45))
}
