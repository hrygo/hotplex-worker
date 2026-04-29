//go:build darwin || linux

package output

import (
	"io"
	"os"
	"syscall"
)

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
