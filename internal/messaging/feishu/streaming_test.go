package feishu

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// ─── CardPhase String ────────────────────────────────────────────────────────

func TestCardPhase_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		phase CardPhase
		want  string
	}{
		{PhaseIdle, "idle"},
		{PhaseCreating, "creating"},
		{PhaseStreaming, "streaming"},
		{PhaseCompleted, "completed"},
		{PhaseAborted, "aborted"},
		{PhaseTerminated, "terminated"},
		{PhaseCreationFailed, "creation_failed"},
		{CardPhase(99), "unknown(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.phase.String())
		})
	}
}

// ─── Phase Transitions ───────────────────────────────────────────────────────

func TestStreamingCardController_transition_ValidTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from CardPhase
		to   CardPhase
		want bool
	}{
		// From PhaseIdle
		{"idle→creating", PhaseIdle, PhaseCreating, true},
		{"idle→streaming", PhaseIdle, PhaseStreaming, false},
		{"idle→completed", PhaseIdle, PhaseCompleted, false},
		// From PhaseCreating
		{"creating→streaming", PhaseCreating, PhaseStreaming, true},
		{"creating→creation_failed", PhaseCreating, PhaseCreationFailed, true},
		{"creating→idle", PhaseCreating, PhaseIdle, false},
		// From PhaseStreaming
		{"streaming→completed", PhaseStreaming, PhaseCompleted, true},
		{"streaming→aborted", PhaseStreaming, PhaseAborted, true},
		{"streaming→terminated", PhaseStreaming, PhaseTerminated, true},
		{"streaming→creating", PhaseStreaming, PhaseCreating, false},
		// Terminal states
		{"completed→anything", PhaseCompleted, PhaseCompleted, false},
		{"aborted→anything", PhaseAborted, PhaseCompleted, false},
		{"terminated→anything", PhaseTerminated, PhaseCompleted, false},
		{"creation_failed→anything", PhaseCreationFailed, PhaseCompleted, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
			c.phase.Store(int32(tt.from))
			got := c.transition(tt.to)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestStreamingCardController_IsCreated(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	c.phase.Store(int32(PhaseIdle))
	require.False(t, c.IsCreated())

	c.phase.Store(int32(PhaseCreating))
	require.False(t, c.IsCreated())

	c.phase.Store(int32(PhaseStreaming))
	require.True(t, c.IsCreated())

	c.phase.Store(int32(PhaseCompleted))
	require.True(t, c.IsCreated())
}

func TestStreamingCardController_ConcurrentTransitions(t *testing.T) {
	t.Parallel()
	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	c.phase.Store(int32(PhaseIdle))
	done := make(chan bool, 10)
	for range 10 {
		go func() {
			// All goroutines try the same valid transition concurrently
			ok := c.transition(PhaseCreating)
			done <- ok
		}()
	}
	successCount := 0
	for range 10 {
		if <-done {
			successCount++
		}
	}
	// Exactly one should succeed; the rest fail because phase already changed
	require.Equal(t, 1, successCount)
	require.Equal(t, PhaseCreating, c.getPhase())
}

// ─── truncateForSummary ──────────────────────────────────────────────────────

func TestTruncateForSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		want   string
		wantOk bool
	}{
		{
			name:   "simple text under limit",
			input:  "Hello world",
			want:   "Hello world",
			wantOk: true,
		},
		{
			name:   "multiline takes first line",
			input:  "First line\nSecond line\nThird line",
			want:   "First line",
			wantOk: true,
		},
		{
			name:   "first line empty takes second",
			input:  "\nSecond line",
			want:   "Second line",
			wantOk: true,
		},
		{
			name:   "only whitespace",
			input:  "   ",
			want:   "",
			wantOk: false,
		},
		{
			name:   "empty string",
			input:  "",
			want:   "",
			wantOk: false,
		},
		{
			name:   "strip heading",
			input:  "# Hello World",
			want:   "Hello World",
			wantOk: true,
		},
		{
			name:   "strip bold",
			input:  "**bold** text",
			want:   "bold text",
			wantOk: true,
		},
		{
			name:   "strip inline code",
			input:  "`code` here",
			want:   "code here",
			wantOk: true,
		},
		{
			name:   "strip italic",
			input:  "_italic_ word",
			want:   "italic word",
			wantOk: true,
		},
		{
			name:   "mixed markdown",
			input:  "# **Title** with `code` and _italic_",
			want:   "Title with code and italic",
			wantOk: true,
		},
		{
			name:   "chinese characters under limit",
			input:  "你好世界",
			want:   "你好世界",
			wantOk: true,
		},
		{
			name:   "chinese characters over limit",
			input:  "这是一段很长的中文文本用来测试截断功能是否正常工作abcdefghijklmnopqrstuvwxyz",
			want:   "这是一段很长的中文文本用来测试截断功能是否正常工作abcdefghijklmnopqrstuvwxy",
			wantOk: true,
		},
		{
			name:   "50 runes exactly",
			input:  "12345678901234567890123456789012345678901234567890",
			want:   "12345678901234567890123456789012345678901234567890",
			wantOk: true,
		},
		{
			name:   "51 runes truncated",
			input:  "123456789012345678901234567890123456789012345678901",
			want:   "12345678901234567890123456789012345678901234567890",
			wantOk: true,
		},
		{
			name:   "mixed chinese and english",
			input:  "Hello 世界 12345678901234567890123456789012345678901234567890123456789",
			want:   "Hello 世界 12345678901234567890123456789012345678901",
			wantOk: true,
		},
		{
			name:   "newline only",
			input:  "\n",
			want:   "",
			wantOk: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateForSummary(tt.input)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantOk, got != "")
		})
	}
}

// ─── Error Detection Helpers ─────────────────────────────────────────────────

func TestIsCardRateLimitError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"nil wrapped error", errors.New(""), false},
		{"rate limit code 230020", errors.New("lark: code=230020 msg=rate limit"), true},
		{"rate limit in middle", errors.New("error code=230020 somewhere"), true},
		{"different error code", errors.New("lark: code=999999 msg=other"), false},
		{"no code", errors.New("some other error"), false},
		{"empty error", errors.New(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isCardRateLimitError(tt.err))
		})
	}
}

func TestIsCardTableLimitError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"table limit 230099", errors.New("lark: code=230099 msg=table limit"), true},
		{"table limit 11310", errors.New("lark: code=11310 msg=table limit"), true},
		{"both codes", errors.New("lark: code=230099 and 11310"), true},
		{"different error code", errors.New("lark: code=999999"), false},
		{"no code", errors.New("some other error"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isCardTableLimitError(tt.err))
		})
	}
}

// ─── Expired and MsgID ───────────────────────────────────────────────────────

func TestStreamingCardController_Expired(t *testing.T) {
	t.Parallel()

	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// streamStartTime is set to time.Now() by NewStreamingCardController.
	// So it should not be expired immediately.
	require.False(t, c.Expired(), "freshly created controller should not be expired")
}

func TestStreamingCardController_MsgID(t *testing.T) {
	t.Parallel()

	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Initially empty.
	require.Equal(t, "", c.MsgID())

	// Access after a delay (fresh controller is not expired).
	require.False(t, c.Expired())
}

func TestStreamingCardController_PhaseCreationFailed(t *testing.T) {
	t.Parallel()

	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// idle → creation_failed is a valid transition (via Terminated).
	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseCreationFailed))

	// Terminal state: no further transitions.
	require.False(t, c.transition(PhaseStreaming))
	require.False(t, c.transition(PhaseCompleted))
}

func TestStreamingCardController_Abort(t *testing.T) {
	t.Parallel()

	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// idle → creating → streaming
	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseStreaming))

	// streaming → aborted
	require.True(t, c.transition(PhaseAborted))

	// Terminal: idempotent abort.
	err := c.Abort(context.Background())
	require.NoError(t, err)
}

func TestStreamingCardController_Abort_IdleNotAllowed(t *testing.T) {
	t.Parallel()

	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Cannot abort from idle.
	require.False(t, c.transition(PhaseAborted))
}

func TestStreamingCardController_Close_Idempotent(t *testing.T) {
	t.Parallel()

	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// idle → creating → completed.
	require.True(t, c.transition(PhaseCreating))
	require.True(t, c.transition(PhaseCompleted))

	// Second Close is idempotent: returns nil without error.
	err := c.Close(context.Background())
	require.NoError(t, err)
}

func TestStreamingCardController_Close_FromCreating(t *testing.T) {
	t.Parallel()

	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// idle → creating
	require.True(t, c.transition(PhaseCreating))

	// Close from creating transitions to completed.
	err := c.Close(context.Background())
	require.NoError(t, err)
}

func TestStreamingCardController_IntegrityCheck(t *testing.T) {
	t.Parallel()

	c := NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Simulate: no bytes written = integrity OK (no stream data).
	require.True(t, c.transition(PhaseCreating))
	c.mu.Lock()
	c.bytesWritten = 0
	c.bytesFlushed = 0
	c.mu.Unlock()

	// Close should not panic with zero bytes.
	err := c.Close(context.Background())
	require.NoError(t, err)
}
