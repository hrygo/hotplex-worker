//go:build windows

package config

import (
	"os"
	"path/filepath"
)

// TempBaseDir returns the base directory for temporary HotPlex files.
func TempBaseDir() string { return filepath.Join(os.TempDir(), "hotplex") }
