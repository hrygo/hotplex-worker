//go:build windows

package croncli

import "fmt"

func sendReloadSignal(pid int) error {
	return fmt.Errorf("SIGHUP not supported on Windows; restart gateway manually")
}
