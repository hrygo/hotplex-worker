package checkers

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestWorkerBinaryChecker(t *testing.T) {
	t.Parallel()

	c := workerBinaryChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "dependencies.worker_binary", d.Name)
	require.Equal(t, "dependencies", d.Category)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
}

func TestSqlitePathChecker_Writable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c := sqlitePathChecker{dbPath: filepath.Join(dir, "test.db")}
	d := c.Check(context.Background())

	require.Equal(t, "dependencies.sqlite_path", d.Name)
	require.Equal(t, "dependencies", d.Category)
	require.Equal(t, cli.StatusPass, d.Status)
}

func TestSqlitePathChecker_NotExist(t *testing.T) {
	t.Parallel()

	c := sqlitePathChecker{dbPath: "/nonexistent/path/test.db"}
	d := c.Check(context.Background())

	require.Equal(t, "dependencies.sqlite_path", d.Name)
	require.Equal(t, "dependencies", d.Category)
	require.Equal(t, cli.StatusWarn, d.Status)
}
