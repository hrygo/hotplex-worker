//go:build windows

package service

import (
	"os"
	"path/filepath"
)

func systemLogDir() string {
	return filepath.Join(os.Getenv("ProgramData"), "hotplex", "logs")
}
