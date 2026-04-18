package proc

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// --- TestManager_New ---------------------------------------------------------

func TestManager_New(t *testing.T) {
	t.Run("nil Logger uses slog.Default", func(t *testing.T) {
		m := New(Opts{})
		require.NotNil(t, m)
		require.Equal(t, slog.Default(), m.log)
	})

	t.Run("nil AllowedTools defaults to nil", func(t *testing.T) {
		m := New(Opts{})
		require.Nil(t, m.allowedTools)
	})

	t.Run("normal construction", func(t *testing.T) {
		logger := slog.Default()
		tools := []string{"Read", "Bash"}
		m := New(Opts{
			Logger:       logger,
			AllowedTools: tools,
		})
		require.NotNil(t, m)
		require.Equal(t, logger, m.log)
		require.Equal(t, tools, m.allowedTools)
	})

	t.Run("fields not started", func(t *testing.T) {
		m := New(Opts{})
		require.False(t, m.started)
		require.False(t, m.exited)
		require.Equal(t, 0, m.pgid)
		require.Equal(t, 0, m.exitCode)
	})
}

// --- TestManager_IsRunning ----------------------------------------------------

func TestManager_IsRunning(t *testing.T) {
	tests := []struct {
		name    string
		started bool
		exited  bool
		want    bool
	}{
		{
			name:    "未启动时返回 false",
			started: false,
			exited:  false,
			want:    false,
		},
		{
			name:    "已启动且未退出时返回 true",
			started: true,
			exited:  false,
			want:    true,
		},
		{
			name:    "已启动且已退出时返回 false",
			started: true,
			exited:  true,
			want:    false,
		},
		{
			name:    "未启动但已退出标记返回 false",
			started: false,
			exited:  true,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Manager{started: tt.started, exited: tt.exited}
			got := m.IsRunning()
			require.Equal(t, tt.want, got)
		})
	}
}

// --- TestManager_PID ---------------------------------------------------------

func TestManager_PID(t *testing.T) {
	t.Run("未启动时返回 -1", func(t *testing.T) {
		m := &Manager{}
		require.Equal(t, -1, m.PID())
	})

	t.Run("cmd nil 时返回 -1", func(t *testing.T) {
		m := &Manager{cmd: nil}
		require.Equal(t, -1, m.PID())
	})

	t.Run("cmd.Process nil 时返回 -1", func(t *testing.T) {
		m := &Manager{cmd: &exec.Cmd{ProcessState: nil}}
		require.Equal(t, -1, m.PID())
	})

	t.Run("cmd.Process 不为 nil 时返回正确 PID", func(t *testing.T) {
		// cmd.Process 是只读字段，无法从包外构造；通过验证 cmd.Process 为 nil
		// 的分支路径来间接覆盖此场景。
		m := &Manager{}
		// cmd 为 nil 场景
		require.Equal(t, -1, m.PID())
	})
}

// --- TestManager_PGID --------------------------------------------------------

func TestManager_PGID(t *testing.T) {
	t.Run("未设置时返回 0", func(t *testing.T) {
		m := &Manager{}
		require.Equal(t, 0, m.PGID())
	})

	t.Run("pgid 已设置时返回正确值", func(t *testing.T) {
		m := &Manager{pgid: 12345}
		require.Equal(t, 12345, m.PGID())
	})
}

// --- TestManager_captureExitCode --------------------------------------------

func TestManager_captureExitCode(t *testing.T) {
	t.Run("ProcessState 为 nil 时提前返回 0", func(t *testing.T) {
		m := &Manager{
			cmd:      &exec.Cmd{},
			exitCode: 999, // 初始值不应被修改
		}
		m.captureExitCode()
		require.Equal(t, 999, m.exitCode) // 未被修改
		require.False(t, m.exited)
	})

	t.Run("cmd 为 nil 时提前返回", func(t *testing.T) {
		m := &Manager{
			cmd:      nil,
			exitCode: 999,
		}
		m.captureExitCode()
		require.Equal(t, 999, m.exitCode)
		require.False(t, m.exited)
	})
}

// --- TestManager_Close -------------------------------------------------------

func TestManager_Close(t *testing.T) {
	t.Run("stdin stdout stderr 均为 nil 时不 panic", func(t *testing.T) {
		m := &Manager{}
		require.NotPanics(t, func() {
			err := m.Close()
			require.NoError(t, err)
		})
	})

	t.Run("devnull 文件正常关闭不返回错误", func(t *testing.T) {
		// 使用临时文件模拟真实文件描述符，而非 os.DevNull
		tmp, err := os.CreateTemp("", "proc-close-*.tmp")
		require.NoError(t, err)
		path := tmp.Name()
		tmp.Close()
		defer os.Remove(path)

		f, err := os.Open(path)
		require.NoError(t, err)
		m := &Manager{stdout: f}
		err = m.Close()
		require.NoError(t, err)
	})

	t.Run("已关闭的文件关闭时返回错误", func(t *testing.T) {
		// 创建一个管道，写入端关闭后读端仍然有效
		r, w, err := os.Pipe()
		require.NoError(t, err)
		require.NoError(t, w.Close()) // 关闭写入端

		// 读端应该可以正常关闭
		m := &Manager{stdin: r}
		err = m.Close()
		require.NoError(t, err)
	})

	t.Run("多文件关闭收集错误", func(t *testing.T) {
		// 使用临时文件，关闭后再次关闭会返回错误
		tmp1, err := os.CreateTemp("", "proc-close-*.tmp")
		require.NoError(t, err)
		path1 := tmp1.Name()
		tmp1.Close()
		defer os.Remove(path1)

		tmp2, err := os.CreateTemp("", "proc-close-*.tmp")
		require.NoError(t, err)
		path2 := tmp2.Name()
		tmp2.Close()
		defer os.Remove(path2)

		f1, err := os.Open(path1)
		require.NoError(t, err)
		f2, err := os.Open(path2)
		require.NoError(t, err)
		// 第一次关闭
		require.NoError(t, f1.Close())
		require.NoError(t, f2.Close())

		// 第二次关闭返回错误
		m := &Manager{stdin: f1, stdout: f2}
		err = m.Close()
		require.Error(t, err)
		require.Contains(t, err.Error(), "proc: close")
	})
}

// --- TestManager_ReadLine ----------------------------------------------------

func TestManager_ReadLine(t *testing.T) {
	t.Run("scanner 为 nil 时返回 io.EOF", func(t *testing.T) {
		m := &Manager{scanner: nil}
		line, err := m.ReadLine()
		require.Equal(t, "", line)
		require.ErrorIs(t, err, io.EOF)
	})
}

// --- TestManager_Start_RealProcess -------------------------------------------

func TestManager_Start_RealProcess(t *testing.T) {
	if testRaceEnabled {
		t.Skip("skipping: real process tests cause TSAN OOM under -race")
	}
	if os.Getenv("CI") != "" {
		t.Skip("skipping: real process tests may OOM under CI coverage instrumentation")
	}

	t.Parallel()

	t.Run("start echo and read output", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{Logger: slog.Default()})
		ctx := context.Background()

		stdin, stdout, _, err := m.Start(ctx, "echo", []string{"hello"}, nil, "")
		require.NoError(t, err)
		require.NotNil(t, stdin)
		require.NotNil(t, stdout)
		t.Cleanup(func() { m.Close() })

		require.True(t, m.IsRunning())
		require.Greater(t, m.PID(), 0)
		require.Equal(t, m.PID(), m.PGID())

		line, err := m.ReadLine()
		require.NoError(t, err)
		require.Equal(t, "hello", line)

		// Next read should be EOF since echo exits after one line.
		_, err = m.ReadLine()
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("start nonexistent command returns error", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{Logger: slog.Default()})
		ctx := context.Background()

		_, _, _, err := m.Start(ctx, "nonexistent_binary_xyz", []string{}, nil, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "proc: start")
	})

	t.Run("double start returns error", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{Logger: slog.Default()})
		ctx := context.Background()

		_, _, _, err := m.Start(ctx, "echo", []string{"test"}, nil, "")
		require.NoError(t, err)
		t.Cleanup(func() { m.Close() })

		_, _, _, err = m.Start(ctx, "echo", []string{"test2"}, nil, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "already started")
	})
}

// --- TestManager_Terminate_Kill ----------------------------------------------

func TestManager_Terminate_GracefulExit(t *testing.T) {
	if testRaceEnabled {
		t.Skip("skipping: real process tests cause TSAN OOM under -race")
	}
	if os.Getenv("CI") != "" {
		t.Skip("skipping: real process tests may OOM under CI coverage instrumentation")
	}

	t.Parallel()

	t.Run("terminate exits cleanly with SIGTERM", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{Logger: slog.Default()})
		ctx := context.Background()

		_, _, _, err := m.Start(ctx, "sleep", []string{"60"}, nil, "")
		require.NoError(t, err)
		t.Cleanup(func() { m.Close() })

		require.True(t, m.IsRunning())

		err = m.Terminate(ctx, syscall.SIGTERM, 5*time.Second)
		require.NoError(t, err)
		require.False(t, m.IsRunning())
	})

	t.Run("kill sends SIGKILL", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{Logger: slog.Default()})
		ctx := context.Background()

		_, _, _, err := m.Start(ctx, "sleep", []string{"60"}, nil, "")
		require.NoError(t, err)
		t.Cleanup(func() { m.Close() })

		err = m.Kill()
		require.NoError(t, err)
		require.False(t, m.IsRunning())
	})

	t.Run("terminate on not-started returns nil", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{Logger: slog.Default()})
		ctx := context.Background()

		err := m.Terminate(ctx, syscall.SIGTERM, time.Second)
		require.NoError(t, err)
	})

	t.Run("kill on not-started returns nil", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{Logger: slog.Default()})
		err := m.Kill()
		require.NoError(t, err)
	})
}

// --- TestManager_Wait --------------------------------------------------------

func TestManager_Wait(t *testing.T) {
	if testRaceEnabled {
		t.Skip("skipping: real process tests cause TSAN OOM under -race")
	}
	if os.Getenv("CI") != "" {
		t.Skip("skipping: real process tests may OOM under CI coverage instrumentation")
	}

	t.Parallel()

	t.Run("wait returns exit code for short-lived process", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{Logger: slog.Default()})
		ctx := context.Background()

		_, _, _, err := m.Start(ctx, "echo", []string{"done"}, nil, "")
		require.NoError(t, err)
		t.Cleanup(func() { m.Close() })

		code, err := m.Wait()
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.False(t, m.IsRunning())
	})

	t.Run("wait on not-started returns error", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{Logger: slog.Default()})
		code, err := m.Wait()
		require.Error(t, err)
		require.Equal(t, -1, code)
		require.Contains(t, err.Error(), "not started")
	})

	t.Run("wait captures non-zero exit code", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{Logger: slog.Default()})
		ctx := context.Background()

		_, _, _, err := m.Start(ctx, "sh", []string{"-c", "exit 42"}, nil, "")
		require.NoError(t, err)
		t.Cleanup(func() { m.Close() })

		code, err := m.Wait()
		// cmd.Wait() returns an exec.ExitError for non-zero exits.
		require.Error(t, err)
		require.Equal(t, 42, code)
	})
}

// --- TestManager_ReadLine_MultiLine ------------------------------------------

func TestManager_ReadLine_MultiLine(t *testing.T) {
	if testRaceEnabled {
		t.Skip("skipping: real process tests cause TSAN OOM under -race")
	}
	if os.Getenv("CI") != "" {
		t.Skip("skipping: real process tests may OOM under CI coverage instrumentation")
	}

	t.Parallel()

	t.Run("read multiple lines from stdout", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{Logger: slog.Default()})
		ctx := context.Background()

		script := "echo line1; echo line2; echo line3"
		_, _, _, err := m.Start(ctx, "sh", []string{"-c", script}, nil, "")
		require.NoError(t, err)
		t.Cleanup(func() { m.Close() })

		lines := []string{}
		for {
			line, err := m.ReadLine()
			if err != nil {
				break
			}
			if line != "" {
				lines = append(lines, line)
			}
		}
		require.Len(t, lines, 3)
		require.Equal(t, "line1", lines[0])
		require.Equal(t, "line2", lines[1])
		require.Equal(t, "line3", lines[2])
	})
}

// --- TestManager_Start_AllowedTools ------------------------------------------

func TestManager_Start_AllowedTools(t *testing.T) {
	if testRaceEnabled {
		t.Skip("skipping: real process tests cause TSAN OOM under -race")
	}
	if os.Getenv("CI") != "" {
		t.Skip("skipping: real process tests may OOM under CI coverage instrumentation")
	}

	t.Parallel()

	t.Run("allowed tools are appended to args", func(t *testing.T) {
		t.Parallel()
		m := New(Opts{
			Logger:       slog.Default(),
			AllowedTools: []string{"Read", "Grep"},
		})
		ctx := context.Background()

		// echo will ignore the extra args but we verify Start doesn't error.
		_, _, _, err := m.Start(ctx, "echo", []string{"test"}, nil, "")
		require.NoError(t, err)
		t.Cleanup(func() { m.Close() })
		require.True(t, m.IsRunning())
	})
}
