package security

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AllowedBaseDirs is the set of permitted base directories for session work dirs.
var AllowedBaseDirs = map[string]bool{
	"/var/hotplex/projects": true,
	"/tmp/hotplex":           true,
}

// ValidateBaseDir checks that the base directory is in the allowed list.
func ValidateBaseDir(baseDir string) error {
	if !AllowedBaseDirs[baseDir] {
		return fmt.Errorf("security: base directory %q not in whitelist", baseDir)
	}
	return nil
}

// SafePathJoin safely joins a base directory with a user-provided path,
// preventing path traversal attacks.
//
// Security guarantees:
//  1. Rejects absolute paths from user input.
//  2. Resolves all symlinks via filepath.EvalSymlinks.
//  3. Verifies the resolved path is still within baseDir.
func SafePathJoin(baseDir, userPath string) (string, error) {
	// Reject absolute paths — they bypass baseDir entirely.
	if filepath.IsAbs(userPath) {
		return "", fmt.Errorf("security: absolute paths not allowed: %s", userPath)
	}

	// Clean the user path.
	clean := filepath.Clean(userPath)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("security: traversal attempt detected: %s", userPath)
	}

	// Join with baseDir.
	joined := filepath.Join(baseDir, clean)

	// Resolve symlinks in the joined path.
	realPath, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", fmt.Errorf("security: path error: %w", err)
	}

	// Resolve symlinks in baseDir.
	realBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", fmt.Errorf("security: base dir error: %w", err)
	}

	// Verify resolved path is inside baseDir.
	realBase = strings.TrimSuffix(realBase, string(filepath.Separator))
	if !strings.HasPrefix(realPath, realBase+string(filepath.Separator)) {
		return "", fmt.Errorf("security: path escapes base directory: %s", userPath)
	}

	return realPath, nil
}
