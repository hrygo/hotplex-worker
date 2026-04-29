//go:build windows

package output

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

func IsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	ft, err := windows.GetFileType(windows.Handle(f.Fd()))
	if err != nil {
		return false
	}
	return ft == windows.FILE_TYPE_CHAR
}
