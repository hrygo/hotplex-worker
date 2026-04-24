package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPIDWriteReadRemove(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	err := writeGatewayPID()
	require.NoError(t, err)

	pidPath := filepath.Join(tmpDir, ".hotplex", ".pids", "gateway.pid")
	data, err := os.ReadFile(pidPath)
	require.NoError(t, err)
	require.Equal(t, []byte(fmt.Sprintf("%d", os.Getpid())), data)

	pid, err := readGatewayPID()
	require.NoError(t, err)
	require.Equal(t, os.Getpid(), pid)

	removeGatewayPID()
	_, err = os.Stat(pidPath)
	require.True(t, os.IsNotExist(err))

	t.Setenv("HOME", origHome)
}

func TestReadGatewayPID_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := readGatewayPID()
	require.Error(t, err)
	require.Contains(t, err.Error(), "no PID file")
}

func TestReadGatewayPID_StalePID(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	pidPath := filepath.Join(tmpDir, ".hotplex", ".pids", "gateway.pid")
	err := os.MkdirAll(filepath.Dir(pidPath), 0o755)
	require.NoError(t, err)

	err = os.WriteFile(pidPath, []byte("99999999"), 0o644)
	require.NoError(t, err)

	_, err = readGatewayPID()
	require.Error(t, err)
	require.Contains(t, err.Error(), "stale")
}
