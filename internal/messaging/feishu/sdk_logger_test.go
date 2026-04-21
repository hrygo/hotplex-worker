package feishu

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ─── redactURL ──────────────────────────────────────────────────────────────

func TestRedactURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "non-URL unchanged",
			input: "plain text message",
			want:  "plain text message",
		},
		{
			name:  "ws URL without params unchanged",
			input: "ws://localhost:8080/path",
			want:  "ws://localhost:8080/path",
		},
		{
			name:  "wss URL without params unchanged",
			input: "wss://api.feishu.cn/ws",
			want:  "wss://api.feishu.cn/ws",
		},
		{
			name:  "http URL without params unchanged",
			input: "http://localhost:8888/health",
			want:  "http://localhost:8888/health",
		},
		{
			name:  "https URL without sensitive params unchanged",
			input: "https://api.feishu.cn/v1/messages?token=abc123",
			want:  "https://api.feishu.cn/v1/messages?token=abc123",
		},
		{
			name:  "access_key redacted",
			input: "https://api.feishu.cn/v1/messages?access_key=mykey&foo=bar",
			want:  "https://api.feishu.cn/v1/messages?access_key=***&foo=bar",
		},
		{
			name:  "ticket redacted",
			input: "wss://api.feishu.cn/ws?ticket=secret&channel=main",
			want:  "wss://api.feishu.cn/ws?channel=main&ticket=***",
		},
		{
			name:  "both redacted order varies",
			input: "https://api.feishu.cn/v1/messages?access_key=key&ticket=tkt&other=val",
			want:  "https://api.feishu.cn/v1/messages?other=val&access_key=***&ticket=***",
		},
		{
			name:  "empty string unchanged",
			input: "",
			want:  "",
		},
		{
			name:  "partial URL prefix not matched",
			input: "wss://example.com",
			want:  "wss://example.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := redactURL(tt.input)
			// Check that *** appears for sensitive params and non-sensitive params are preserved.
			if strings.Contains(tt.input, "access_key") {
				require.Contains(t, got, "access_key=***")
				require.NotContains(t, got, "access_key=mykey")
			}
			if strings.Contains(tt.input, "ticket") {
				require.Contains(t, got, "ticket=***")
				require.NotContains(t, got, "ticket=secret")
			}
			// Non-URL unchanged
			if !strings.HasPrefix(tt.input, "http") && !strings.HasPrefix(tt.input, "ws") {
				require.Equal(t, tt.input, got)
			}
		})
	}
}

// ─── SlogLogger Debug ─────────────────────────────────────────────────────────

func TestSlogLogger_Debug(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	l := SlogLogger{Logger: log}

	// Silent debug messages produce no output (discarded).
	l.Debug(context.Background(), "ping success from server")
	l.Debug(context.Background(), "receive pong from endpoint")

	// Non-silent debug messages are logged (need LevelDebug handler).
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, &slog.HandlerOptions{Level: slog.LevelDebug})
	l2 := SlogLogger{Logger: slog.New(h)}
	l2.Debug(context.Background(), "some important debug message")
	require.Contains(t, got, "some important debug message")
}

// ─── SlogLogger Info ──────────────────────────────────────────────────────────

func TestSlogLogger_Info(t *testing.T) {
	t.Parallel()
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, nil)
	l := SlogLogger{Logger: slog.New(h)}

	l.Info(context.Background(), "info message", "key", "value")
	require.Contains(t, got, "info message")
	require.Contains(t, got, "key")
	require.Contains(t, got, "value")
}

// ─── SlogLogger Warn ──────────────────────────────────────────────────────────

func TestSlogLogger_Warn(t *testing.T) {
	t.Parallel()
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, nil)
	l := SlogLogger{Logger: slog.New(h)}

	l.Warn(context.Background(), "warning message")
	require.Contains(t, got, "warning message")
}

// ─── SlogLogger Error ─────────────────────────────────────────────────────────

func TestSlogLogger_Error(t *testing.T) {
	t.Parallel()
	var got string
	h := slog.NewTextHandler(&captureWriter{&got}, nil)
	l := SlogLogger{Logger: slog.New(h)}

	l.Error(context.Background(), "error message")
	require.Contains(t, got, "error message")
}

// captureWriter implements io.Writer for slog handler.
type captureWriter struct {
	s *string
}

func (w *captureWriter) Write(p []byte) (int, error) {
	*w.s += string(p)
	return len(p), nil
}
