//go:build windows

package security

import (
	"os"
	"path/filepath"

	"github.com/hrygo/hotplex/internal/config"
)

var AllowedBaseDirs = map[string]bool{
	config.TempBaseDir(): true,
}

// ForbiddenWorkDirs are system directories that must never be used as session work dirs.
// Populated from environment variables to handle non-C: drive installations.
var ForbiddenWorkDirs []string

func init() {
	sysDrive := os.Getenv("SystemDrive")
	if sysDrive == "" {
		sysDrive = "C:"
	}
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = filepath.Join(sysDrive, "Windows")
	}
	ForbiddenWorkDirs = []string{
		sysRoot,
		filepath.Join(sysDrive, "Program Files"),
		filepath.Join(sysDrive, "Program Files (x86)"),
		filepath.Join(sysDrive, "ProgramData"),
		filepath.Join(sysDrive, "System Volume Information"),
	}
}
