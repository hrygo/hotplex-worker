package tts

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- WAV Encoding Tests ---

func TestEncodePCMFloat32ToWAV_Silence(t *testing.T) {
	t.Parallel()

	samples := make([]float32, 48000) // 2 seconds of silence at 24kHz
	wav := encodePCMFloat32ToWAV(samples)

	// Verify WAV header
	require.Len(t, wav, 44+len(samples)*2)
	assert.Equal(t, "RIFF", string(wav[0:4]))
	assert.Equal(t, "WAVE", string(wav[8:12]))
	assert.Equal(t, "fmt ", string(wav[12:16]))
	assert.Equal(t, "data", string(wav[36:40]))

	// Verify format: PCM, mono, 24kHz, 16-bit
	assert.Equal(t, uint16(1), binary.LittleEndian.Uint16(wav[20:22]))     // PCM
	assert.Equal(t, uint16(1), binary.LittleEndian.Uint16(wav[22:24]))     // mono
	assert.Equal(t, uint32(24000), binary.LittleEndian.Uint32(wav[24:28])) // sample rate
	assert.Equal(t, uint16(16), binary.LittleEndian.Uint16(wav[34:36]))    // bits per sample
}

func TestEncodePCMFloat32ToWAV_Clipping(t *testing.T) {
	t.Parallel()

	samples := []float32{2.0, -2.0, 0.5, -0.5}
	wav := encodePCMFloat32ToWAV(samples)

	// Values should be clamped to [-1.0, 1.0] then scaled to int16
	require.Len(t, wav, 44+len(samples)*2)

	readInt16 := func(i int) int16 {
		return int16(binary.LittleEndian.Uint16(wav[44+i*2:]))
	}

	maxVal := int16(32767)
	assert.Equal(t, maxVal, readInt16(0))        // 2.0 clamped to 1.0 → 32767
	assert.Equal(t, -maxVal, readInt16(1))       // -2.0 clamped to -1.0 → -32767
	assert.Equal(t, int16(16383), readInt16(2))  // 0.5 → ~16383
	assert.Equal(t, int16(-16383), readInt16(3)) // -0.5 → ~-16383
}

func TestEncodePCMFloat32ToWAV_Empty(t *testing.T) {
	t.Parallel()

	wav := encodePCMFloat32ToWAV([]float32{})
	require.Len(t, wav, 44) // Just the header
}

func TestEncodePCMFloat32ToWAV_NaNInf(t *testing.T) {
	t.Parallel()

	samples := []float32{
		float32(math.NaN()),
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
		0.5,
	}
	wav := encodePCMFloat32ToWAV(samples)
	require.Len(t, wav, 44+len(samples)*2)

	readInt16 := func(i int) int16 {
		return int16(binary.LittleEndian.Uint16(wav[44+i*2:]))
	}

	// NaN and Inf should be treated as 0.
	assert.Equal(t, int16(0), readInt16(0))
	assert.Equal(t, int16(0), readInt16(1))
	assert.Equal(t, int16(0), readInt16(2))
	// Normal value should pass through.
	assert.Equal(t, int16(16383), readInt16(3))
}

// --- Vocab Loading Tests ---

func TestLoadVocab_PhonemeToID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	vocab := map[string]int64{"a": 1, "b": 2, "c": 3, "^": 0, "$": 99}
	data, err := json.Marshal(vocab)
	require.NoError(t, err)
	path := filepath.Join(dir, "vocab.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	got, err := loadVocab(path)
	require.NoError(t, err)
	assert.Equal(t, int64(1), got["a"])
	assert.Equal(t, int64(2), got["b"])
	assert.Equal(t, int64(0), got["^"])
	assert.Equal(t, int64(99), got["$"])
}

func TestLoadVocab_IDToPhoneme(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Format: {"0": "^", "1": "a", "2": "b"}
	vocab := map[string]string{"0": "^", "1": "a", "2": "b"}
	data, err := json.Marshal(vocab)
	require.NoError(t, err)
	path := filepath.Join(dir, "vocab.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	got, err := loadVocab(path)
	require.NoError(t, err)
	assert.Equal(t, int64(0), got["^"])
	assert.Equal(t, int64(1), got["a"])
	assert.Equal(t, int64(2), got["b"])
}

func TestLoadVocab_NotFound(t *testing.T) {
	t.Parallel()

	_, err := loadVocab("/nonexistent/vocab.json")
	assert.Error(t, err)
}

func TestLoadVocab_InvalidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "vocab.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o644))

	_, err := loadVocab(path)
	assert.Error(t, err)
}

// --- Voice Embedding Loading Tests ---

func TestLoadVoiceEmbedding(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	vec := []float32{0.1, 0.2, 0.3, -0.5, 1.0}
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	path := filepath.Join(dir, "voice.bin")
	require.NoError(t, os.WriteFile(path, buf, 0o644))

	got, err := loadVoiceEmbedding(path)
	require.NoError(t, err)
	assert.InDelta(t, float32(0.1), got[0], 0.001)
	assert.InDelta(t, float32(0.2), got[1], 0.001)
	assert.InDelta(t, float32(0.3), got[2], 0.001)
	assert.InDelta(t, float32(-0.5), got[3], 0.001)
	assert.InDelta(t, float32(1.0), got[4], 0.001)
}

func TestLoadVoiceEmbedding_NotFound(t *testing.T) {
	t.Parallel()

	_, err := loadVoiceEmbedding("/nonexistent/voice.bin")
	assert.Error(t, err)
}

func TestLoadVoiceEmbedding_InvalidSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "voice.bin")
	require.NoError(t, os.WriteFile(path, []byte{1, 2, 3}, 0o644)) // 3 bytes, not aligned to 4

	_, err := loadVoiceEmbedding(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not aligned")
}

// --- Tokenization Tests ---

func TestKokoroSynthesizer_Tokenize(t *testing.T) {
	t.Parallel()

	k := NewKokoroSynthesizer("model.onnx", "", nil)
	k.vocab = map[string]int64{
		"^": 0, "$": 99,
		"h": 10, "ɛ": 11, "l": 12, "ə": 13, "ʊ": 14,
	}

	tokens := k.tokenize("hɛləʊ")
	require.Equal(t, []int64{0, 10, 11, 12, 13, 14, 99}, tokens)
}

func TestKokoroSynthesizer_Tokenize_SkipsUnknown(t *testing.T) {
	t.Parallel()

	k := NewKokoroSynthesizer("model.onnx", "", nil)
	k.vocab = map[string]int64{
		"^": 0, "$": 99,
		"a": 1, "b": 2,
	}

	tokens := k.tokenize("aXb")
	require.Equal(t, []int64{0, 1, 2, 99}, tokens)
}

func TestKokoroSynthesizer_Tokenize_Empty(t *testing.T) {
	t.Parallel()

	k := NewKokoroSynthesizer("model.onnx", "", nil)
	k.vocab = map[string]int64{"^": 0, "$": 99}

	tokens := k.tokenize("")
	require.Equal(t, []int64{0, 99}, tokens)
}

func TestKokoroSynthesizer_Tokenize_NilVocab(t *testing.T) {
	t.Parallel()

	k := NewKokoroSynthesizer("model.onnx", "", nil)
	// vocab is nil
	tokens := k.tokenize("hello")
	assert.Nil(t, tokens)
}

// --- ONNX Runtime Discovery Tests ---

func TestFindOnnxRuntimeLibrary_NotFound(t *testing.T) {
	t.Parallel()

	_, err := findOnnxRuntimeLibrary()
	// In test environments, onnxruntime is unlikely to be installed.
	// This just verifies the function doesn't panic.
	_ = err
}

// --- KokoroSynthesizer Asset Path Derivation ---

func TestNewKokoroSynthesizer_AssetPaths(t *testing.T) {
	t.Parallel()

	k := NewKokoroSynthesizer("/opt/models/kokoro-v1.0.onnx", "af_bella", nil)
	assert.Equal(t, "/opt/models/voices/af_bella.bin", k.voicePath)
	assert.Equal(t, "/opt/models/vocab.json", k.vocabPath)
}

func TestNewKokoroSynthesizerWithOptions_AssetPaths(t *testing.T) {
	t.Parallel()

	k := NewKokoroSynthesizerWithOptions("model.onnx", "custom_voice", 0, nil)
	assert.Equal(t, "voices/custom_voice.bin", k.voicePath)
	assert.Equal(t, "vocab.json", k.vocabPath)
}

// --- WAV Integration Test ---

func TestEncodePCMFloat32ToWAV_SineWave(t *testing.T) {
	t.Parallel()

	// Generate a 440Hz sine wave for 0.1 seconds at 24kHz.
	const sampleRate = 24000
	const freq = 440.0
	const duration = 0.1
	numSamples := int(float64(sampleRate) * duration)
	samples := make([]float32, numSamples)
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / float64(sampleRate)))
	}

	wav := encodePCMFloat32ToWAV(samples)
	require.Len(t, wav, 44+numSamples*2)

	// Verify first sample is near zero (sin(0) = 0)
	firstSample := int16(binary.LittleEndian.Uint16(wav[44:46]))
	assert.Less(t, absInt16(firstSample), int16(100)) // near zero

	// Verify a sample near the peak (quarter period ≈ 24000/440/4 ≈ 13.6 samples)
	peakIdx := 44 + 14*2
	peakSample := int16(binary.LittleEndian.Uint16(wav[peakIdx : peakIdx+2]))
	assert.Greater(t, absInt16(peakSample), int16(30000)) // near max amplitude
}

func absInt16(x int16) int16 {
	if x < 0 {
		return -x
	}
	return x
}
