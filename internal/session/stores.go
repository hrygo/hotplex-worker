package session

import (
	"fmt"
	"os"
	"path/filepath"
)

// ensureDBDir creates the parent directory of dbPath if it does not exist.
// This is a simple wrapper around os.MkdirAll which is idempotent and fast
// for existing directories (typically one stat syscall). We intentionally
// don't cache results to support multiple database paths in tests and future
// multi-tenancy scenarios.
func ensureDBDir(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("session store: create db dir: %w", err)
		}
	}
	return nil
}
