package security

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AllowedBaseDirs is the set of permitted base directories for session work dirs.
var AllowedBaseDirs = map[string]bool{
	"/var/hotplex/projects": true,
	"/tmp/hotplex":          true,
}

// ValidateBaseDir checks that the base directory is in the allowed list.
func ValidateBaseDir(baseDir string) error {
	if !AllowedBaseDirs[baseDir] {
		return fmt.Errorf("security: base directory %q not in whitelist", baseDir)
	}
	return nil
}

// ForbiddenWorkDirs are system directories that must never be used as session work dirs.
// Both exact matches and sub-paths are rejected.
//
// Sources:
//   - Linux FHS 3.0: /bin, /sbin, /usr, /etc, /boot, /lib, /root, /srv
//   - macOS SIP:     /System, /usr, /bin, /sbin
//   - systemd:       /run (ProtectSystem), /home (ProtectHome)
//   - POSIX virtual: /dev, /proc, /sys
//
// Notable exclusions:
//   - /opt: widely used for user software (Homebrew, third-party tools)
//   - /var: allowed; hotplex workspace uses /var/hotplex/projects
//   - /tmp: allowed; hotplex default uses /tmp/hotplex/workspace
//   - /snap, /flatpak: package-managed app dirs, not system-critical
var ForbiddenWorkDirs = []string{
	"/bin",    // FHS: essential user binaries
	"/sbin",   // FHS: essential system binaries
	"/usr",    // FHS: system-wide read-only programs & libraries
	"/etc",    // FHS: system configuration
	"/boot",   // FHS: kernel & bootloader
	"/lib",    // FHS: shared libraries
	"/lib64",  // FHS: 64-bit shared libraries
	"/root",   // FHS: superuser home (systemd ProtectHome)
	"/home",   // FHS: user homes (systemd ProtectHome)
	"/System", // macOS SIP: system files
	"/dev",    // POSIX: device files
	"/proc",   // Linux: process & kernel info
	"/sys",    // Linux: kernel objects
	"/run",    // FHS: runtime data (PID files, sockets, locks)
	"/srv",    // FHS: service data
}

// ValidateWorkDir validates that a work directory is safe for worker execution.
//
// Rules:
//  1. Must be an absolute path.
//  2. Must be clean (no ".." components).
//  3. Must not be or reside under a forbidden system directory.
//  4. Symlinks are resolved and the real path is also checked against the blacklist.
func ValidateWorkDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("security: work dir must not be empty")
	}

	cleaned := filepath.Clean(dir)

	if !filepath.IsAbs(cleaned) {
		return fmt.Errorf("security: work dir must be absolute: %s", dir)
	}

	if err := checkForbidden(cleaned); err != nil {
		return err
	}

	// Resolve symlinks and check the real path too.
	realPath, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		// Directory doesn't exist yet — that's OK, we already validated the logical path.
		return nil
	}
	return checkForbidden(realPath)
}

// checkForbidden returns an error if path is exactly or under a forbidden directory.
func checkForbidden(path string) error {
	// Reject root itself — no process should use / as its working directory.
	if path == "/" {
		return fmt.Errorf("security: work dir %q is a forbidden system directory", path)
	}

	for _, forbidden := range ForbiddenWorkDirs {
		if path == forbidden {
			return fmt.Errorf("security: work dir %q is a forbidden system directory", path)
		}
		// Sub-path check: "/home/foo" starts with "/home/".
		if strings.HasPrefix(path, forbidden+"/") {
			return fmt.Errorf("security: work dir %q is under forbidden directory %q", path, forbidden)
		}
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
