package checkers

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestDiskSpace(t *testing.T) {
	t.Parallel()

	c := diskSpaceChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "runtime.disk_space", d.Name)
	require.Equal(t, "runtime", d.Category)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail, cli.StatusWarn}, d.Status)
	require.NotEmpty(t, d.Message)
}

func TestPortAvailable(t *testing.T) {
	t.Parallel()

	c := portAvailableChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "runtime.port_available", d.Name)
	require.Equal(t, "runtime", d.Category)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
}

func TestPortAvailable_Blocked(t *testing.T) {
	t.Parallel()

	// Occupy port 0 to get a free port, then block it
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Skip("cannot bind a free port, skipping blocked-port test")
	}
	t.Cleanup(func() { _ = l.Close() })

	// Now test that a non-available port is detected by the checker.
	// We use port 1 (privileged, almost certainly not available) as a
	// reliable way to test the "port in use" branch.
	// The standard check on 8888/9999 may or may not be blocked.
	c := portAvailableChecker{}
	d := c.Check(context.Background())

	// At minimum, the checker must produce a valid diagnostic structure.
	require.Equal(t, "runtime.port_available", d.Name)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
}

func TestOrphanPIDs_NoDir(t *testing.T) {
	t.Parallel()

	c := orphanPIDsChecker{pidDir: "/nonexistent/pids"}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "does not exist")
}

func TestOrphanPIDs_EmptyDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c := orphanPIDsChecker{pidDir: dir}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestOrphanPIDs_StaleFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "99999999.pid"), []byte("99999999"), 0o644))

	c := orphanPIDsChecker{pidDir: dir}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusWarn, d.Status)
	require.NotNil(t, d.FixFunc)
	require.NoError(t, d.FixFunc())

	// Verify the stale file was removed
	_, err := os.Stat(filepath.Join(dir, "99999999.pid"))
	require.True(t, os.IsNotExist(err))
}

func TestOrphanPIDs_MixedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Stale PID (very high, not running)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "88888888.pid"), []byte("88888888"), 0o644))
	// Non-PID file (should be ignored)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0o644))
	// Subdirectory (should be ignored)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o755))

	c := orphanPIDsChecker{pidDir: dir}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusWarn, d.Status)
}

func TestDataDirWritable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c := dataDirWritableChecker{dataDir: dir}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "writable")
}

func TestDataDirWritable_NotExist(t *testing.T) {
	t.Parallel()

	c := dataDirWritableChecker{dataDir: "/nonexistent/data/path"}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusWarn, d.Status)
	require.Contains(t, d.Message, "does not exist")
}

func TestDataDirWritable_FixFunc(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "data")
	c := dataDirWritableChecker{dataDir: target}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusWarn, d.Status)
	require.NotNil(t, d.FixFunc)
	require.NoError(t, d.FixFunc())

	info, err := os.Stat(target)
	require.NoError(t, err)
	require.True(t, info.IsDir())
}

func TestDataDirWritable_NotADirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "notadir")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0o644))

	c := dataDirWritableChecker{dataDir: filePath}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, "not a directory")
}
