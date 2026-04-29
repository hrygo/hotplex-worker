//go:build darwin || linux

package config

func hotplexFallbackDir() string { return TempBaseDir() }
