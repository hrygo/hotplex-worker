package tts

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSynthesizer is a test double for the Synthesizer interface.
type mockSynthesizer struct {
	synthesizeFn func(ctx context.Context, text string) ([]byte, error)
}

func (m *mockSynthesizer) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if m.synthesizeFn != nil {
		return m.synthesizeFn(ctx, text)
	}
	return []byte("fake-opus-audio"), nil
}

func TestMockSynthesizer_ImplementsInterface(t *testing.T) {
	t.Parallel()

	var _ Synthesizer = (*mockSynthesizer)(nil)
}

func TestEdgeSynthesizer_ImplementsInterface(t *testing.T) {
	t.Parallel()

	var _ Synthesizer = (*EdgeSynthesizer)(nil)
}

func TestNewEdgeSynthesizer_DefaultVoice(t *testing.T) {
	t.Parallel()

	s := NewEdgeSynthesizer("", slog.Default())
	require.NotNil(t, s)
	assert.Equal(t, "zh-CN-XiaoxiaoNeural", s.voice)
}

func TestNewEdgeSynthesizer_CustomVoice(t *testing.T) {
	t.Parallel()

	s := NewEdgeSynthesizer("zh-CN-YunxiNeural", slog.Default())
	require.NotNil(t, s)
	assert.Equal(t, "zh-CN-YunxiNeural", s.voice)
}

func TestEdgeSynthesizer_Synthesize_EmptyText(t *testing.T) {
	t.Parallel()

	s := NewEdgeSynthesizer("", slog.Default())
	_, err := s.Synthesize(context.Background(), "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty text")
}

func TestEstimateAudioDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		bytes int
		want  int
	}{
		{"zero bytes", 0, 1},
		{"negative bytes", -1, 1},
		{"small bytes", 500, 1},
		{"1 second", 2000, 1},
		{"5 seconds", 10000, 5},
		{"60 seconds", 120000, 60},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, EstimateAudioDuration(tt.bytes))
		})
	}
}

func TestMP3ToOpus_InvalidInput(t *testing.T) {
	t.Parallel()

	// Garbage input should produce an error from ffmpeg
	_, err := MP3ToOpus(context.Background(), []byte("not-mp3"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ffmpeg")
}

func TestMP3ToOpus_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := MP3ToOpus(ctx, []byte("data"))
	assert.Error(t, err)
}

func TestMockSynthesizer_CustomBehavior(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("synthesis failed")
	m := &mockSynthesizer{
		synthesizeFn: func(_ context.Context, text string) ([]byte, error) {
			return nil, expectedErr
		},
	}

	_, err := m.Synthesize(context.Background(), "test")
	assert.ErrorIs(t, err, expectedErr)
}

func TestMockSynthesizer_Success(t *testing.T) {
	t.Parallel()

	m := &mockSynthesizer{}
	data, err := m.Synthesize(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, []byte("fake-opus-audio"), data)
}
