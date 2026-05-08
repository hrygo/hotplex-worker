package tts

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

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

// --- FallbackSynthesizer Tests ---

func TestFallbackSynthesizer_PrimarySuccess(t *testing.T) {
	t.Parallel()

	primary := &mockSynthesizer{
		synthesizeFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("primary-audio"), nil
		},
	}
	secondary := &mockSynthesizer{
		synthesizeFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("secondary-audio"), nil
		},
	}

	fb := NewFallbackSynthesizer(primary, secondary, slog.Default())
	data, err := fb.Synthesize(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, []byte("primary-audio"), data)
}

func TestFallbackSynthesizer_FallsBackOnPrimaryError(t *testing.T) {
	t.Parallel()

	primary := &mockSynthesizer{
		synthesizeFn: func(_ context.Context, _ string) ([]byte, error) {
			return nil, errors.New("primary failed")
		},
	}
	secondary := &mockSynthesizer{
		synthesizeFn: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("secondary-audio"), nil
		},
	}

	fb := NewFallbackSynthesizer(primary, secondary, slog.Default())
	data, err := fb.Synthesize(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, []byte("secondary-audio"), data)
}

func TestFallbackSynthesizer_SkipsFallbackOnContextCancelled(t *testing.T) {
	t.Parallel()

	primary := &mockSynthesizer{
		synthesizeFn: func(_ context.Context, _ string) ([]byte, error) {
			return nil, errors.New("primary failed")
		},
	}
	secondaryCalled := false
	secondary := &mockSynthesizer{
		synthesizeFn: func(_ context.Context, _ string) ([]byte, error) {
			secondaryCalled = true
			return []byte("secondary-audio"), nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fb := NewFallbackSynthesizer(primary, secondary, slog.Default())
	_, err := fb.Synthesize(ctx, "hello")
	assert.Error(t, err)
	assert.False(t, secondaryCalled, "secondary should not be called when ctx is cancelled")
}

// mockCloserSynthesizer implements both Synthesizer and Closer.
type mockCloserSynthesizer struct {
	mockSynthesizer
	closeCalled bool
	closeErr    error
}

func (m *mockCloserSynthesizer) Close(_ context.Context) error {
	m.closeCalled = true
	return m.closeErr
}

func TestFallbackSynthesizer_ClosesBothSynthesizers(t *testing.T) {
	t.Parallel()

	primary := &mockCloserSynthesizer{}
	secondary := &mockCloserSynthesizer{}

	fb := NewFallbackSynthesizer(primary, secondary, slog.Default())
	err := fb.Close(context.Background())
	require.NoError(t, err)
	assert.True(t, primary.closeCalled)
	assert.True(t, secondary.closeCalled)
}

func TestFallbackSynthesizer_CloseCollectsErrors(t *testing.T) {
	t.Parallel()

	primary := &mockCloserSynthesizer{closeErr: errors.New("primary close err")}
	secondary := &mockCloserSynthesizer{closeErr: errors.New("secondary close err")}

	fb := NewFallbackSynthesizer(primary, secondary, slog.Default())
	err := fb.Close(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "primary close")
	assert.Contains(t, err.Error(), "secondary close")
}

// --- MossSynthesizer Tests ---

func TestMossSynthesizer_ImplementsInterfaces(t *testing.T) {
	t.Parallel()

	var _ Synthesizer = (*MossSynthesizer)(nil)
	var _ Closer = (*MossSynthesizer)(nil)
}

func TestMossSynthesizer_EmptyText(t *testing.T) {
	t.Parallel()

	m := NewMossSynthesizer("/tmp/moss", "", 0, 0, 0, slog.Default())
	_, err := m.Synthesize(context.Background(), "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty text")
}

// --- Factory Tests ---

func TestNewConfiguredSynthesizer(t *testing.T) {
	t.Parallel()

	cfg := SynthesizerConfig{
		EdgeVoice:       "zh-CN-YunxiNeural",
		MossModelDir:    "/tmp/moss",
		MossVoice:       "Xiaoyu",
		MossPort:        18083,
		MossCpuThreads:  2,
		MossIdleTimeout: 5 * time.Minute,
	}
	synth := NewConfiguredSynthesizer(cfg, slog.Default())
	require.NotNil(t, synth)

	fb, ok := synth.(*FallbackSynthesizer)
	require.True(t, ok, "should return a FallbackSynthesizer")
	require.NotNil(t, fb.primary)
	require.NotNil(t, fb.secondary)
}

func TestNewConfiguredSynthesizer_DefaultVoice(t *testing.T) {
	t.Parallel()

	synth := NewConfiguredSynthesizer(SynthesizerConfig{}, slog.Default())
	fb, ok := synth.(*FallbackSynthesizer)
	require.True(t, ok)

	edge, ok := fb.primary.(*EdgeSynthesizer)
	require.True(t, ok)
	assert.Equal(t, "zh-CN-XiaoxiaoNeural", edge.voice)
}
