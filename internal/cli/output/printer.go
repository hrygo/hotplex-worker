package output

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/hrygo/hotplex/internal/cli"
)

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiCyan   = "\033[36m"
)

func PrintDiagnostic(out io.Writer, d cli.Diagnostic, verbose bool) {
	tty := isTTY(out)

	var symbol, color string
	switch d.Status {
	case cli.StatusPass:
		symbol, color = "✓", ansiGreen
	case cli.StatusWarn:
		symbol, color = "⚠", ansiYellow
	case cli.StatusFail:
		symbol, color = "✗", ansiRed
	default:
		symbol = "?"
	}

	cat := fmt.Sprintf("%-12s", d.Category)
	line := symbol + " " + cat + " " + d.Message
	if tty && color != "" {
		line = color + ansiBold + symbol + ansiReset + " " + cat + " " + d.Message
	}
	_, _ = fmt.Fprintln(out, line)

	if verbose && d.Detail != "" {
		detail := "  Detail: " + d.Detail
		if tty {
			detail = ansiDim + detail + ansiReset
		}
		_, _ = fmt.Fprintln(out, detail)
	}

	if d.FixHint != "" {
		hint := "  → " + d.FixHint
		if tty {
			hint = ansiDim + hint + ansiReset
		}
		_, _ = fmt.Fprintln(out, hint)
	}
}

// PrintSummary prints the summary line with pass/warn/fail counts.
func PrintSummary(out io.Writer, pass, warn, fail, fixable int) {
	tty := isTTY(out)

	total := pass + warn + fail
	summary := fmt.Sprintf("%d checks: %d passed, %d warnings, %d failed", total, pass, warn, fail)
	if fixable > 0 {
		summary += fmt.Sprintf(" (%d fixable)", fixable)
	}

	if tty {
		var color string
		switch {
		case fail > 0:
			color = ansiRed
		case warn > 0:
			color = ansiYellow
		default:
			color = ansiGreen
		}
		summary = color + ansiBold + summary + ansiReset
	}

	_, _ = fmt.Fprintln(out, summary)
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		return false
	}
	return st.Mode&syscall.S_IFMT == syscall.S_IFCHR
}
