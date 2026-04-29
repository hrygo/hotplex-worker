//go:build darwin || linux

package config

// TempBaseDir returns the base directory for temporary HotPlex files.
func TempBaseDir() string { return "/tmp/hotplex" }
