//go:build !windows

package main

func isServiceRun() bool {
	return false
}

func extractServiceConfig() string { return "" }

func runAsWindowsService(string) {}
