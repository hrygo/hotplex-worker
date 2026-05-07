package tts

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	onnxruntime "github.com/yalue/onnxruntime_go"
)

// KokoroSynthesizer uses local CPU with ONNX model (Kokoro-82M).
// It implements a "temporary resident" pattern: loads the model on demand
// and unloads it after a configurable idle timeout (default 30m) to save memory.
type KokoroSynthesizer struct {
	modelPath string
	voice     string
	voicePath string
	vocabPath string
	log       *slog.Logger

	mu             sync.Mutex
	lastUsed       time.Time
	idleTimeout    time.Duration
	idleTimer      *time.Timer
	closed         bool
	activeRequests sync.WaitGroup

	// Runtime state (guarded by mu)
	session *onnxruntime.DynamicAdvancedSession
	vocab   map[string]int64
	style   []float32

	// ONNX input/output names (discovered at load time)
	inputNames  []string
	outputNames []string
}

func NewKokoroSynthesizer(modelPath, voice string, log *slog.Logger) *KokoroSynthesizer {
	return NewKokoroSynthesizerWithOptions(modelPath, voice, 30*time.Minute, log)
}

func NewKokoroSynthesizerWithOptions(modelPath, voice string, idleTimeout time.Duration, log *slog.Logger) *KokoroSynthesizer {
	if voice == "" {
		voice = "af_heart"
	}
	if idleTimeout <= 0 {
		idleTimeout = 30 * time.Minute
	}

	// Derive asset paths from model directory.
	modelDir := filepath.Dir(modelPath)

	return &KokoroSynthesizer{
		modelPath:   modelPath,
		voice:       voice,
		voicePath:   filepath.Join(modelDir, "voices", voice+".bin"),
		vocabPath:   filepath.Join(modelDir, "vocab.json"),
		log:         log,
		idleTimeout: idleTimeout,
	}
}

func (k *KokoroSynthesizer) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("tts: empty text")
	}

	// Acquire lock only for state management, not for inference.
	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		return nil, ErrSynthesizerClosed
	}

	// 1. Lazy load if not loaded
	if k.session == nil {
		if err := k.load(ctx); err != nil {
			k.mu.Unlock()
			return nil, fmt.Errorf("tts kokoro load: %w", err)
		}
	}

	// 2. Update usage and reset/start idle timer
	k.lastUsed = time.Now()
	if k.idleTimer != nil {
		k.idleTimer.Stop()
	}
	k.idleTimer = time.AfterFunc(k.idleTimeout, k.unloadOnIdle)

	// 3. Track active request count, then release lock for inference.
	k.activeRequests.Add(1)
	k.mu.Unlock()

	defer k.activeRequests.Done()

	// 4. G2P: text → IPA phonemes
	phonemes, err := k.phonemize(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("tts kokoro g2p: %w", err)
	}

	// 5. Tokenize: IPA → token IDs
	tokens := k.tokenize(phonemes)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("tts kokoro: tokenization produced empty tokens for text %q", text)
	}

	// 6. ONNX inference: tokens + style → PCM float32
	pcm, err := k.infer(ctx, tokens)
	if err != nil {
		return nil, fmt.Errorf("tts kokoro infer: %w", err)
	}

	// 7. Encode PCM float32 → WAV bytes
	wav := encodePCMFloat32ToWAV(pcm)

	k.log.Debug("tts: synthesized (kokoro)", "voice", k.voice, "text_len", len(text), "audio_len", len(wav))
	return wav, nil
}

func (k *KokoroSynthesizer) load(_ context.Context) error {
	k.log.Info("tts: loading kokoro model into memory", "path", k.modelPath, "voice", k.voicePath)

	// Initialize ONNX Runtime environment if not already initialized.
	if !onnxruntime.IsInitialized() {
		libPath, err := findOnnxRuntimeLibrary()
		if err != nil {
			return fmt.Errorf("onnxruntime library not found: %w (install with: brew install onnxruntime / apt install libonnxruntime-dev)", err)
		}
		onnxruntime.SetSharedLibraryPath(libPath)
		if err := onnxruntime.InitializeEnvironment(); err != nil {
			return fmt.Errorf("onnxruntime init: %w", err)
		}
	}

	// Load vocab.
	vocab, err := loadVocab(k.vocabPath)
	if err != nil {
		return fmt.Errorf("load vocab: %w", err)
	}
	k.vocab = vocab

	// Load voice style embedding.
	style, err := loadVoiceEmbedding(k.voicePath)
	if err != nil {
		return fmt.Errorf("load voice %s: %w", k.voicePath, err)
	}
	k.style = style

	// Discover model I/O names.
	inputs, outputs, err := onnxruntime.GetInputOutputInfo(k.modelPath)
	if err != nil {
		return fmt.Errorf("inspect model: %w", err)
	}

	inputNames := make([]string, len(inputs))
	for i, inp := range inputs {
		inputNames[i] = inp.Name
	}
	outputNames := make([]string, len(outputs))
	for i, out := range outputs {
		outputNames[i] = out.Name
	}
	k.inputNames = inputNames
	k.outputNames = outputNames

	// Create ONNX session.
	session, err := onnxruntime.NewDynamicAdvancedSession(
		k.modelPath, inputNames, outputNames, nil,
	)
	if err != nil {
		return fmt.Errorf("create onnx session: %w", err)
	}
	k.session = session

	k.log.Info("tts: kokoro model loaded", "inputs", inputNames, "outputs", outputNames, "vocab_size", len(vocab))
	return nil
}

func (k *KokoroSynthesizer) unloadOnIdle() {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.session == nil || k.closed {
		return
	}

	if time.Since(k.lastUsed) < k.idleTimeout {
		return
	}

	k.log.Info("tts: kokoro model idle, unloading to release resources", "idle_timeout", k.idleTimeout)
	k.unload()
}

func (k *KokoroSynthesizer) unload() {
	if k.session != nil {
		_ = k.session.Destroy()
		k.session = nil
	}
	if k.idleTimer != nil {
		k.idleTimer.Stop()
		k.idleTimer = nil
	}
}

// Close terminates the synthesizer. It marks the instance as closed,
// prevents new requests, and waits for in-flight requests to complete.
func (k *KokoroSynthesizer) Close(_ context.Context) error {
	k.mu.Lock()
	k.closed = true
	k.unload()
	k.mu.Unlock()

	k.activeRequests.Wait()
	return nil
}

// --- G2P via espeak-ng CLI ---

func (k *KokoroSynthesizer) phonemize(ctx context.Context, text string) (string, error) {
	cmd := exec.CommandContext(ctx, "espeak-ng", "--ipa=3", "-q", text)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		hint := stderr.String()
		if hint == "" {
			hint = err.Error()
		}
		return "", fmt.Errorf("espeak-ng: %s", hint)
	}

	phonemes := bytes.TrimSpace(stdout.Bytes())
	if len(phonemes) == 0 {
		return "", fmt.Errorf("espeak-ng: empty output for text %q", text)
	}
	return string(phonemes), nil
}

// --- Tokenization ---

func (k *KokoroSynthesizer) tokenize(ipa string) []int64 {
	if k.vocab == nil {
		return nil
	}

	tokens := make([]int64, 0, len(ipa)+2)

	// BOS token (if present in vocab)
	if id, ok := k.vocab["^"]; ok {
		tokens = append(tokens, id)
	}

	// Map each IPA character to its token ID
	for _, ch := range ipa {
		s := string(ch)
		if id, ok := k.vocab[s]; ok {
			tokens = append(tokens, id)
		}
		// Unknown characters are silently skipped
	}

	// EOS token (if present in vocab)
	if id, ok := k.vocab["$"]; ok {
		tokens = append(tokens, id)
	}

	return tokens
}

// --- ONNX Inference ---

func (k *KokoroSynthesizer) infer(_ context.Context, tokens []int64) ([]float32, error) {
	k.mu.Lock()
	session := k.session
	inputNames := k.inputNames
	style := k.style
	k.mu.Unlock()

	if session == nil {
		return nil, fmt.Errorf("session not loaded")
	}

	// Create input tensors.
	tokenShape := onnxruntime.NewShape(1, int64(len(tokens)))
	tokenData := make([]int64, len(tokens))
	copy(tokenData, tokens)
	tokenTensor, err := onnxruntime.NewTensor(tokenShape, tokenData)
	if err != nil {
		return nil, fmt.Errorf("create token tensor: %w", err)
	}
	defer func() { _ = tokenTensor.Destroy() }()

	styleShape := onnxruntime.NewShape(1, int64(len(style)))
	styleData := make([]float32, len(style))
	copy(styleData, style)
	styleTensor, err := onnxruntime.NewTensor(styleShape, styleData)
	if err != nil {
		return nil, fmt.Errorf("create style tensor: %w", err)
	}
	defer func() { _ = styleTensor.Destroy() }()

	speedData := []float32{1.0}
	speedTensor, err := onnxruntime.NewScalar(speedData[0])
	if err != nil {
		return nil, fmt.Errorf("create speed tensor: %w", err)
	}
	defer func() { _ = speedTensor.Destroy() }()

	// Map inputs by name order.
	inputs := make([]onnxruntime.Value, len(inputNames))
	for i, name := range inputNames {
		switch name {
		case "tokens", "input_ids":
			inputs[i] = tokenTensor
		case "style", "style_vec", "g":
			inputs[i] = styleTensor
		case "speed":
			inputs[i] = speedTensor
		default:
			return nil, fmt.Errorf("unknown model input %q", name)
		}
	}

	// Create output tensor (dynamic size — use IoBinding or large pre-allocated).
	// Kokoro outputs up to ~30s of audio at 24kHz = 720K samples.
	const maxSamples = 720000
	outputShape := onnxruntime.NewShape(1, maxSamples)
	outputData := make([]float32, maxSamples)
	outputTensor, err := onnxruntime.NewTensor(outputShape, outputData)
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer func() { _ = outputTensor.Destroy() }()

	outputs := []onnxruntime.Value{outputTensor}

	// Run inference.
	if err := session.Run(inputs, outputs); err != nil {
		return nil, fmt.Errorf("onnx run: %w", err)
	}

	// Trim silence from output (find last non-zero sample).
	result := outputTensor.GetData()
	end := len(result)
	threshold := float32(1e-4)
	for end > 0 && abs32(result[end-1]) < threshold {
		end--
	}
	if end == 0 {
		return result[:1], nil
	}
	return result[:end], nil
}

// --- Asset Loading ---

func loadVocab(path string) (map[string]int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read vocab: %w", err)
	}

	// Support two formats:
	// 1. {"phoneme": id, ...}  (string → int)
	// 2. {"id": "phoneme", ...}  (string → string)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse vocab: %w", err)
	}

	vocab := make(map[string]int64, len(raw))

	// Detect format: if first value is a number, it's phoneme→id.
	for k, v := range raw {
		var numVal int64
		if err := json.Unmarshal(v, &numVal); err == nil {
			vocab[k] = numVal
			continue
		}
		// Otherwise it's id→phoneme: map phoneme back to id.
		var strVal string
		if err := json.Unmarshal(v, &strVal); err == nil {
			var id int64
			if _, err := fmt.Sscanf(k, "%d", &id); err == nil {
				vocab[strVal] = id
			}
		}
	}

	return vocab, nil
}

func loadVoiceEmbedding(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read voice: %w", err)
	}

	if len(data)%4 != 0 {
		return nil, fmt.Errorf("voice file size %d not aligned to float32", len(data))
	}

	n := len(data) / 4
	vec := make([]float32, n)
	br := bytes.NewReader(data)
	if err := binary.Read(br, binary.LittleEndian, &vec); err != nil {
		return nil, fmt.Errorf("decode voice: %w", err)
	}

	return vec, nil
}

// --- ONNX Runtime Library Discovery ---

func findOnnxRuntimeLibrary() (string, error) {
	// Check ONNXRUNTIME_LIB_PATH environment variable first.
	if p := os.Getenv("ONNXRUNTIME_LIB_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// Search common paths based on platform.
	candidates := onnxRuntimeSearchPaths()
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// Try to find via PATH lookup.
	for _, name := range []string{"libonnxruntime.so", "libonnxruntime.dylib", "onnxruntime.dll"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("onnxruntime shared library not found in any known location")
}
