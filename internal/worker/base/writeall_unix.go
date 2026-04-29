//go:build darwin || linux

package base

import (
	"fmt"
	"runtime"
	"syscall"
)

// WriteAll loops syscall.Write until all data is written, handling partial
// writes and EAGAIN (non-blocking pipe on macOS). Go's stdlib File.Write does
// not retry EAGAIN for syscall-backed files, so we must use raw syscall.
func WriteAll(fd int, data []byte) error {
	n := 0
	for n < len(data) {
		nn, err := syscall.Write(fd, data[n:])
		if err != nil {
			if err == syscall.EAGAIN {
				runtime.Gosched()
				continue
			}
			return err
		}
		if nn == 0 {
			return fmt.Errorf("writeAll: zero write")
		}
		n += nn
	}
	return nil
}
