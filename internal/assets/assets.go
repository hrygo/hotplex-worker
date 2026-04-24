package assets

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hrygo/hotplex/scripts"
)

// InstallScripts extracts all embedded Python scripts to the target directory.
// It ensures the scripts are available on the filesystem for the Python interpreter.
// If a script already exists, it is overwritten to ensure the version matches
// the current binary.
func InstallScripts(destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("assets: mkdir %s: %w", destDir, err)
	}

	entries, err := fs.ReadDir(scripts.FS, ".")
	if err != nil {
		return fmt.Errorf("assets: read embed fs: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".py" {
			continue
		}

		name := entry.Name()
		data, err := scripts.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("assets: read embed file %s: %w", name, err)
		}

		destPath := filepath.Join(destDir, name)
		// WriteFile with 0755 to ensure scripts are executable.
		if err := os.WriteFile(destPath, data, 0o755); err != nil {
			return fmt.Errorf("assets: write %s: %w", destPath, err)
		}
	}

	return nil
}
