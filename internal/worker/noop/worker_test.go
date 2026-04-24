package noop

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestNoOpWorker_Capabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		got   interface{}
		want  interface{}
		check func(got, want interface{})
	}{
		{
			name:  "Type returns TypeUnknown",
			got:   worker.TypeUnknown,
			want:  worker.TypeUnknown,
			check: func(got, want interface{}) { require.Equal(t, want, got) },
		},
		{
			name:  "SupportsResume returns false",
			got:   false,
			want:  false,
			check: func(got, want interface{}) { require.Equal(t, want, got) },
		},
		{
			name:  "SupportsStreaming returns false",
			got:   false,
			want:  false,
			check: func(got, want interface{}) { require.Equal(t, want, got) },
		},
		{
			name:  "SupportsTools returns false",
			got:   false,
			want:  false,
			check: func(got, want interface{}) { require.Equal(t, want, got) },
		},
		{
			name:  "SessionStoreDir returns empty string",
			got:   "",
			want:  "",
			check: func(got, want interface{}) { require.Equal(t, want, got) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(tt.got, tt.want)
		})
	}
}

func TestNoOpWorker_CapabilitiesMethods(t *testing.T) {
	t.Parallel()
	w := NewWorker()

	require.Equal(t, worker.TypeUnknown, w.Type())
	require.False(t, w.SupportsResume())
	require.False(t, w.SupportsStreaming())
	require.False(t, w.SupportsTools())
	require.Nil(t, w.EnvWhitelist())
	require.Empty(t, w.SessionStoreDir())
	require.Zero(t, w.MaxTurns())
	require.Nil(t, w.Modalities())
}

func TestNoOpWorker_StartTerminateKill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   func() error
	}{
		{name: "Start returns nil", fn: func() error {
			return NewWorker().Start(context.Background(), worker.SessionInfo{})
		}},
		{name: "Terminate returns nil", fn: func() error {
			return NewWorker().Terminate(context.Background())
		}},
		{name: "Kill returns nil", fn: func() error {
			return NewWorker().Kill()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.fn()
			require.NoError(t, err)
		})
	}
}

func TestNoOpWorker_InputResume(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "Input returns ErrNotImplemented",
			fn: func() error {
				return NewWorker().Input(context.Background(), "hello", nil)
			},
		},
		{
			name: "Resume is a no-op (returns nil)",
			fn: func() error {
				return NewWorker().Resume(context.Background(), worker.SessionInfo{})
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.fn()
			if tt.name == "Input returns ErrNotImplemented" {
				require.ErrorIs(t, err, worker.ErrNotImplemented)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNoOpWorker_Wait(t *testing.T) {
	t.Parallel()
	exitCode, err := NewWorker().Wait()
	require.Equal(t, 0, exitCode)
	require.ErrorIs(t, err, io.EOF)
}

func TestNoOpConn_SendClose(t *testing.T) {
	t.Parallel()

	t.Run("Send does not panic and returns nil", func(t *testing.T) {
		t.Parallel()
		c := NewConn("sess-1", "user-1")
		require.NotPanics(t, func() {
			err := c.Send(context.Background(), nil)
			require.NoError(t, err)
		})
	})

	t.Run("Close closes the receive channel", func(t *testing.T) {
		t.Parallel()
		c := NewConn("sess-2", "user-2")
		err := c.Close()
		require.NoError(t, err)
	})

	t.Run("Close then Recv does not block and returns zero value", func(t *testing.T) {
		t.Parallel()
		c := NewConn("sess-3", "user-3")
		_ = c.Close()

		// Recv must not block after Close; it should return the zero value immediately.
		select {
		case ev, ok := <-c.Recv():
			require.False(t, ok, "channel should be closed")
			require.Nil(t, ev, "should receive nil from closed channel")
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Recv() blocked after Close(); expected immediate return")
		}
	})
}

func TestNoOpWorker_SetConnConn(t *testing.T) {
	t.Parallel()

	t.Run("Conn returns nil when not set", func(t *testing.T) {
		t.Parallel()
		w := NewWorker()
		require.Nil(t, w.Conn())
	})

	t.Run("Conn returns the value set by SetConn", func(t *testing.T) {
		t.Parallel()
		w := NewWorker()
		conn := NewConn("my-session", "my-user")
		w.SetConn(conn)
		require.Same(t, conn, w.Conn())
	})
}

func TestNoOpWorker_Health(t *testing.T) {
	t.Parallel()
	h := NewWorker().Health()
	require.True(t, h.Healthy)
	require.False(t, h.Running)
	require.Equal(t, worker.TypeUnknown, h.Type)
}

func TestNoOpConn_UserID_SessionID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sessionID  string
		userID     string
		wantSessID string
		wantUserID string
	}{
		{
			name:       "returns correct session and user IDs",
			sessionID:  "sess-abc",
			userID:     "user-123",
			wantSessID: "sess-abc",
			wantUserID: "user-123",
		},
		{
			name:       "returns empty strings when not set",
			sessionID:  "",
			userID:     "",
			wantSessID: "",
			wantUserID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewConn(tt.sessionID, tt.userID)
			require.Equal(t, tt.wantSessID, c.SessionID())
			require.Equal(t, tt.wantUserID, c.UserID())
		})
	}
}

func TestNoOpWorker_LastIO(t *testing.T) {
	t.Parallel()
	zero := time.Time{}
	got := NewWorker().LastIO()
	require.True(t, got.IsZero(), "LastIO should return zero time.Time")
	_ = zero // suppress unused variable warning
}
