package security

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ─── OutputLimiter ─────────────────────────────────────────────────────────────

func TestOutputLimiter_Check(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setupBytes int64
		line       []byte
		wantErr    bool
		errMsg     string
	}{
		{
			name:    "small line accepted",
			line:    []byte("hello world"),
			wantErr: false,
		},
		{
			name:    "empty line accepted",
			line:    []byte{},
			wantErr: false,
		},
		{
			name:    "line exceeds MaxLineBytes",
			line:    make([]byte, MaxLineBytes+1),
			wantErr: true,
			errMsg:  "line exceeds",
		},
		{
			name:    "exactly MaxLineBytes accepted",
			line:    make([]byte, MaxLineBytes),
			wantErr: false,
		},
		{
			name:       "total exceeds MaxSessionBytes",
			setupBytes: MaxSessionBytes - 10,
			line:       make([]byte, 100),
			wantErr:    true,
			errMsg:     "session output exceeds",
		},
		{
			name:       "exactly MaxSessionBytes accepted",
			setupBytes: MaxSessionBytes - 100,
			line:       make([]byte, 100),
			wantErr:    false,
		},
		{
			name:       "zero remaining bytes",
			setupBytes: MaxSessionBytes,
			line:       []byte("any"),
			wantErr:    true,
			errMsg:     "session output exceeds",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			limiter := &OutputLimiter{totalBytes: tt.setupBytes}
			err := limiter.Check(tt.line)

			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestOutputLimiter_Check_Accumulates(t *testing.T) {
	t.Parallel()

	limiter := &OutputLimiter{}

	// First check
	err := limiter.Check([]byte("line1"))
	require.NoError(t, err)
	require.Equal(t, int64(5), limiter.Total())

	// Second check accumulates
	err = limiter.Check([]byte("line2"))
	require.NoError(t, err)
	require.Equal(t, int64(10), limiter.Total())

	// Third check
	err = limiter.Check([]byte("line3"))
	require.NoError(t, err)
	require.Equal(t, int64(15), limiter.Total())
}

func TestOutputLimiter_Total(t *testing.T) {
	t.Parallel()

	t.Run("initial zero", func(t *testing.T) {
		t.Parallel()
		limiter := &OutputLimiter{}
		require.Equal(t, int64(0), limiter.Total())
	})

	t.Run("after checks", func(t *testing.T) {
		t.Parallel()
		limiter := &OutputLimiter{}
		_ = limiter.Check([]byte("12345"))
		_ = limiter.Check([]byte("67890"))
		require.Equal(t, int64(10), limiter.Total())
	})

	t.Run("concurrent access", func(t *testing.T) {
		t.Parallel()
		limiter := &OutputLimiter{}

		// Simulate concurrent reads (race detector will catch issues)
		go func() {
			_ = limiter.Total()
		}()
		_ = limiter.Total()
	})
}

func TestOutputLimiter_Reset(t *testing.T) {
	t.Parallel()

	t.Run("reset after accumulation", func(t *testing.T) {
		t.Parallel()
		limiter := &OutputLimiter{}

		// Accumulate some bytes
		_ = limiter.Check([]byte("12345"))
		_ = limiter.Check([]byte("67890"))
		require.Equal(t, int64(10), limiter.Total())

		// Reset
		limiter.Reset()
		require.Equal(t, int64(0), limiter.Total())
	})

	t.Run("can reuse after reset", func(t *testing.T) {
		t.Parallel()
		limiter := &OutputLimiter{}

		// First session
		_ = limiter.Check([]byte("session1"))
		require.Equal(t, int64(8), limiter.Total())

		// Reset for new session
		limiter.Reset()
		require.Equal(t, int64(0), limiter.Total())

		// Second session
		_ = limiter.Check([]byte("session2"))
		require.Equal(t, int64(8), limiter.Total())
	})

	t.Run("reset empty limiter", func(t *testing.T) {
		t.Parallel()
		limiter := &OutputLimiter{}
		limiter.Reset() // Should not panic
		require.Equal(t, int64(0), limiter.Total())
	})
}

func TestOutputLimiter_ConcurrentUse(t *testing.T) {
	// This test ensures thread safety with the race detector
	t.Parallel()

	limiter := &OutputLimiter{}

	// Start multiple goroutines that check and read total
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = limiter.Check([]byte("test"))
				_ = limiter.Total()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have accumulated 10 * 100 * 4 = 4000 bytes
	require.Equal(t, int64(4000), limiter.Total())
}

func TestOutputLimiter_BoundaryConditions(t *testing.T) {
	t.Parallel()

	t.Run("exactly MaxLineBytes", func(t *testing.T) {
		t.Parallel()
		limiter := &OutputLimiter{}
		line := make([]byte, MaxLineBytes)
		err := limiter.Check(line)
		require.NoError(t, err)
	})

	t.Run("one byte over MaxLineBytes", func(t *testing.T) {
		t.Parallel()
		limiter := &OutputLimiter{}
		line := make([]byte, MaxLineBytes+1)
		err := limiter.Check(line)
		require.Error(t, err)
		require.Contains(t, err.Error(), "line exceeds")
	})

	t.Run("exactly MaxSessionBytes", func(t *testing.T) {
		t.Parallel()
		limiter := &OutputLimiter{totalBytes: MaxSessionBytes - 1}
		err := limiter.Check([]byte("x"))
		require.NoError(t, err)
	})

	t.Run("one byte over MaxSessionBytes", func(t *testing.T) {
		t.Parallel()
		limiter := &OutputLimiter{totalBytes: MaxSessionBytes}
		err := limiter.Check([]byte("x"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "session output exceeds")
	})

	t.Run("large line after accumulation", func(t *testing.T) {
		t.Parallel()
		limiter := &OutputLimiter{totalBytes: MaxSessionBytes / 2}

		// Line that would fit individually but not with current total
		line := make([]byte, MaxSessionBytes/2+1)
		err := limiter.Check(line)
		require.Error(t, err)
		// Could be either "line exceeds" or "session output exceeds" depending on size
		require.Error(t, err)
	})
}

// ─── Constants validation ───────────────────────────────────────────────────────

func TestOutputLimits_Constants(t *testing.T) {
	t.Parallel()

	// Verify constants are set to expected values
	require.Equal(t, 10*1024*1024, MaxLineBytes, "MaxLineBytes should be 10MB")
	require.Equal(t, 20*1024*1024, MaxSessionBytes, "MaxSessionBytes should be 20MB")
	require.Equal(t, 1*1024*1024, MaxEnvelopeBytes, "MaxEnvelopeBytes should be 1MB")
}
