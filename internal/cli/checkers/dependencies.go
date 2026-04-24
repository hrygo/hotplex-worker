package checkers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hrygo/hotplex/internal/cli"
)

type workerBinaryChecker struct{}

func (c workerBinaryChecker) Name() string     { return "dependencies.worker_binary" }
func (c workerBinaryChecker) Category() string { return "dependencies" }
func (c workerBinaryChecker) Check(ctx context.Context) cli.Diagnostic {
	claude, errClaude := exec.LookPath("claude")
	opencode, errOpenCode := exec.LookPath("opencode")

	if errClaude == nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Claude Code found: " + claude,
		}
	}
	if errOpenCode == nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "OpenCode found: " + opencode,
		}
	}
	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusFail,
		Message:  "No worker binary found (claude or opencode)",
		FixHint:  "Install Claude Code (npm install -g @anthropic-ai/claude-code) or OpenCode",
	}
}

type sqlitePathChecker struct {
	dbPath string
}

func (c sqlitePathChecker) Name() string     { return "dependencies.sqlite_path" }
func (c sqlitePathChecker) Category() string { return "dependencies" }
func (c sqlitePathChecker) Check(ctx context.Context) cli.Diagnostic {
	parentDir := filepath.Dir(c.dbPath)
	info, err := os.Stat(parentDir)
	if err != nil {
		if os.IsNotExist(err) {
			return cli.Diagnostic{
				Name:     c.Name(),
				Category: c.Category(),
				Status:   cli.StatusWarn,
				Message:  "SQLite parent directory does not exist: " + parentDir,
				FixHint:  "Directory will be created automatically",
				FixFunc: func() error {
					return os.MkdirAll(parentDir, 0o755)
				},
			}
		}
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Cannot stat SQLite parent directory: " + err.Error(),
		}
	}
	if !info.IsDir() {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "SQLite parent path is not a directory: " + parentDir,
		}
	}
	testPath := filepath.Join(parentDir, ".hotplex_write_test")
	f, err := os.Create(testPath)
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "SQLite parent directory is not writable: " + parentDir,
			Detail:   err.Error(),
		}
	}
	_ = f.Close()
	_ = os.Remove(testPath)
	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "SQLite path is valid and writable: " + c.dbPath,
	}
}

func init() {
	cli.DefaultRegistry.Register(workerBinaryChecker{})
	cli.DefaultRegistry.Register(sqlitePathChecker{dbPath: filepath.Join(os.ExpandEnv("$HOME"), ".hotplex", "data", "hotplex.db")})
}
