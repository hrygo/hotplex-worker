//go:build windows

package base

import (
	"fmt"
	"syscall"
)

// WriteAll loops syscall.Write until all data is written.
// Windows pipes do not produce EAGAIN, so no retry logic is needed.
func WriteAll(fd int, data []byte) error {
	n := 0
	for n < len(data) {
		nn, err := syscall.Write(syscall.Handle(fd), data[n:])
		if err != nil {
			return err
		}
		if nn == 0 {
			return fmt.Errorf("writeAll: zero write")
		}
		n += nn
	}
	return nil
}
