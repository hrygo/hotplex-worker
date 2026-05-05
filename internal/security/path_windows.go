//go:build windows

package security

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/hrygo/hotplex/internal/config"
)

var (
	allowedBaseDirs     = map[string]bool{config.TempBaseDir(): true}
	forbiddenWorkDirs   []string
	securityConfigMutex sync.RWMutex
)

func init() {
	sysDrive := os.Getenv("SystemDrive")
	if sysDrive == "" {
		sysDrive = "C:"
	}
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = filepath.Join(sysDrive, "Windows")
	}
	forbiddenWorkDirs = []string{
		sysRoot,
		filepath.Join(sysDrive, "Program Files"),
		filepath.Join(sysDrive, "Program Files (x86)"),
		filepath.Join(sysDrive, "ProgramData"),
		filepath.Join(sysDrive, "System Volume Information"),
	}
}

// GetAllowedBaseDirs returns a copy of the current allowed base directories map.
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
func GetForbiddenWorkDirs() []string {
	securityConfigMutex.RLock()
	defer securityConfigMutex.RUnlock()

	result := make([]string, len(forbiddenWorkDirs))
	copy(result, forbiddenWorkDirs)
	return result
}

// ConfigureFromConfig applies security settings from the configuration file.
func ConfigureFromConfig(cfg *config.SecurityConfig) {
	securityConfigMutex.Lock()
	defer securityConfigMutex.Unlock()

	for _, pattern := range cfg.WorkDirAllowedBasePatterns {
		expandedPath := os.ExpandEnv(pattern)
		if expandedPath != "" {
			allowedBaseDirs[expandedPath] = true
		}
	}
	for _, dir := range cfg.WorkDirForbiddenDirs {
		expandedPath := os.ExpandEnv(dir)
		if expandedPath != "" && !allowedBaseDirs[expandedPath] {
			forbiddenWorkDirs = append(forbiddenWorkDirs, expandedPath)
		}
	}
}
