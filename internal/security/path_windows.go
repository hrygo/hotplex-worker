//go:build windows

package security

import (
	"os"
	"path/filepath"
)

var AllowedBaseDirs = map[string]bool{
	filepath.Join(os.TempDir(), "hotplex"): true,
}

var ForbiddenWorkDirs = []string{
	`C:\Windows`,
	`C:\Program Files`,
	`C:\Program Files (x86)`,
	`C:\ProgramData`,
	`C:\System Volume Information`,
}
