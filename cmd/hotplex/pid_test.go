package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayStateWriteReadRemove(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	err := writeGatewayState("/test/config.yaml", true)
	require.NoError(t, err)

	pidPath := filepath.Join(tmpDir, ".hotplex", ".pids", "gateway.pid")
	_, err = os.ReadFile(pidPath)
	require.NoError(t, err)

	state, _ := readGatewayState()
	requireConfigPath(t, state)

	removeGatewayState()
	_, err = os.Stat(pidPath)
	require.True(t, os.IsNotExist(err))

	t.Setenv("HOME", origHome)
}

func requireConfigPath(t *testing.T, state *gatewayState) {
	t.Helper()
	require.NotNil(t, state)
	require.Equal(t, os.Getpid(), state.PID)
	require.Equal(t, "/test/config.yaml", state.ConfigPath)
	require.True(t, state.DevMode)
}

func TestReadGatewayState_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := readGatewayState()
	require.Error(t, err)
	require.Contains(t, err.Error(), "no PID file")
}

func TestReadGatewayState_StalePID(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	pidPath := filepath.Join(tmpDir, ".hotplex", ".pids", "gateway.pid")
	err := os.MkdirAll(filepath.Dir(pidPath), 0o755)
	require.NoError(t, err)

	err = os.WriteFile(pidPath, []byte(`{"pid":99999999}`), 0o644)
	require.NoError(t, err)

	_, err = readGatewayState()
	require.Error(t, err)
	require.Contains(t, err.Error(), "stale")
}
