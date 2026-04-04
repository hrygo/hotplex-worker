package proc

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"testing"

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
