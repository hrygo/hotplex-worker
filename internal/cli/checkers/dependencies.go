package checkers

import (
	"context"
	"fmt"
	"net/http"
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

// networkReachableChecker checks external API connectivity.
// Skipped by default — only runs when explicitly requested.
type networkReachableChecker struct{}

func (c networkReachableChecker) Name() string     { return "dependencies.network_reachable" }
func (c networkReachableChecker) Category() string { return "dependencies" }
func (c networkReachableChecker) Check(ctx context.Context) cli.Diagnostic {
	client := &http.Client{Timeout: 5}
	resp, err := client.Get("https://api.anthropic.com")
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Cannot reach https://api.anthropic.com",
			Detail:   err.Error(),
			FixHint:  "Check network connection and DNS settings",
		}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  fmt.Sprintf("API reachable (HTTP %d)", resp.StatusCode),
		}
	}
	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusWarn,
		Message:  fmt.Sprintf("API returned unexpected status: HTTP %d", resp.StatusCode),
	}
}

func init() {
	cli.DefaultRegistry.Register(workerBinaryChecker{})
	cli.DefaultRegistry.Register(sqlitePathChecker{dbPath: filepath.Join(os.ExpandEnv("$HOME"), ".hotplex", "data", "hotplex.db")})
}
