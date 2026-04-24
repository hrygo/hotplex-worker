package stt

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Transcriber interface checks
// ---------------------------------------------------------------------------

func TestLocalSTT_ImplementsTranscriber(t *testing.T) {
	t.Parallel()
	var _ Transcriber = (*LocalSTT)(nil)
}

func TestPersistentSTT_ImplementsTranscriber(t *testing.T) {
	t.Parallel()
	var _ Transcriber = (*PersistentSTT)(nil)
}

func TestFallbackSTT_ImplementsTranscriber(t *testing.T) {
	t.Parallel()
	var _ Transcriber = (*FallbackSTT)(nil)
}

// ---------------------------------------------------------------------------
// LocalSTT
// ---------------------------------------------------------------------------

func TestLocalSTT_RequiresDisk(t *testing.T) {
	t.Parallel()
	s := &LocalSTT{}
	require.True(t, s.RequiresDisk())
}

func TestLocalSTT_TranscribeSuccess(t *testing.T) {
	t.Parallel()
	s := NewLocalSTT("echo transcribed: {file}", slog.Default())
	text, err := s.Transcribe(context.Background(), []byte("fake-audio"))
	require.NoError(t, err)
	require.Contains(t, text, "transcribed:")
	require.Contains(t, text, "stt_")
}

func TestLocalSTT_TranscribeEmptyCommand(t *testing.T) {
	t.Parallel()
	s := NewLocalSTT("", slog.Default())
	_, err := s.Transcribe(context.Background(), []byte("audio"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty command")
}

func TestLocalSTT_TranscribeCommandFailure(t *testing.T) {
	t.Parallel()
	s := NewLocalSTT("false", slog.Default())
	_, err := s.Transcribe(context.Background(), []byte("audio"))
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// FallbackSTT
// ---------------------------------------------------------------------------

func TestFallbackSTT_RequiresDisk(t *testing.T) {
	t.Parallel()
	s := &FallbackSTT{}
	require.True(t, s.RequiresDisk())
}

func TestFallbackSTT_PrimarySuccess(t *testing.T) {
	t.Parallel()
	primary := &mockTranscriber{text: "primary result", err: nil}
	secondary := &mockTranscriber{text: "secondary result", err: nil}

	s := NewFallbackSTT(primary, secondary, slog.Default())
	text, err := s.Transcribe(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "primary result", text)
	require.False(t, secondary.called)
}

func TestFallbackSTT_PrimaryFails_SecondarySuccess(t *testing.T) {
	t.Parallel()
	primary := &mockTranscriber{err: fmt.Errorf("primary error")}
	secondary := &mockTranscriber{text: "fallback result"}

	s := NewFallbackSTT(primary, secondary, slog.Default())
	text, err := s.Transcribe(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "fallback result", text)
	require.True(t, secondary.called)
}

func TestFallbackSTT_PrimaryEmpty_SecondarySuccess(t *testing.T) {
	t.Parallel()
	primary := &mockTranscriber{text: ""}
	secondary := &mockTranscriber{text: "fallback result"}

	s := NewFallbackSTT(primary, secondary, slog.Default())
	text, err := s.Transcribe(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "fallback result", text)
}

func TestFallbackSTT_BothFail(t *testing.T) {
	t.Parallel()
	primary := &mockTranscriber{err: fmt.Errorf("primary error")}
	secondary := &mockTranscriber{err: fmt.Errorf("secondary error")}

	s := NewFallbackSTT(primary, secondary, slog.Default())
	_, err := s.Transcribe(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "secondary error")
}

// ---------------------------------------------------------------------------
// PersistentSTT
// ---------------------------------------------------------------------------

func TestPersistentSTT_RequiresDisk(t *testing.T) {
	t.Parallel()
	s := &PersistentSTT{}
	require.True(t, s.RequiresDisk())
}

func TestPersistentSTT_CloseBeforeUse(t *testing.T) {
	t.Parallel()
	s := NewPersistentSTT("cat", "test-stt", 0, slog.Default())
	require.NoError(t, s.Close(context.Background()))
}

func TestPersistentSTT_LazyStart(t *testing.T) {
	t.Parallel()
	s := NewPersistentSTT("cat", "test-stt", 0, slog.Default())
	require.False(t, s.started)
	_ = s.Close(context.Background())
}

func TestPersistentSTT_StartFailure(t *testing.T) {
	t.Parallel()
	s := NewPersistentSTT("/nonexistent/binary", "test-stt", 0, slog.Default())
	defer func() { _ = s.Close(context.Background()) }()

	_, err := s.Transcribe(context.Background(), []byte("audio"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "persistent stt:")
}

func TestPersistentSTT_TranscribeSuccess(t *testing.T) {
	script := `#!/bin/sh
IFS= read -r line
echo '{"text":"hello from persistent","error":""}'
`
	scriptPath := filepath.Join(t.TempDir(), "mock_stt.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	s := NewPersistentSTT(scriptPath, "test-stt", 0, slog.Default())
	defer func() { _ = s.Close(context.Background()) }()

	text, err := s.Transcribe(context.Background(), []byte("fake-audio"))
	require.NoError(t, err)
	require.Equal(t, "hello from persistent", text)
}

func TestPersistentSTT_MultipleRequests(t *testing.T) {
	script := `#!/bin/sh
count=0
while IFS= read -r line; do
	count=$((count + 1))
	echo "{\"text\":\"result-$count\",\"error\":\"\"}"
done
`
	scriptPath := filepath.Join(t.TempDir(), "multi.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	s := NewPersistentSTT(scriptPath, "test-stt", 0, slog.Default())
	defer func() { _ = s.Close(context.Background()) }()

	text1, err := s.Transcribe(context.Background(), []byte("audio1"))
	require.NoError(t, err)
	require.Equal(t, "result-1", text1)

	text2, err := s.Transcribe(context.Background(), []byte("audio2"))
	require.NoError(t, err)
	require.Equal(t, "result-2", text2)
}

func TestPersistentSTT_SubprocessExit(t *testing.T) {
	script := `#!/bin/sh
read -r line
`
	scriptPath := filepath.Join(t.TempDir(), "exit.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	s := NewPersistentSTT(scriptPath, "test-stt", 0, slog.Default())
	defer func() { _ = s.Close(context.Background()) }()

	_, err := s.Transcribe(context.Background(), []byte("audio"))
	require.Error(t, err)
}

func TestPersistentSTT_InvalidJSON(t *testing.T) {
	script := `#!/bin/sh
read -r line
echo 'not-json'
`
	scriptPath := filepath.Join(t.TempDir(), "badjson.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	s := NewPersistentSTT(scriptPath, "test-stt", 0, slog.Default())
	defer func() { _ = s.Close(context.Background()) }()

	_, err := s.Transcribe(context.Background(), []byte("audio"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse response")
}

func TestPersistentSTT_ErrorResponse(t *testing.T) {
	script := `#!/bin/sh
read -r line
echo '{"text":"","error":"model load failed"}'
`
	scriptPath := filepath.Join(t.TempDir(), "errresp.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	s := NewPersistentSTT(scriptPath, "test-stt", 0, slog.Default())
	defer func() { _ = s.Close(context.Background()) }()

	_, err := s.Transcribe(context.Background(), []byte("audio"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "model load failed")
}

func TestPersistentSTT_LongResponse(t *testing.T) {
	longText := strings.Repeat("a", 10000)
	script := fmt.Sprintf(`#!/bin/sh
IFS= read -r line
echo '{"text":"%s","error":""}'`, longText)
	scriptPath := filepath.Join(t.TempDir(), "long.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	s := NewPersistentSTT(scriptPath, "test-stt", 0, slog.Default())
	defer func() { _ = s.Close(context.Background()) }()

	text, err := s.Transcribe(context.Background(), []byte("audio"))
	require.NoError(t, err)
	require.Equal(t, longText, text)
}

func TestPersistentSTT_ConcurrentClose(t *testing.T) {
	script := `#!/bin/sh
while IFS= read -r line; do
	echo '{"text":"ok","error":""}'
done
`
	scriptPath := filepath.Join(t.TempDir(), "concurrent_close.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	s := NewPersistentSTT(scriptPath, "test-stt", 0, slog.Default())
	_, _ = s.Transcribe(context.Background(), []byte("audio"))

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Close(context.Background())
		}()
	}
	wg.Wait()
}

func TestPersistentSTT_JSONRoundTrip(t *testing.T) {
	script := `#!/bin/sh
IFS= read -r line
echo "$line" | grep -q '"audio_path"' 2>/dev/null
if [ $? -eq 0 ]; then
	echo '{"text":"protocol ok","error":""}'
else
	echo '{"text":"","error":"bad protocol"}'
fi
`
	scriptPath := filepath.Join(t.TempDir(), "proto_test.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	s := NewPersistentSTT(scriptPath, "test-stt", 0, slog.Default())
	defer func() { _ = s.Close(context.Background()) }()

	text, err := s.Transcribe(context.Background(), []byte("test-audio"))
	require.NoError(t, err)
	require.Equal(t, "protocol ok", text)
}

// ---------------------------------------------------------------------------
// Helper: RandomAlphaNum
// ---------------------------------------------------------------------------

func TestRandomAlphaNum_Length(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		n    int
	}{
		{"zero", 0},
		{"one", 1},
		{"sixteen", 16},
		{"thirtytwo", 32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := RandomAlphaNum(tt.n)
			require.Len(t, result, tt.n)
		})
	}
}

func TestRandomAlphaNum_Charset(t *testing.T) {
	t.Parallel()
	result := RandomAlphaNum(100)
	for _, c := range result {
		require.True(t, (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'),
			"unexpected char: %c", c)
	}
}

func TestRandomAlphaNum_Unique(t *testing.T) {
	results := make(map[string]bool)
	for i := range 100 {
		s := RandomAlphaNum(16)
		require.False(t, results[s], "duplicate at iteration %d: %s", i, s)
		results[s] = true
	}
}

// ---------------------------------------------------------------------------
// Helper: AudioToPCM
// ---------------------------------------------------------------------------

func TestAudioToPCM_NoFFmpeg(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		_, err := AudioToPCM(context.Background(), []byte("fake"))
		require.Error(t, err)
	}
}

func TestAudioToPCM_EmptyInput(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	_, err := AudioToPCM(context.Background(), []byte{})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// mock transcriber
// ---------------------------------------------------------------------------

type mockTranscriber struct {
	text   string
	err    error
	called bool
}

func (m *mockTranscriber) Transcribe(_ context.Context, _ []byte) (string, error) {
	m.called = true
	return m.text, m.err
}

func (m *mockTranscriber) RequiresDisk() bool { return true }
