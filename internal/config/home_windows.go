//go:build windows

package config

func hotplexFallbackDir() string { return TempBaseDir() }
