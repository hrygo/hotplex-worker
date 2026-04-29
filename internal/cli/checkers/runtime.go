package checkers

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hrygo/hotplex/internal/worker/proc"

	"github.com/hrygo/hotplex/internal/cli"
)

type diskSpaceChecker struct{}

func (c diskSpaceChecker) Name() string     { return "runtime.disk_space" }
func (c diskSpaceChecker) Category() string { return "runtime" }
func (c diskSpaceChecker) Check(ctx context.Context) cli.Diagnostic {
	freeMB, err := GetDiskFreeMB(".")
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Cannot determine disk space",
			Detail:   err.Error(),
		}
	}
	if freeMB >= 100 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  fmt.Sprintf("Disk space available: %d MB (minimum 100 MB)", freeMB),
		}
	}
	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusFail,
		Message:  fmt.Sprintf("Low disk space: %d MB available (minimum 100 MB)", freeMB),
		FixHint:  "Free up disk space or move data directory to a larger volume",
	}
}

type portAvailableChecker struct{}

func (c portAvailableChecker) Name() string     { return "runtime.port_available" }
func (c portAvailableChecker) Category() string { return "runtime" }
func (c portAvailableChecker) Check(ctx context.Context) cli.Diagnostic {
	var blocked []string
	for _, port := range []int{8888, 9999} {
		addr := fmt.Sprintf(":%d", port)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			blocked = append(blocked, fmt.Sprintf(":%d (%s)", port, err.Error()))
			continue
		}
		_ = l.Close()
	}
	if len(blocked) == 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Ports 8888 and 9999 are available",
		}
	}
	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusFail,
		Message:  "Ports in use: " + strings.Join(blocked, ", "),
		FixHint:  "Kill the process using the port (lsof -i :PORT | grep LISTEN) then kill -9 <PID>",
	}
}

type orphanPIDsChecker struct {
	pidDir string
}

func (c orphanPIDsChecker) Name() string     { return "runtime.orphan_pids" }
func (c orphanPIDsChecker) Category() string { return "runtime" }
func (c orphanPIDsChecker) Check(ctx context.Context) cli.Diagnostic {
	entries, err := os.ReadDir(c.pidDir)
	if err != nil {
		if os.IsNotExist(err) {
			return cli.Diagnostic{
				Name:     c.Name(),
				Category: c.Category(),
				Status:   cli.StatusPass,
				Message:  "PID directory does not exist (no orphans possible)",
			}
		}
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Cannot read PID directory: " + c.pidDir,
			Detail:   err.Error(),
		}
	}

	var orphanFiles []string
	var alive []int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		pidStr := strings.TrimSuffix(name, filepath.Ext(name))
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		if err := proc.IsProcessAlive(pid); err != nil {
			orphanFiles = append(orphanFiles, name)
		} else {
			alive = append(alive, pid)
		}
	}

	if len(orphanFiles) == 0 && len(alive) == 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "No PID files found",
		}
	}

	if len(orphanFiles) > 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  fmt.Sprintf("%d orphan PID file(s) found (processes not running)", len(orphanFiles)),
			Detail:   "Orphans: " + strings.Join(orphanFiles, ", "),
			FixHint:  "Run doctor --fix to remove stale PID files",
			FixFunc: func() error {
				for _, f := range orphanFiles {
					if err := os.Remove(filepath.Join(c.pidDir, f)); err != nil {
						return fmt.Errorf("remove %s: %w", f, err)
					}
				}
				return nil
			},
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  fmt.Sprintf("%d worker process(es) running", len(alive)),
		Detail:   "PIDs: " + fmt.Sprint(alive),
	}
}

type dataDirWritableChecker struct {
	dataDir string
}

func (c dataDirWritableChecker) Name() string     { return "runtime.data_dir_writable" }
func (c dataDirWritableChecker) Category() string { return "runtime" }
func (c dataDirWritableChecker) Check(ctx context.Context) cli.Diagnostic {
	info, err := os.Stat(c.dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return cli.Diagnostic{
				Name:     c.Name(),
				Category: c.Category(),
				Status:   cli.StatusWarn,
				Message:  "Data directory does not exist: " + c.dataDir,
				FixHint:  "Directory will be created automatically",
				FixFunc: func() error {
					return os.MkdirAll(c.dataDir, 0o755)
				},
			}
		}
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Cannot stat data directory: " + c.dataDir,
			Detail:   err.Error(),
		}
	}
	if !info.IsDir() {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Data path is not a directory: " + c.dataDir,
		}
	}

	testPath := filepath.Join(c.dataDir, ".hotplex_write_test")
	f, err := os.Create(testPath)
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Data directory is not writable: " + c.dataDir,
			Detail:   err.Error(),
			FixHint:  "Check directory permissions or run with sudo",
		}
	}
	_ = f.Close()
	_ = os.Remove(testPath)
	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "Data directory is writable: " + c.dataDir,
	}
}

func init() {
	cli.DefaultRegistry.Register(diskSpaceChecker{})
	cli.DefaultRegistry.Register(portAvailableChecker{})
	cli.DefaultRegistry.Register(orphanPIDsChecker{pidDir: filepath.Join(os.ExpandEnv("$HOME"), ".hotplex", ".pids")})
	cli.DefaultRegistry.Register(dataDirWritableChecker{dataDir: filepath.Join(os.ExpandEnv("$HOME"), ".hotplex", "data")})
}
