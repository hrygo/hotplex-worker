//go:build windows

package config

import (
	"os"
	"path/filepath"
)

func hotplexFallbackDir() string { return filepath.Join(os.TempDir(), "hotplex") }
