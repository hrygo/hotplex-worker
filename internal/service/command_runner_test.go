package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type mockCommandRunner struct {
	lookPathFn       func(string) (string, error)
	combinedOutputFn func(string, ...string) ([]byte, error)
	runFn            func(string, ...string) error
}

func (m mockCommandRunner) LookPath(file string) (string, error) {
	return m.lookPathFn(file)
}

func (m mockCommandRunner) CombinedOutput(name string, args ...string) ([]byte, error) {
	return m.combinedOutputFn(name, args...)
}

func (m mockCommandRunner) Run(name string, args ...string) error {
	return m.runFn(name, args...)
}

func TestResolveBinaryPath_LookPathFound(t *testing.T) {
	t.Parallel()
	runner := mockCommandRunner{
		lookPathFn: func(file string) (string, error) {
			return "/usr/local/bin/hotplex", nil
		},
	}
	path, err := resolveBinaryPath(runner)
	require.NoError(t, err)
	require.Contains(t, path, "hotplex")
}

func TestResolveBinaryPath_LookPathFails_FallbackToExecutable(t *testing.T) {
	t.Parallel()
	runner := mockCommandRunner{
		lookPathFn: func(file string) (string, error) {
			return "", errors.New("not found")
		},
	}
	path, err := resolveBinaryPath(runner)
	require.NoError(t, err)
	require.NotEmpty(t, path)
}

func TestRealRunner_LookPath(t *testing.T) {
	t.Parallel()
	runner := realRunner{}
	p, err := runner.LookPath("sh")
	if err != nil {
		t.Skipf("sh not in PATH: %v", err)
	}
	require.NotEmpty(t, p)
}

func TestMockRunner_SystemctlCombinedOutput(t *testing.T) {
	t.Parallel()
	runner := mockCommandRunner{
		combinedOutputFn: func(name string, args ...string) ([]byte, error) {
			require.Equal(t, "systemctl", name)
			require.Contains(t, args, "is-active")
			return []byte("active\n"), nil
		},
	}
	out, err := runner.CombinedOutput("systemctl", "is-active", "hotplex")
	require.NoError(t, err)
	require.Equal(t, "active\n", string(out))
}

func TestMockRunner_RunError(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("command failed")
	runner := mockCommandRunner{
		runFn: func(name string, args ...string) error {
			return expectedErr
		},
	}
	err := runner.Run("false")
	require.ErrorIs(t, err, expectedErr)
}

func TestCommandRunner_Interface(t *testing.T) {
	t.Parallel()
	var _ CommandRunner = realRunner{}
	var _ CommandRunner = mockCommandRunner{}
}
