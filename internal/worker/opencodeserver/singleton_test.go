package opencodeserver

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

func TestNewSingletonProcessManager(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{
		IdleDrainPeriod: 30 * time.Minute,
		ReadyTimeout:    10 * time.Second,
		HTTPTimeout:     30 * time.Second,
	}
	mgr := NewSingletonProcessManager(log, cfg)

	require.NotNil(t, mgr)
	require.Equal(t, stateIdle, mgr.state)
	require.Equal(t, 0, mgr.refs)
	require.NotNil(t, mgr.client)
	require.NotNil(t, mgr.crashCh)
}

func TestSingletonProcessManager_IsRunning(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Initial state is idle → not running.
	require.False(t, mgr.IsRunning())

	// Simulate running state.
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.mu.Unlock()

	require.True(t, mgr.IsRunning())
}

func TestSingletonProcessManager_PID_NoProcess(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// No process started yet.
	require.Equal(t, 0, mgr.PID())
}

func TestSingletonProcessManager_allocatePort(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	port, err := mgr.allocatePort()
	require.NoError(t, err)
	require.Greater(t, port, 0)
	require.Less(t, port, 65536)
}

func TestSingletonProcessManager_allocatePort_Multiple(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	ports := make(map[int]bool)
	for range 5 {
		port, err := mgr.allocatePort()
		require.NoError(t, err)
		// Each call should return a different port (OS assigns ephemeral port).
		ports[port] = true
	}
	// All ports should be unique.
	require.Equal(t, 5, len(ports))
}

func TestSingletonProcessManager_Release_NoRefs(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Release with zero refs: should not panic, idempotent.
	mgr.Release()
	require.Equal(t, 0, mgr.refs)
}

func TestSingletonProcessManager_Release_DecrementsRef(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Manually set state to running and refs > 0.
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 2
	mgr.mu.Unlock()

	mgr.Release()
	require.Equal(t, 1, mgr.refs)
}

func TestSingletonProcessManager_Release_StartsIdleDrain(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{
		IdleDrainPeriod: 100 * time.Millisecond,
	}
	mgr := NewSingletonProcessManager(log, cfg)

	// Set running with 1 ref.
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 1
	mgr.mu.Unlock()

	mgr.Release()
	require.Equal(t, 0, mgr.refs)
	// Idle drain should start.
	mgr.mu.Lock()
	hasTimer := mgr.idleTimer != nil
	mgr.mu.Unlock()
	require.True(t, hasTimer, "idle drain timer should be started when refs reach 0")
}

func TestSingletonProcessManager_Shutdown(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Set state=running, refs>0, proc=nil (proc is nil when no real process started).
	mgr.mu.Lock()
	mgr.state = stateRunning
	mgr.refs = 5
	mgr.mu.Unlock()

	mgr.Shutdown(context.Background())

	require.Equal(t, stateStopped, mgr.state)
	// Refs unchanged when proc is nil (procure not started in test).
	mgr.mu.Lock()
	refs := mgr.refs
	mgr.mu.Unlock()
	require.Equal(t, 5, refs)
}

func TestSingletonProcessManager_Shutdown_AlreadyStopped(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	// Already stopped.
	mgr.mu.Lock()
	mgr.state = stateStopped
	mgr.mu.Unlock()

	// Shutdown again should not panic.
	mgr.Shutdown(context.Background())
	require.Equal(t, stateStopped, mgr.state)
}

func TestSingletonProcessManager_buildEnv(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	env := mgr.buildEnv()
	require.NotEmpty(t, env)
	// buildEnv delegates to base.BuildEnv which should populate env vars.
	for _, e := range env {
		require.NotEmpty(t, e)
	}
}

func TestSingletonProcessManager_Acquire_StoppedState(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.OpenCodeServerConfig{}
	mgr := NewSingletonProcessManager(log, cfg)

	mgr.mu.Lock()
	mgr.state = stateStopped
	mgr.mu.Unlock()

	addr, client, crash, err := mgr.Acquire(context.Background())
	require.Error(t, err)
	require.Empty(t, addr)
	require.Nil(t, client)
	require.Nil(t, crash)
}

func TestInitSingleton(t *testing.T) {
	InitSingleton(slog.Default(), config.OpenCodeServerConfig{})
	require.NotNil(t, singleton.Load())
}

func TestShutdownSingleton_Nil(t *testing.T) {
	ShutdownSingleton(context.Background())
}

func TestShutdownSingleton_Real(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	InitSingleton(log, config.OpenCodeServerConfig{})
	require.NotNil(t, singleton.Load())

	ShutdownSingleton(context.Background())
	require.Nil(t, singleton.Load())
}
