package output

import (
	"io"
	"os"

	isatty "github.com/mattn/go-isatty"
)

// IsTTY reports whether the writer is a terminal (TTY).
func IsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}
