//go:build darwin || linux

package service

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeServiceFile creates parent directories and atomically writes a service
// config file (launchd plist or systemd unit) to path.
func writeServiceFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}
	return nil
}
