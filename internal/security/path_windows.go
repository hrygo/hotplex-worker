//go:build windows

package security

import (
	"github.com/hrygo/hotplex/internal/config"
)

var AllowedBaseDirs = map[string]bool{
	config.TempBaseDir(): true,
}

var ForbiddenWorkDirs = []string{
	`C:\Windows`,
	`C:\Program Files`,
	`C:\Program Files (x86)`,
	`C:\ProgramData`,
	`C:\System Volume Information`,
}
