//go:build darwin || linux

package security

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/hrygo/hotplex/internal/config"
)

var (
	allowedBaseDirs     = make(map[string]bool)
	forbiddenWorkDirs   = make([]string, 0)
	securityConfigMutex sync.RWMutex
)

func init() {
	// Initialize with system defaults
	allowedBaseDirs[config.TempBaseDir()] = true
	forbiddenWorkDirs = []string{
		"/bin",    // FHS: essential user binaries
		"/sbin",   // FHS: essential system binaries
		"/usr",    // FHS: system-wide read-only programs & libraries
		"/etc",    // FHS: system configuration
		"/boot",   // FHS: kernel & bootloader
		"/lib",    // FHS: shared libraries
		"/lib64",  // FHS: 64-bit shared libraries
		"/root",   // FHS: superuser home (systemd ProtectHome)
		"/home",   // FHS: user homes (systemd ProtectHome) - but allow subdirectories via initUserHomeDirs()
		"/System", // macOS SIP: system files
		"/dev",    // POSIX: device files
		"/proc",   // Linux: process & kernel info
		"/sys",    // Linux: kernel objects
		"/run",    // FHS: runtime data (PID files, sockets, locks)
		"/srv",    // FHS: service data
	}

	// Initialize user home directory patterns (program convention)
	initUserHomeDirs()

	// Production base directory (if exists)
	if _, err := os.Stat("/var/hotplex/projects"); err == nil {
		allowedBaseDirs["/var/hotplex/projects"] = true
	}
}

// initUserHomeDirs adds common user home directory patterns to the whitelist.
// This implements "convention over configuration" - reasonable defaults that work for most developers.
func initUserHomeDirs() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}

	// Common project directory patterns under user home
	patterns := []string{
		"workspace",       // Common convention: ~/workspace
		"projects",        // Common convention: ~/projects
		"work",           // Common convention: ~/work
		"dev",            // Common convention: ~/dev
		".hotplex/workspace", // HotPlex convention: ~/.hotplex/workspace
	}

	for _, pattern := range patterns {
		fullPath := filepath.Join(homeDir, pattern)
		allowedBaseDirs[fullPath] = true
	}
}

// ConfigureFromConfig applies security settings from the configuration file.
// This implements "configuration as supplement" - user overrides for special cases.
// Whitelist (allowed) takes priority over blacklist (forbidden).
func ConfigureFromConfig(cfg *config.SecurityConfig) {
	securityConfigMutex.Lock()
	defer securityConfigMutex.Unlock()

	// Apply extra whitelist patterns from config (highest priority)
	for _, pattern := range cfg.WorkDirAllowedBasePatterns {
		expandedPath := pattern
		// Expand ~ to user home directory
		if len(pattern) > 0 && pattern[0] == '~' {
			homeDir, err := os.UserHomeDir()
			if err == nil {
				if len(pattern) == 1 || pattern[1] == '/' {
					expandedPath = filepath.Join(homeDir, pattern[1:])
				}
			}
		}
		// Expand environment variables
		expandedPath = os.ExpandEnv(expandedPath)
		if expandedPath != "" {
			allowedBaseDirs[expandedPath] = true
		}
	}

	// Apply extra blacklist directories from config (lower priority than whitelist)
	for _, dir := range cfg.WorkDirForbiddenDirs {
		expandedPath := dir
		// Expand environment variables
		expandedPath = os.ExpandEnv(expandedPath)
		if expandedPath != "" {
			// Only add to forbidden if not explicitly allowed (whitelist priority)
			if !allowedBaseDirs[expandedPath] {
				forbiddenWorkDirs = append(forbiddenWorkDirs, expandedPath)
			}
		}
	}
}

// GetAllowedBaseDirs returns a copy of the current allowed base directories map.
// Used for testing and diagnostics.
func GetAllowedBaseDirs() map[string]bool {
	securityConfigMutex.RLock()
	defer securityConfigMutex.RUnlock()

	result := make(map[string]bool, len(allowedBaseDirs))
	for k, v := range allowedBaseDirs {
		result[k] = v
	}
	return result
}

// GetForbiddenWorkDirs returns a copy of the current forbidden work directories slice.
// Used for testing and diagnostics.
func GetForbiddenWorkDirs() []string {
	securityConfigMutex.RLock()
	defer securityConfigMutex.RUnlock()

	result := make([]string, len(forbiddenWorkDirs))
	copy(result, forbiddenWorkDirs)
	return result
}
