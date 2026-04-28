package session

import (
	"fmt"
	"os"
	"path/filepath"
)

// ensureDBDir creates the parent directory of dbPath if it does not exist.
func ensureDBDir(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("session store: create db dir: %w", err)
		}
	}
	return nil
}
